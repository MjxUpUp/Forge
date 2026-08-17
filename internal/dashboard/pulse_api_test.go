package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// pulseServer mounts the full middleware stack (Host validation + security headers + mux),
// mirroring Serve, so the pulse endpoints are tested through the same defenses.
//
// pulseServer 挂完整 middleware 栈（Host 校验 + 安全头 + mux），与 Serve 一致，
// 让 pulse 端点在同样的防线内被测。
func pulseServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(localhostOnly(securityHeaders(newMux(opts))))
	t.Cleanup(srv.Close)
	return srv
}

func pulseGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

// TestServe_PulseFeed: /api/pulse/feed.json returns 200 + generatedAt + merged events;
// a malformed since is a 400 (client error, not a 500). A limit below the event count
// truncates and marks truncated:true (the client full-refetches on a truncated poll).
//
// TestServe_PulseFeed：/api/pulse/feed.json 返回 200 + generatedAt + 归并事件；
// 畸形 since 是 400（客户端错误，非 500）。limit 低于事件数时截断并标记
// truncated:true（客户端在截断的轮询后全量重拉）。
func TestServe_PulseFeed(t *testing.T) {
	root, _, _ := feedFixture(t)
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/feed.json")
	if code != 200 {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	var payload struct {
		GeneratedAt time.Time   `json:"generatedAt"`
		Events      []FeedEvent `json:"events"`
		Truncated   bool        `json:"truncated"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if payload.GeneratedAt.IsZero() {
		t.Error("generatedAt 不得为零值")
	}
	if len(payload.Events) != 5 {
		t.Errorf("events len = %d, want 5", len(payload.Events))
	}
	if payload.Truncated {
		t.Error("5 条事件默认 limit 不应截断")
	}

	// limit 低于事件数：截断 + truncated 标记。
	code, body = pulseGet(t, srv.URL+"/api/pulse/feed.json?limit=2")
	if code != 200 {
		t.Fatalf("limit 请求 status = %d", code)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 2 || !payload.Truncated {
		t.Errorf("limit=2 应返回 2 条且 truncated=true，got %d 条 truncated=%v", len(payload.Events), payload.Truncated)
	}

	code, _ = pulseGet(t, srv.URL+"/api/pulse/feed.json?since=not-a-time")
	if code != 400 {
		t.Errorf("畸形 since 应 400，got %d", code)
	}

	// since 过滤经 HTTP 层生效。
	code, body = pulseGet(t, srv.URL+"/api/pulse/feed.json?since="+url.QueryEscape(time.Unix(1700000000, 0).UTC().Add(2*time.Hour).Format(time.RFC3339)))
	if code != 200 {
		t.Fatalf("since 过滤请求 status = %d", code)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 2 {
		t.Errorf("since 过滤后 events len = %d, want 2", len(payload.Events))
	}
}

// TestServe_PulseTask: /api/pulse/task.json returns the task state + its events + score;
// SessionID never leaks into the JSON body.
//
// TestServe_PulseTask：/api/pulse/task.json 返回任务状态 + 其事件 + 评分；
// SessionID 绝不泄露进 JSON body。
func TestServe_PulseTask(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	st := &taskpipeline.TaskState{
		TaskRef: "feat/x", Branch: "feat/x", OriginTool: "pi",
		SessionID: "secret-session-xyz", // 值含 session 字样，泄露则测试红
		StartedAt: base,
		// 直构 History 时 CurrentGate 不会自动推进（那是 RecordGateResult 的职责），显式设置。
		CurrentGate: "task-verify",
		History: []taskpipeline.TaskGateResult{
			{Gate: "task-implement", Passed: true, CompletedAt: base.Add(time.Hour), HeadCommit: "abc1234567890"},
		},
		Score: nil,
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/x", SessionID: "secret-session-xyz",
		Score: 85, Grade: "B", Strength: "Strong",
		Deterministic: 4, AgentClaim: 1, AcceptancePass: 2, AcceptanceTotal: 2,
		CompletedAt: base.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/x")
	if code != 200 {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	if strings.Contains(string(body), "secret-session-xyz") {
		t.Fatalf("task.json 泄露 SessionID: %s", body)
	}
	var payload struct {
		TaskRef string `json:"taskRef"`
		Project string `json:"project"`
		State   struct {
			CurrentGate  string    `json:"currentGate"`
			StartedAt    time.Time `json:"startedAt"`
			OriginTool   string    `json:"originTool"`
			Zombie       bool      `json:"zombie"`
			GateProgress struct {
				Passed int `json:"passed"`
				Total  int `json:"total"`
			} `json:"gateProgress"`
		} `json:"state"`
		Events []FeedEvent `json:"events"`
		Score  *struct {
			Overall        float64 `json:"overall"`
			Grade          string  `json:"grade"`
			FromConclusion bool    `json:"fromConclusion"`
		} `json:"score"`
		Acceptance struct {
			Pass  int `json:"pass"`
			Total int `json:"total"`
		} `json:"acceptance"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if payload.TaskRef != "feat/x" || payload.Project == "" {
		t.Errorf("taskRef/project 异常: %q/%q", payload.TaskRef, payload.Project)
	}
	if payload.State.CurrentGate != "task-verify" || payload.State.OriginTool != "pi" || payload.State.Zombie {
		t.Errorf("state 异常: %+v", payload.State)
	}
	if payload.State.GateProgress.Passed != 1 || payload.State.GateProgress.Total != 3 {
		t.Errorf("gateProgress = %+v, want 1/3", payload.State.GateProgress)
	}
	if len(payload.Events) == 0 {
		t.Error("events 不得为空（至少有 task-start + gate）")
	}
	for _, e := range payload.Events {
		if e.TaskRef != "feat/x" {
			t.Errorf("task.json 事件须全部属于 feat/x: %+v", e)
		}
	}
	if payload.Acceptance.Pass != 2 || payload.Acceptance.Total != 2 {
		t.Errorf("acceptance = %+v, want 2/2（来自 act 结论）", payload.Acceptance)
	}
	// state.Score 为 nil 的存量任务：评分块须从结论回填（否则详情页与自己的
	// conclusion 事件自相矛盾——流里显示 B 85 分，评分块却显示"未评分"）。
	if payload.Score == nil || payload.Score.Overall != 85 || payload.Score.Grade != "B" || !payload.Score.FromConclusion {
		t.Errorf("score 结论回填异常: %+v", payload.Score)
	}
}

// TestServe_PulseTask_ScoredTask: a task with TaskState.Score emits the score block with
// dimensions (name/weight/score/detail) and evidence.
//
// TestServe_PulseTask_ScoredTask：带 TaskState.Score 的任务输出 score 块，含维度
// （name/weight/score/detail）与 evidence。
func TestServe_PulseTask_ScoredTask(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	st := &taskpipeline.TaskState{
		TaskRef: "feat/scored", Branch: "feat/scored", StartedAt: base,
	}
	for _, g := range taskpipeline.DefaultGates() {
		st.History = append(st.History, taskpipeline.TaskGateResult{Gate: g.ID, Passed: true, CompletedAt: base.Add(time.Hour)})
	}
	// 直接构造 ScoreResult（ScoreTask 要走完整评分管线，fixture 直填更稳）。
	st.Score = &scoringtypes.ScoreResult{
		TaskRef: "feat/scored", Overall: 90, Grade: "A", ScoredAt: base,
		Dimensions: []scoringtypes.DimensionScore{
			{Dimension: scoringtypes.DimensionProcess, Score: 95, Detail: "门禁全过"},
			{Dimension: scoringtypes.DimensionTesting, Score: 85, Detail: "测试齐备"},
		},
		Evidence:     &scoringtypes.EvidenceSummary{Deterministic: 3, AgentClaim: 1, Total: 4, Ratio: 0.75},
		CappedReason: "escape-hatch",
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/scored")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var payload struct {
		Score *struct {
			Overall    float64 `json:"overall"`
			Grade      string  `json:"grade"`
			Dimensions []struct {
				Name   string  `json:"name"`
				Weight float64 `json:"weight"`
				Score  int     `json:"score"`
				Detail string  `json:"detail"`
			} `json:"dimensions"`
			CappedReason string `json:"cappedReason"`
			Evidence     struct {
				Deterministic int     `json:"deterministic"`
				AgentClaim    int     `json:"agentClaim"`
				Ratio         float64 `json:"ratio"`
			} `json:"evidence"`
		} `json:"score"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if payload.Score == nil {
		t.Fatal("带评分的任务 score 不得为 null")
	}
	if payload.Score.Overall != 90 || payload.Score.Grade != "A" {
		t.Errorf("score overall/grade 异常: %+v", payload.Score)
	}
	if len(payload.Score.Dimensions) != 2 || payload.Score.Dimensions[0].Name != "process" || payload.Score.Dimensions[0].Weight != 0.25 {
		t.Errorf("dimensions 异常: %+v", payload.Score.Dimensions)
	}
	if payload.Score.Evidence.Deterministic != 3 || payload.Score.Evidence.AgentClaim != 1 {
		t.Errorf("evidence 异常: %+v", payload.Score.Evidence)
	}
	if payload.Score.CappedReason != "escape-hatch" {
		t.Errorf("cappedReason = %q, want escape-hatch", payload.Score.CappedReason)
	}
}

// TestServe_PulseTask_Errors: missing ref → 400; unknown ref → 404.
//
// TestServe_PulseTask_Errors：缺 ref → 400；未知 ref → 404。
func TestServe_PulseTask_Errors(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	srv := pulseServer(t, Options{Root: root})

	code, _ := pulseGet(t, srv.URL+"/api/pulse/task.json")
	if code != 400 {
		t.Errorf("缺 ref 应 400，got %d", code)
	}
	code, _ = pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/nonexistent")
	if code != 404 {
		t.Errorf("未知 ref 应 404，got %d", code)
	}
}

// TestServe_PulseProjects: /api/pulse/projects.json lists each project with active/zombie
// counts and the last conclusion's grade/score.
//
// TestServe_PulseProjects：/api/pulse/projects.json 列出每个项目的活跃/僵尸计数与
// 最近结论的 grade/score。
func TestServe_PulseProjects(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	now := time.Now()
	// 一个进行中任务 + 一个僵尸分派。
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: "feat/active", Branch: "feat/active", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	stalled := &taskpipeline.TaskState{TaskRef: "feat/stalled", Branch: "feat/stalled", StartedAt: now.Add(-9 * 24 * time.Hour)}
	if err := stalled.AssignTo("kimi", "backend", "claude-code"); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-8 * 24 * time.Hour)
	stalled.Assignment.OfferedAt = &old
	if err := taskpipeline.SaveTaskState(root, stalled); err != nil {
		t.Fatal(err)
	}
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/done", Score: 88, Grade: "B", Strength: "Strong", CompletedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/projects.json")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var rows []struct {
		Key         string   `json:"key"`
		Name        string   `json:"name"`
		ActiveTasks int      `json:"activeTasks"`
		Zombies     int      `json:"zombies"`
		LastGrade   string   `json:"lastGrade"`
		LastScore   *float64 `json:"lastScore"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	r0 := rows[0]
	if r0.Key == "" || r0.Name == "" {
		t.Errorf("key/name 不得为空: %+v", r0)
	}
	if r0.ActiveTasks != 2 || r0.Zombies != 1 {
		t.Errorf("activeTasks/zombies = %d/%d, want 2/1", r0.ActiveTasks, r0.Zombies)
	}
	if r0.LastGrade != "B" || r0.LastScore == nil || *r0.LastScore != 88 {
		t.Errorf("lastGrade/lastScore 异常: %+v", r0)
	}
}

// TestServe_PulseStats: with no data the numeric fields are null (not 0); with conclusions
// they carry real aggregates.
//
// TestServe_PulseStats：无数据时数值字段为 null（非 0）；有结论时带真实聚合值。
func TestServe_PulseStats(t *testing.T) {
	// 空项目：数值字段 null。
	rootEmpty, _ := forgedatatest.RealProject(t)
	srv := pulseServer(t, Options{Root: rootEmpty})
	code, body := pulseGet(t, srv.URL+"/api/pulse/stats.json")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	for _, field := range []string{`"avgScore":null`, `"medianScore":null`, `"evidenceBlindRate":null`} {
		if !strings.Contains(string(body), field) {
			t.Errorf("空数据应输出 %s，body=%s", field, body)
		}
	}

	// 有数据：真实聚合。
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/a", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/b", Score: 60, Grade: "D", Strength: "Weak", RetrospectiveNudge: true,
		CompletedAt: base.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	srv2 := pulseServer(t, Options{Root: root})
	code, body = pulseGet(t, srv2.URL+"/api/pulse/stats.json")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var stats struct {
		Projects          int      `json:"projects"`
		ActiveTasks       int      `json:"activeTasks"`
		Zombies           int      `json:"zombies"`
		AvgScore          *float64 `json:"avgScore"`
		MedianScore       *float64 `json:"medianScore"`
		Trend             string   `json:"trend"`
		Alerts            int      `json:"alerts"`
		Nudges            int      `json:"nudges"`
		EvidenceBlindRate *float64 `json:"evidenceBlindRate"`
	}
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if stats.AvgScore == nil || *stats.AvgScore != 70 {
		t.Errorf("avgScore = %v, want 70", stats.AvgScore)
	}
	if stats.MedianScore == nil {
		t.Error("medianScore 不得为 null（有数据）")
	}
	if stats.Trend == "" {
		t.Error("trend 不得为空串")
	}
	if stats.Alerts != 1 { // 1 条 RetrospectiveNudge
		t.Errorf("alerts = %d, want 1", stats.Alerts)
	}
	if stats.Nudges != 1 { // alerts = zombies + nudges 的拆解分量
		t.Errorf("nudges = %d, want 1", stats.Nudges)
	}
	if stats.EvidenceBlindRate == nil || *stats.EvidenceBlindRate != 0.5 {
		t.Errorf("evidenceBlindRate = %v, want 0.5（Weak 1/2）", stats.EvidenceBlindRate)
	}
}

// TestServe_PulseSkills: /api/pulse/skills.json serves the overview with merged hits and
// the never-triggered list; the canonical dir is injected via FORGE_SKILLS_CANONICAL.
//
// TestServe_PulseSkills：/api/pulse/skills.json 输出总览（合并命中数 + 从未触发名单）；
// canonical 目录经 FORGE_SKILLS_CANONICAL 注入。
func TestServe_PulseSkills(t *testing.T) {
	opts, canonical, _ := skillsFixture(t)
	t.Setenv(skillscanonical.EnvName, canonical)
	srv := pulseServer(t, opts)

	code, body := pulseGet(t, srv.URL+"/api/pulse/skills.json")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var ov SkillsOverview
	if err := json.Unmarshal(body, &ov); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	alpha := findSkill(t, ov.Skills, "alpha")
	if alpha.Hits != 3 {
		t.Errorf("alpha hits = %d, want 3", alpha.Hits)
	}
	if len(ov.NeverTriggered) != 1 || ov.NeverTriggered[0] != "beta" {
		t.Errorf("neverTriggered = %v, want [beta]", ov.NeverTriggered)
	}
	if ov.Coverage == "" {
		t.Error("coverage 不得为空")
	}
}

// TestServe_PulseSkill: /api/pulse/skill.json serves the detail view; invalid names are
// rejected with 400 (path-traversal guard), liveFalsePositiveRate stays null.
//
// TestServe_PulseSkill：/api/pulse/skill.json 输出详情视图；非法名 400（路径遍历防护），
// liveFalsePositiveRate 保持 null。
func TestServe_PulseSkill(t *testing.T) {
	_, canonical, _ := skillsFixture(t)
	t.Setenv(skillscanonical.EnvName, canonical)
	// evalDir 走 ~/.pi/research/skill-eval——测试把 home 指到临时目录，直接在该处建 runs
	// fixture，让 handler 内的 skillseval.EvalDir() 解析到它。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1700000000, 0).UTC()
	writeEvalRuns(t, evalDir, "alpha", []skillseval.EvalRun{{
		RunID: "run-1", Skill: "alpha", Timestamp: base,
		ForgeVersion: "v1", AgentModel: "m1", DescHash: "dh1",
		Results: []skillseval.CaseResult{
			{CaseID: "t1", Kind: skillseval.KindTrigger, Pass: true},
			{CaseID: "n1", Kind: skillseval.KindNotTrigger, Pass: false},
		},
		HealthScore: 60,
	}})

	root, _ := forgedatatest.RealProject(t)
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/skill.json?name=alpha")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	if !strings.Contains(string(body), `"liveFalsePositiveRate":null`) {
		t.Errorf("liveFalsePositiveRate 应序列化为 null: %s", body)
	}
	var d SkillDetailView
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if d.Name != "alpha" || len(d.Runs) != 1 {
		t.Errorf("detail 异常: %+v", d)
	}
	if d.TriggerQuality == nil || d.TriggerQuality.FromRun != "run-1" {
		t.Errorf("triggerQuality 异常: %+v", d.TriggerQuality)
	}
	if d.TriggerQuality.NotTriggerAcc == nil || *d.TriggerQuality.NotTriggerAcc != 0 {
		t.Errorf("notTriggerAcc = %v, want 0", d.TriggerQuality.NotTriggerAcc)
	}

	code, _ = pulseGet(t, srv.URL+"/api/pulse/skill.json")
	if code != 400 {
		t.Errorf("缺 name 应 400，got %d", code)
	}
	code, _ = pulseGet(t, srv.URL+"/api/pulse/skill.json?name=..")
	if code != 400 {
		t.Errorf("路径遍历名应 400，got %d", code)
	}
}

// TestServe_PulseQuality: /api/pulse/quality.json aggregates every overview skill's
// triggerQuality + compare in one response (replacing the frontend's former N+1 fan-out);
// a skill without runs degrades to null sections.
//
// TestServe_PulseQuality：/api/pulse/quality.json 一次聚合总览中每个 skill 的
// triggerQuality + compare（替代前端此前的 N+1 扇出）；无 run 的 skill 降级为 null 段。
func TestServe_PulseQuality(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "alpha")
	writeCanonicalSkill(t, canonical, "beta")
	t.Setenv(skillscanonical.EnvName, canonical)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1700000000, 0).UTC()
	writeEvalRuns(t, evalDir, "alpha", []skillseval.EvalRun{
		{
			RunID: "run-1", Skill: "alpha", Timestamp: base,
			ForgeVersion: "v1", AgentModel: "m1", DescHash: "dh1",
			Results: []skillseval.CaseResult{
				{CaseID: "t1", Kind: skillseval.KindTrigger, Pass: true},
			},
			HealthScore: 100,
		},
		{
			RunID: "run-2", Skill: "alpha", Timestamp: base.Add(time.Hour),
			ForgeVersion: "v1", AgentModel: "m1", DescHash: "dh1", BaselineRunID: "run-1",
			Results: []skillseval.CaseResult{
				{CaseID: "t1", Kind: skillseval.KindTrigger, Pass: false}, // 退化
			},
			HealthScore: 50,
		},
	})
	if err := skillseval.SetBaseline(evalDir, "alpha", "run-1", "test"); err != nil {
		t.Fatal(err)
	}

	root, _ := forgedatatest.RealProject(t)
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/quality.json")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var views []SkillQualityView
	if err := json.Unmarshal(body, &views); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	byName := map[string]SkillQualityView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	alpha, ok := byName["alpha"]
	if !ok {
		t.Fatalf("quality 载荷缺 alpha: %+v", views)
	}
	if alpha.TriggerQuality == nil || alpha.TriggerQuality.FromRun != "run-2" {
		t.Errorf("alpha triggerQuality 异常: %+v", alpha.TriggerQuality)
	}
	if alpha.Compare == nil || !alpha.Compare.Comparable || alpha.Compare.NetRegressions != 1 {
		t.Errorf("alpha compare 应为 1 例净回归: %+v", alpha.Compare)
	}
	beta, ok := byName["beta"]
	if !ok {
		t.Fatalf("quality 载荷缺 beta（无 run 也应在列）: %+v", views)
	}
	if beta.TriggerQuality != nil || beta.Compare != nil {
		t.Errorf("无 run 的 beta 应降级为 null 段: %+v", beta)
	}
}

