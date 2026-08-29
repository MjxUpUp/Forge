package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

// pulseTaskPayload 是 task.json 测试家族的共用解码目标（2026-08-30 瘦身合并原
// 6 个内联 payload struct）：任一 task.json 断言读取字段的并集——缺席的 JSON
// 字段解码为零值，各测试只看到自己那一片契约。taskRef/project/state/score/
// acceptance（TestServe_PulseTask）、dimensions/cappedReason/evidence
// （ScoredTask）、events/truncated（Truncated）、结论回填证据指针
// （EvidenceBackfill / NoFabrication）、state.lease/docReview（LeaseAndDocReview）。
type pulseTaskPayload struct {
	TaskRef   string      `json:"taskRef"`
	Project   string      `json:"project"`
	Events    []FeedEvent `json:"events"`
	Truncated bool        `json:"truncated"`
	State     struct {
		CurrentGate  string    `json:"currentGate"`
		StartedAt    time.Time `json:"startedAt"`
		OriginTool   string    `json:"originTool"`
		Zombie       bool      `json:"zombie"`
		GateProgress struct {
			Passed int `json:"passed"`
			Total  int `json:"total"`
		} `json:"gateProgress"`
		Lease *struct {
			HolderNode string    `json:"holderNode"`
			Active     bool      `json:"active"`
			ExpiresAt  time.Time `json:"expiresAt"`
			Fencing    int64     `json:"fencing"`
			TTLSec     int64     `json:"ttlSec"`
		} `json:"lease"`
	} `json:"state"`
	Score *struct {
		Overall        float64 `json:"overall"`
		Grade          string  `json:"grade"`
		FromConclusion bool    `json:"fromConclusion"`
		CappedReason   string  `json:"cappedReason"`
		Dimensions     []struct {
			Name   string  `json:"name"`
			Weight float64 `json:"weight"`
			Score  int     `json:"score"`
			Detail string  `json:"detail"`
		} `json:"dimensions"`
		Evidence *struct {
			Deterministic int     `json:"deterministic"`
			AgentClaim    int     `json:"agentClaim"`
			Ratio         float64 `json:"ratio"`
			Strength      string  `json:"strength"`
		} `json:"evidence"`
	} `json:"score"`
	Acceptance struct {
		Pass  int `json:"pass"`
		Total int `json:"total"`
	} `json:"acceptance"`
	DocReview *struct {
		Passed      bool `json:"passed"`
		RubricScore int  `json:"rubricScore"`
		Round       int  `json:"round"`
		RoundsTotal int  `json:"roundsTotal"`
	} `json:"docReview"`
}

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
	var payload pulseTaskPayload
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
	var payload pulseTaskPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if payload.Score == nil {
		t.Fatal("带评分的任务 score 不得为 null")
	}
	if payload.Score.Overall != 90 || payload.Score.Grade != "A" {
		t.Errorf("score overall/grade 异常: %+v", payload.Score)
	}
	// 权重来自 scoringtypes.DefaultWeights()（pulse API 按维度名重新推导），
	// 断言用同一真相源——写死 0.25 在新增 expression 维度（权重重平衡）时断裂。
	if len(payload.Score.Dimensions) != 2 || payload.Score.Dimensions[0].Name != "process" || payload.Score.Dimensions[0].Weight != scoringtypes.DefaultWeights()[string(scoringtypes.DimensionProcess)] {
		t.Errorf("dimensions 异常: %+v", payload.Score.Dimensions)
	}
	if payload.Score.Evidence.Deterministic != 3 || payload.Score.Evidence.AgentClaim != 1 {
		t.Errorf("evidence 异常: %+v", payload.Score.Evidence)
	}
	if payload.Score.CappedReason != "escape-hatch" {
		t.Errorf("cappedReason = %q, want escape-hatch", payload.Score.CappedReason)
	}
}

// TestServe_PulseTask_Truncated：事件流超出 feed 上限的任务，transcript 被截断且载荷
// 标 truncated:true——与 feed.json 对齐，详情页得以如实标注「不完整」而不是静默冒充
// 完整序列（回归守卫：buildPulseTask 曾丢弃 FeedResult.Truncated）。
func TestServe_PulseTask_Truncated(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	st := &taskpipeline.TaskState{
		TaskRef: "feat/long", Branch: "feat/long", StartedAt: base,
	}
	// 1 条 task-start + defaultFeedLimit+10 条 gate 事件 = 超 defaultFeedLimit → 必截断。
	for i := 0; i < defaultFeedLimit+10; i++ {
		st.History = append(st.History, taskpipeline.TaskGateResult{
			Gate: "task-verify", Passed: true, CompletedAt: base.Add(time.Duration(i+1) * time.Minute),
		})
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	// 恰好等于上限的对照任务：1 条 task-start + defaultFeedLimit-1 条 gate = defaultFeedLimit
	// 条整 → 不截断（钉死 `len > limit` 而非 `>=` 的边界）。
	exact := &taskpipeline.TaskState{
		TaskRef: "feat/exact", Branch: "feat/exact", StartedAt: base,
	}
	for i := 0; i < defaultFeedLimit-1; i++ {
		exact.History = append(exact.History, taskpipeline.TaskGateResult{
			Gate: "task-verify", Passed: true, CompletedAt: base.Add(time.Duration(i+1) * time.Minute),
		})
	}
	if err := taskpipeline.SaveTaskState(root, exact); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/long")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var payload pulseTaskPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(payload.Events) != defaultFeedLimit {
		t.Errorf("events len = %d, want %d（默认上限截断）", len(payload.Events), defaultFeedLimit)
	}
	if !payload.Truncated {
		t.Error("truncated = false, want true——截断必须暴露给前端")
	}
	// 钉死截断保留哪一端：降序取最新 defaultFeedLimit 条（对 transcript 是升序渲染的
	// 前提，截错方向 = 灾难性语义反转，仅断言 len+truncated 抓不住）。
	if len(payload.Events) > 0 && !payload.Events[0].Time.Equal(base.Add(time.Duration(defaultFeedLimit+10)*time.Minute)) {
		t.Errorf("events[0].time = %v, want 最新端 +%dmin（截断必须保留最新事件）",
			payload.Events[0].Time, defaultFeedLimit+10)
	}
	if len(payload.Events) >= defaultFeedLimit && !payload.Events[defaultFeedLimit-1].Time.Equal(base.Add(11*time.Minute)) {
		t.Errorf("events[%d].time = %v, want 窗口边界 +11min（+1..+10min 与 task-start 被截掉）",
			defaultFeedLimit-1, payload.Events[defaultFeedLimit-1].Time)
	}
	for i, e := range payload.Events {
		if e.TaskRef != "feat/long" {
			t.Fatalf("events[%d].taskRef = %q, want feat/long（taskRef 过滤失效）", i, e.TaskRef)
		}
	}

	code, body = pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/exact")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(payload.Events) != defaultFeedLimit {
		t.Errorf("恰好等于上限: events len = %d, want %d", len(payload.Events), defaultFeedLimit)
	}
	if payload.Truncated {
		t.Error("恰好等于上限: truncated = true, want false（len > limit 而非 >=）")
	}
}

// TestServe_PulseTask_EvidenceBackfill: a scored task whose ScoreResult.Evidence is nil
// (buildEvidenceSummary returns nil when det+claim=0 — legitimate for tasks scored
// before evidence collection) still gets the evidence block backfilled from its act
// conclusion; otherwise the detail page shows no 证据链 while its own conclusion event
// in the transcript below carries det/claim counts — self-contradiction, the same bug
// class the FromConclusion score backfill fixed. FromConclusion must stay false: the
// score itself is real, only the evidence block is degraded.
//
// TestServe_PulseTask_EvidenceBackfill：ScoreResult.Evidence 为 nil 的已评分任务
// （det+claim=0 时 buildEvidenceSummary 合法地返回 nil——证据采集上线前评分的存量
// 任务）仍须从 act 结论回填证据块；否则详情页评分块无证据链，而下方 transcript 里
// 自己的 conclusion 事件却带着 det/claim 计数——自相矛盾，与 FromConclusion 评分
// 回填修的是同一类 bug。FromConclusion 须保持 false：评分本身是真的，仅证据块降级。
func TestServe_PulseTask_EvidenceBackfill(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	st := &taskpipeline.TaskState{
		TaskRef: "feat/noevi", Branch: "feat/noevi", StartedAt: base,
		Score: &scoringtypes.ScoreResult{
			TaskRef: "feat/noevi", Overall: 72, Grade: "C", ScoredAt: base,
			Dimensions: []scoringtypes.DimensionScore{
				{Dimension: scoringtypes.DimensionProcess, Score: 70, Detail: "部分门禁"},
			},
			Evidence: nil, // 评分时无证据输入——合法 nil
		},
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/noevi", Score: 72, Grade: "C", Strength: "Weak",
		Deterministic: 5, AgentClaim: 3, Ratio: 0.625,
		CompletedAt: base.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/noevi")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var payload pulseTaskPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if payload.Score == nil {
		t.Fatal("score 不得为 null")
	}
	if payload.Score.FromConclusion {
		t.Error("fromConclusion 须为 false——评分真实存在，仅证据块回填")
	}
	if payload.Score.Evidence == nil {
		t.Fatal("evidence 须从结论回填，不得为 null（详情页否则自相矛盾）")
	}
	ev := payload.Score.Evidence
	if ev.Deterministic != 5 || ev.AgentClaim != 3 || ev.Strength != "Weak" || ev.Ratio != 0.625 {
		t.Errorf("回填 evidence 异常: %+v", ev)
	}
}