// TestServe_PulsePage: / serves the embedded static pulse page (200 + key markers) and
// overrides the global script-src 'none' CSP so the single-file inline <script> can run;
// the legacy /pulse URL redirects there; non-page routes keep the strict CSP.
//
// TestServe_PulsePage：/ 返回内嵌静态 pulse 页（200 + 关键标记），并覆盖全局
// script-src 'none' 的 CSP 使单文件内联 <script> 可运行；旧 /pulse 地址重定向到 /；
// 非页面路由保持严格 CSP。
func TestServe_PulsePage(t *testing.T) {
	root, _, _ := feedFixture(t)
	srv := pulseServer(t, Options{Root: root})

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	for _, marker := range []string{`Forge <span class="accent">Pulse</span>`, `/api/pulse/feed.json`, `data-theme`, `SKILL_FOLD_MIN`, `skill-fold`} {
		if !strings.Contains(string(body), marker) {
			t.Errorf("页面缺关键标记 %q", marker)
		}
	}
	csp := resp.Header.Get(`Content-Security-Policy`)
	if !strings.Contains(csp, `script-src 'unsafe-inline'`) {
		t.Errorf("pulse 页 CSP 应放行内联脚本，got %q", csp)
	}
	if strings.Contains(csp, `http:`) || strings.Contains(csp, `https:`) {
		t.Errorf("CSP 不得放行外部源，got %q", csp)
	}

	// Legacy /pulse URL: permanent redirect to / (http.Get follows; assert the landing).
	//
	// 旧 /pulse 地址：永久重定向到 /（http.Get 自动跟随；断言最终落点）。
	respP, err := http.Get(srv.URL + "/pulse")
	if err != nil {
		t.Fatal(err)
	}
	defer respP.Body.Close()
	if respP.StatusCode != 200 || respP.Request.URL.Path != `/` {
		t.Errorf("GET /pulse 应重定向到 /，got %s status %d", respP.Request.URL.Path, respP.StatusCode)
	}

	// Non-page routes keep the middleware's strict CSP.
	//
	// 非页面路由保持 middleware 的严格 CSP。
	resp2, err := http.Get(srv.URL + `/api/pulse/feed.json`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if csp2 := resp2.Header.Get(`Content-Security-Policy`); !strings.Contains(csp2, `script-src 'none'`) {
		t.Errorf("API 路由 CSP 应保持 script-src 'none'，got %q", csp2)
	}
}

// TestPulseCanonicalDir_EmbedFallback: without FORGE_SKILLS_CANONICAL the dir resolves to
// the embed cache under home (the production path — the skills tests above only cover the
// env-override branch). Cache present → cache dir; absent → "" (silent degrade, no error).
//
// TestPulseCanonicalDir_EmbedFallback：无 FORGE_SKILLS_CANONICAL 时解析到 home 下的
// embed 缓存（生产路径——上面的 skills 测试只覆盖 env 覆盖分支）。缓存存在 → 返回
// 缓存目录；缺失 → ""（静默降级，不报错）。
func TestPulseCanonicalDir_EmbedFallback(t *testing.T) {
	t.Setenv(skillscanonical.EnvName, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := pulseCanonicalDir(); got != "" {
		t.Errorf("缓存缺失时应降级为空串, got %q", got)
	}

	cache, err := skillscanonical.EmbeddedCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := pulseCanonicalDir(); got != cache {
		t.Errorf("缓存存在时 = %q, want %q", got, cache)
	}
}