// TestServe_PulseTask_NoEvidenceNoFabrication: the anti-fabrication guard — a scored
// task whose conclusion carries zero evidence (det=0, claim=0, Strength="NoData"; the
// only shape BuildConclusion emits for zero evidence — Strength is never empty in
// production) must NOT get a fabricated all-zero evidence block: score.evidence stays
// null and the frontend shows「无证据记录」honestly. Regression guard for the former
// always-true `Strength != ""` disjunct that made this skip path unreachable in
// production, and against "simplifying" the backfill into an unconditional one.
//
// TestServe_PulseTask_NoEvidenceNoFabrication：反编造守卫——结论零证据（det=0、
// claim=0、Strength="NoData"；BuildConclusion 对零证据的唯一生产形态——生产里
// Strength 永非空）的已评分任务不得被编造全零证据块：score.evidence 保持 null，
// 前端如实显示「无证据记录」。回归守卫：曾有的 `Strength != ""` 恒真析取项让本
// skip 路径生产不可达；也防回填分支被「简化」成无条件回填。
func TestServe_PulseTask_NoEvidenceNoFabrication(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	st := &taskpipeline.TaskState{
		TaskRef: "feat/noevi-guard", Branch: "feat/noevi-guard", StartedAt: base,
		Score: &scoringtypes.ScoreResult{
			TaskRef: "feat/noevi-guard", Overall: 72, Grade: "C", ScoredAt: base,
			Dimensions: []scoringtypes.DimensionScore{
				{Dimension: scoringtypes.DimensionProcess, Score: 70, Detail: "部分门禁"},
			},
			Evidence: nil, // 评分时无证据输入——合法 nil
		},
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/noevi-guard", Score: 72, Grade: "C", Strength: "NoData",
		Deterministic: 0, AgentClaim: 0, Ratio: 0,
		CompletedAt: base.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/noevi-guard")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var payload pulseTaskPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if payload.Score == nil {
		t.Fatal("score 不得为 null——评分真实存在")
	}
	if payload.Score.Evidence != nil {
		t.Errorf("evidence 须保持 null（零证据结论不得回填编造的全零块），实际: %+v", payload.Score.Evidence)
	}
	if payload.Score.FromConclusion {
		t.Error("fromConclusion 须为 false——评分真实存在，不走结论降级回填")
	}
}

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

	// 有数据：真实聚合。时间用 now 相对偏移：nudges 是 14 天窗口计数（2026-08 校准，
	// 防"告警只增不减"），2023 固定戳会被窗口过滤——用近期时间让样本落进窗口。
	root, p := forgedatatest.RealProject(t)
	now := time.Now()
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/a", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/b", Score: 60, Grade: "D", Strength: "Weak", RetrospectiveNudge: true,
		CompletedAt: now.Add(-1 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// 窗口外（15 天前）的 nudge：计 alerts 不应计入——钉住窗口在 API 面生效。
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/stale", Score: 60, Grade: "D", Strength: "Weak", RetrospectiveNudge: true,
		CompletedAt: now.Add(-15 * 24 * time.Hour),
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
	if stats.AvgScore == nil || *stats.AvgScore < 66.6 || *stats.AvgScore > 66.7 {
		t.Errorf("avgScore = %v, want ≈66.67（(80+60+60)/3）", stats.AvgScore)
	}
	if stats.MedianScore == nil || *stats.MedianScore != 60 {
		t.Errorf("medianScore = %v, want 60", stats.MedianScore)
	}
	if stats.Trend == "" {
		t.Error("trend 不得为空串")
	}
	if stats.Alerts != 1 { // 1 条窗口内 RetrospectiveNudge（stale 的被窗口滤除）
		t.Errorf("alerts = %d, want 1", stats.Alerts)
	}
	if stats.Nudges != 1 { // alerts = zombies + nudges 的拆解分量（窗口内）
		t.Errorf("nudges = %d, want 1", stats.Nudges)
	}
	if stats.EvidenceBlindRate == nil || *stats.EvidenceBlindRate < 0.65 || *stats.EvidenceBlindRate > 0.67 {
		t.Errorf("evidenceBlindRate = %v, want ≈0.66（Weak 2/3，stale 也计入全量盲区率）", stats.EvidenceBlindRate)
	}
}

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

// TestServe_PulseSkill：/api/pulse/skill.json 输出详情视图；非法名 400（路径遍历防护）。
func TestServe_PulseSkill(t *testing.T) {
	_, canonical, _ := skillsFixture(t)
	t.Setenv(skillscanonical.EnvName, canonical)
	// eval 根走 GlobalHome 解析（FORGE_DATA_HOME 优先于 HOME）——而 RealProject 在夹具
	// 写入之后才 t.Setenv FORGE_DATA_HOME，若只设 HOME 会让夹具目录与 handler 目录分叉。
	// 用 FORGE_EVAL_DIR 钉死，两端解析恒一致（顺带覆盖 env override 路径）。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(skillseval.EnvDirName, filepath.Join(home, "evals"))
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
	// 同 TestServe_PulseSkill：FORGE_DATA_HOME 由 RealProject 后置设置，必须用
	// FORGE_EVAL_DIR 钉死 eval 根，夹具与 handler 才解析到同一目录。
	t.Setenv(skillseval.EnvDirName, filepath.Join(home, "evals"))
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

	// 旧 /pulse 地址：永久重定向到 /（http.Get 自动跟随；断言最终落点）。
	respP, err := http.Get(srv.URL + "/pulse")
	if err != nil {
		t.Fatal(err)
	}
	defer respP.Body.Close()
	if respP.StatusCode != 200 || respP.Request.URL.Path != `/` {
		t.Errorf("GET /pulse 应重定向到 /，got %s status %d", respP.Request.URL.Path, respP.StatusCode)
	}

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

// TestServe_PulseTask_LeaseAndDocReview：state 块投影跨机租约（持有者/有效/过期时刻/
// fencing——「过期即自由」规则不在面板侧另造），载荷带 doc gate 的 L2 回检证据。
// 两者皆无的任务在线上结构中完全没有 lease/docReview 键（omitempty——与
// FeedEvent.Node 同一条兼容纪律，在原始 body 层断言，omitempty 被删则测试红）。
func TestServe_PulseTask_LeaseAndDocReview(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	base := time.Now().UTC() // 租约须在请求时点未过期（TTL 4h 足够覆盖测试运行）
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: "feat/lease", Branch: "feat/lease", StartedAt: base, CurrentGate: "task-implement",
		Lease: &taskpipeline.Lease{
			HolderNode: "fnode_abc123abc123abc123abc123abc12345", TTLSec: 4 * 3600,
			Fencing: 3, ClaimedAt: base.UnixMilli(),
		},
		DocReview: &taskpipeline.DocReview{
			Passed: true, RubricScore: 92, Round: 2, ReviewedAt: base,
		},
		DocReviewHistory: []taskpipeline.DocReview{
			{Passed: false, RubricScore: 61, Round: 1, ReviewedAt: base.Add(-time.Hour)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// 对照组：无租约、无回检的存量形态任务。
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: "feat/plain", Branch: "feat/plain", StartedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})

	code, body := pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/lease")
	if code != 200 {
		t.Fatalf("status = %d, want 200: %s", code, body)
	}
	var payload pulseTaskPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	l := payload.State.Lease
	if l == nil {
		t.Fatalf("lease 块缺失: %s", body)
	}
	if l.HolderNode != "fnode_abc123abc123abc123abc123abc12345" || !l.Active || l.Fencing != 3 || l.TTLSec != 4*3600 {
		t.Errorf("lease 投影异常: %+v", l)
	}
	if want := base.Add(4 * time.Hour); l.ExpiresAt.Sub(want) > time.Second || want.Sub(l.ExpiresAt) > time.Second {
		t.Errorf("expiresAt = %s, want ≈%s（claimed+TTL 单一公式）", l.ExpiresAt, want)
	}
	dr := payload.DocReview
	if dr == nil {
		t.Fatalf("docReview 块缺失: %s", body)
	}
	if !dr.Passed || dr.RubricScore != 92 || dr.Round != 2 || dr.RoundsTotal != 2 {
		t.Errorf("docReview 投影异常: %+v（roundsTotal 应含历史轮次 1+1）", dr)
	}

	code, body = pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/plain")
	if code != 200 {
		t.Fatalf("plain status = %d, want 200: %s", code, body)
	}
	if strings.Contains(string(body), `"lease"`) || strings.Contains(string(body), `"docReview"`) {
		t.Errorf("存量任务的线上结构被改变（lease/docReview 键应缺席）: %s", body)
	}

	// --round 跳号（合法手动覆盖）：Round=5 而历史仅 1 轮时，roundsTotal 钳制到
	// ≥ Round——否则面板自相矛盾（「第 5 轮 · 累计 2 轮」）。
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: "feat/skip", Branch: "feat/skip", StartedAt: base,
		DocReview:        &taskpipeline.DocReview{Passed: true, RubricScore: 88, Round: 5, ReviewedAt: base},
		DocReviewHistory: []taskpipeline.DocReview{{Passed: false, RubricScore: 70, Round: 1, ReviewedAt: base.Add(-time.Hour)}},
	}); err != nil {
		t.Fatal(err)
	}
	code, body = pulseGet(t, srv.URL+"/api/pulse/task.json?ref=feat/skip")
	if code != 200 {
		t.Fatalf("skip status = %d, want 200: %s", code, body)
	}
	var skipPayload pulseTaskPayload
	if err := json.Unmarshal(body, &skipPayload); err != nil {
		t.Fatalf("decode skip: %v\n%s", err, body)
	}
	if skipPayload.DocReview == nil || skipPayload.DocReview.RoundsTotal != 5 || skipPayload.DocReview.Round != 5 {
		t.Errorf("roundsTotal 钳制异常: %+v（--round 跳号时须 ≥ Round）", skipPayload.DocReview)
	}
}

// TestServe_PulseProjects_Sync：projects.json 行在已绑定时携带机器本地同步块
// （remote/nodeId/lastPushAt/lastPullAt——DataDir/sync-remote.json 的 camelCase
// 投影），未绑定时完全没有 "sync" 键（omitempty；线上结构钉在原始 body 层，
// omitempty 被删则测试红）。
func TestServe_PulseProjects_Sync(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	if err := os.MkdirAll(p.DataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.DataDir, `sync-remote.json`), []byte(
		`{"remote":"git@example.com:org/sync.git","node_id":"fnode_abc123abc123abc123abc123abc12345","last_push_at":"2026-08-26T03:00:00Z","last_pull_at":"2026-08-26T04:00:00Z"}`,
	), 0600); err != nil {
		t.Fatal(err)
	}
	srv := pulseServer(t, Options{Root: root})
	code, body := pulseGet(t, srv.URL+"/api/pulse/projects.json")
	if code != 200 {
		t.Fatalf("status = %d: %s", code, body)
	}
	var rows []struct {
		Key  string `json:"key"`
		Sync *struct {
			Remote     string `json:"remote"`
			NodeID     string `json:"nodeId"`
			LastPushAt string `json:"lastPushAt"`
			LastPullAt string `json:"lastPullAt"`
		} `json:"sync"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(rows) != 1 || rows[0].Sync == nil {
		t.Fatalf("sync 块缺失: %s", body)
	}
	s := rows[0].Sync
	if s.Remote != "git@example.com:org/sync.git" || s.NodeID != "fnode_abc123abc123abc123abc123abc12345" ||
		s.LastPushAt != "2026-08-26T03:00:00Z" || s.LastPullAt != "2026-08-26T04:00:00Z" {
		t.Errorf("sync 投影异常: %+v", s)
	}

	// 未绑定项目：sync-remote.json 缺席 → 线上结构无 "sync" 键。
	root2, _ := forgedatatest.RealProject(t)
	srv2 := pulseServer(t, Options{Root: root2})
	code, body2 := pulseGet(t, srv2.URL+"/api/pulse/projects.json")
	if code != 200 {
		t.Fatalf("unbound status = %d: %s", code, body2)
	}
	if strings.Contains(string(body2), `"sync"`) {
		t.Errorf("未绑定项目的线上结构被改变（sync 键应缺席）: %s", body2)
	}
}

// TestPulseDocReview_ReviewedAtOmitEmpty 钉住 pulseDocReview.ReviewedAt 的 JSON
// 契约：time.Time 的 omitempty 对零值无效（仍序列化 0001-01-01），故字段用
// *time.Time——零值评审时刻须序列化为缺席而非假日期；有值时刻须出现。
func TestPulseDocReview_ReviewedAtOmitEmpty(t *testing.T) {
	zero := pulseDocReview{Passed: true, RubricScore: 80, Round: 1, RoundsTotal: 1}
	data, err := json.Marshal(zero)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `reviewedAt`) {
		t.Fatalf(`零值 ReviewedAt 须缺席而非假日期: %s`, data)
	}
	ts := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	withTs := zero
	withTs.ReviewedAt = &ts
	data, err = json.Marshal(withTs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `reviewedAt`) {
		t.Fatalf(`有值 ReviewedAt 须出现在序列化结果: %s`, data)
	}
}
