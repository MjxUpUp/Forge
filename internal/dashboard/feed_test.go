package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// writeChecklogEntries appends entries as JSONL to DataDir/checklog.jsonl — the same file
// checklog.LoadAllAll globs (checklog*.jsonl). Direct write keeps fixtures deterministic
// (Record stamps time.Now, fixtures need fixed timestamps).
//
// writeChecklogEntries 把条目以 JSONL 追加到 DataDir/checklog.jsonl——checklog.LoadAllAll
// glob 的同一文件（checklog*.jsonl）。直接写保证 fixture 时间戳确定（Record 会盖 time.Now）。
func writeChecklogEntries(t *testing.T, dataDir string, entries []checklog.Entry) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dataDir, `checklog.jsonl`), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// feedFixture builds one project with a full event set: task-start (t0), gate pass (t1,
// implement, head commit), gate fail (t2, verify), skill-trigger (t3), conclusion (t4, A/92).
//
// feedFixture 构造一个带全套事件的项目：task-start（t0）、gate 通过（t1，implement，
// 带 head commit）、gate 失败（t2，verify）、skill-trigger（t3）、结论（t4，A/92）。
func feedFixture(t *testing.T) (root string, dataDir string, base time.Time) {
	t.Helper()
	root, p := forgedatatest.RealProject(t)
	base = time.Unix(1700000000, 0).UTC()

	st := &taskpipeline.TaskState{
		TaskRef: "feat/x", Branch: "feat/x", OriginTool: "pi",
		StartedAt: base,
		History: []taskpipeline.TaskGateResult{
			{Gate: "task-implement", Passed: true, CompletedAt: base.Add(time.Hour), HeadCommit: "abc1234567890"},
			{Gate: "task-verify", Passed: false, CompletedAt: base.Add(2 * time.Hour)},
		},
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	writeChecklogEntries(t, p.DataDir, []checklog.Entry{{
		Check: checklog.CheckSkillTrigger, Passed: true, Checked: true,
		TaskRef:    "feat/x",
		Detail:     checklog.DetailForSkillTrigger("code-review-gate", "PostToolUse", "edit .go"),
		RecordedAt: base.Add(3 * time.Hour),
	}})
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/x", Score: 92, Grade: "A", Strength: "Strong",
		Deterministic: 5, AgentClaim: 1, AcceptancePass: 2, AcceptanceTotal: 3,
		CompletedAt: base.Add(4 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return root, p.DataDir, base
}

func feedKinds(events []FeedEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}

// TestAggregateFeed_MergeSortsDesc: the four sources merge into one stream, sorted by time
// descending (most recent first), each event carrying kind/project/taskRef/severity.
//
// TestAggregateFeed_MergeSortsDesc：四源归并成单一流，按时间降序（最近在前），
// 每条带 kind/project/taskRef/severity。
func TestAggregateFeed_MergeSortsDesc(t *testing.T) {
	root, _, base := feedFixture(t)
	res, err := AggregateFeed(Options{Root: root}, base.Add(5*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	events := res.Events
	if res.Truncated {
		t.Error("5 条事件不应触发截断")
	}
	want := []string{"conclusion", "skill-trigger", "gate", "gate", "task-start"}
	if len(events) != len(want) {
		t.Fatalf("events len = %d, want %d: %v", len(events), len(want), feedKinds(events))
	}
	for i, k := range want {
		if events[i].Kind != k {
			t.Fatalf("events[%d].Kind = %q, want %q（全序 %v）", i, events[i].Kind, k, feedKinds(events))
		}
	}
	// Strictly non-increasing times.
	for i := 1; i < len(events); i++ {
		if events[i].Time.After(events[i-1].Time) {
			t.Errorf("events 未按时间降序: [%d]=%v 晚于 [%d]=%v", i, events[i].Time, i-1, events[i-1].Time)
		}
	}
	for _, e := range events {
		if e.Project == "" || e.TaskRef != "feat/x" {
			t.Errorf("事件缺 project/taskRef: %+v", e)
		}
	}
}

// TestAggregateFeed_EventFields pins the per-kind field contract the frontend consumes:
// gate carries gate/passed/commit (+retry detail), conclusion carries grade/score + evidence
// detail, task-start carries origin tool + gate progress in the title.
//
// TestAggregateFeed_EventFields 钉住前端消费的 per-kind 字段契约：gate 带
// gate/passed/commit（+retry detail），conclusion 带 grade/score + 证据 detail，
// task-start 标题带 origin tool 与 gate 进度。
func TestAggregateFeed_EventFields(t *testing.T) {
	root, _, base := feedFixture(t)
	res, err := AggregateFeed(Options{Root: root}, base.Add(5*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	events := res.Events
	find := func(kind string, passed *bool) FeedEvent {
		t.Helper()
		for _, e := range events {
			if e.Kind != kind {
				continue
			}
			if passed == nil || (e.Passed != nil && *e.Passed == *passed) {
				return e
			}
		}
		t.Fatalf("找不到 kind=%q passed=%v 的事件: %v", kind, passed, feedKinds(events))
		return FeedEvent{}
	}

	tr := true
	fa := false
	gatePass := find("gate", &tr)
	if gatePass.Gate != "implement" || gatePass.Severity != "ok" {
		t.Errorf("implement gate 事件异常: %+v", gatePass)
	}
	if gatePass.Commit != "abc1234" {
		t.Errorf("Commit = %q, want 短哈希 abc1234", gatePass.Commit)
	}
	gateFail := find("gate", &fa)
	if gateFail.Gate != "verify" || gateFail.Severity != "fail" {
		t.Errorf("verify gate 失败事件异常: %+v", gateFail)
	}

	start := find("task-start", nil)
	if start.Severity != "info" {
		t.Errorf("进行中 task-start severity = %q, want info", start.Severity)
	}
	if !strings.Contains(start.Title, "feat/x") || !strings.Contains(start.Title, "pi") || !strings.Contains(start.Title, "gate 1/3") {
		t.Errorf("task-start 标题应含 ref + origin tool + gate 进度（1/3）: %q", start.Title)
	}

	st := find("skill-trigger", nil)
	if st.Severity != "info" || !strings.Contains(st.Detail, "code-review-gate") {
		t.Errorf("skill-trigger 事件异常: %+v", st)
	}
	if st.Skill != "code-review-gate" {
		t.Errorf("skill-trigger Skill = %q, want code-review-gate（结构化字段——前端聚合不得反解 title）", st.Skill)
	}

	con := find("conclusion", nil)
	if con.Grade != "A" || con.Score != 92 || con.Severity != "ok" {
		t.Errorf("conclusion 事件异常: %+v", con)
	}
	if !strings.Contains(con.Detail, "Strong") || !strings.Contains(con.Detail, "2/3") {
		t.Errorf("conclusion Detail 应带证据强度与验收 x/y: %q", con.Detail)
	}
}

// TestAggregateFeed_SinceFilter: since returns only events with Time strictly after it
// (polling increment semantics — a since equal to an event's time must not re-emit it).
//
// TestAggregateFeed_SinceFilter：since 只返回 Time 严格晚于它的事件（轮询增量语义——
// since 等于某事件时间时该事件不得重发）。
func TestAggregateFeed_SinceFilter(t *testing.T) {
	root, _, base := feedFixture(t)
	// t2 = base+2h (gate fail): strictly-after must keep only t3 (skill-trigger) + t4 (conclusion).
	events, err := AggregateFeed(Options{Root: root}, base.Add(5*time.Hour), FeedQuery{Since: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 2 || events.Events[0].Kind != "conclusion" || events.Events[1].Kind != "skill-trigger" {
		t.Fatalf("since 过滤结果异常: %v", feedKinds(events.Events))
	}
}

// TestAggregateFeed_ProjectFilter: filtering by project name or by forge key both scope the
// stream to that project only.
//
// TestAggregateFeed_ProjectFilter：按项目名或 forge key 过滤都把流限定到该项目。
func TestAggregateFeed_ProjectFilter(t *testing.T) {
	rootA, pA := forgedatatest.RealProject(t)
	rootB, pB := forgedatatest.RealProject(t)
	now := time.Now()
	for _, r := range []string{rootA, rootB} {
		if err := taskpipeline.SaveTaskState(r, &taskpipeline.TaskState{
			TaskRef: "feat/t", Branch: "feat/t", StartedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Roots: []string{rootA, rootB}}

	all, err := AggregateFeed(opts, now, FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Events) != 2 {
		t.Fatalf("不过滤应 2 条（两项目各一 task-start），got %d", len(all.Events))
	}

	byName, err := AggregateFeed(opts, now, FeedQuery{Project: projectName(rootA)})
	if err != nil {
		t.Fatal(err)
	}
	if len(byName.Events) != 1 || byName.Events[0].Project != projectName(rootA) {
		t.Errorf("按名过滤异常: %+v", byName.Events)
	}

	byKey, err := AggregateFeed(opts, now, FeedQuery{Project: pA.Key})
	if err != nil {
		t.Fatal(err)
	}
	if len(byKey.Events) != 1 || byKey.Events[0].Project != projectName(rootA) {
		t.Errorf("按 key 过滤应命中项目 A: %+v", byKey.Events)
	}
	_ = pB
}

// TestAggregateFeed_GlobalMergesProjects: Roots mode merges both projects and each event
// carries its own project attribution.
//
// TestAggregateFeed_GlobalMergesProjects：Roots 模式归并两项目，各事件带自己的项目归属。
func TestAggregateFeed_GlobalMergesProjects(t *testing.T) {
	rootA, _ := forgedatatest.RealProject(t)
	rootB, _ := forgedatatest.RealProject(t)
	now := time.Now()
	if err := taskpipeline.SaveTaskState(rootA, &taskpipeline.TaskState{TaskRef: "feat/a", Branch: "feat/a", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SaveTaskState(rootB, &taskpipeline.TaskState{TaskRef: "feat/b", Branch: "feat/b", StartedAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	events, err := AggregateFeed(Options{Roots: []string{rootA, rootB}}, now, FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 2 {
		t.Fatalf("跨项目归并应 2 条，got %d", len(events.Events))
	}
	projects := map[string]bool{}
	for _, e := range events.Events {
		projects[e.Project] = true
	}
	if !projects[projectName(rootA)] || !projects[projectName(rootB)] {
		t.Errorf("事件应分属两项目: %+v", projects)
	}
}

// TestAggregateFeed_ZombieSeverity: a stalled delegation (offered>7d) projects as
// task-start severity=warn with the zombie duration marked in the title.
//
// TestAggregateFeed_ZombieSeverity：停滞分派（offered>7d）投影为 task-start
// severity=warn，标题标注僵尸时长。
func TestAggregateFeed_ZombieSeverity(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	now := time.Now()
	stalled := &taskpipeline.TaskState{TaskRef: "feat/stalled", Branch: "feat/stalled", StartedAt: now.Add(-9 * 24 * time.Hour)}
	if err := stalled.AssignTo("kimi", "backend", "claude-code"); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-8 * 24 * time.Hour)
	stalled.Assignment.OfferedAt = &old
	if err := taskpipeline.SaveTaskState(root, stalled); err != nil {
		t.Fatal(err)
	}
	events, err := AggregateFeed(Options{Root: root}, now, FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 {
		t.Fatalf("应 1 条 task-start，got %v", feedKinds(events.Events))
	}
	e := events.Events[0]
	if e.Severity != "warn" {
		t.Errorf("僵尸任务 severity = %q, want warn", e.Severity)
	}
	if !strings.Contains(e.Title, "僵尸") || !strings.Contains(e.Title, "8d") {
		t.Errorf("僵尸标题应含「僵尸」与时长大致 8d: %q", e.Title)
	}
}

// TestAggregateFeed_ConclusionSeverityMap pins the grade→severity mapping:
// A/B→ok, C→info, D→warn, F→fail.
//
// TestAggregateFeed_ConclusionSeverityMap 钉住 grade→severity 映射：
// A/B→ok、C→info、D→warn、F→fail。
func TestAggregateFeed_ConclusionSeverityMap(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	grades := []struct {
		grade string
		want  string
	}{
		{"A", "ok"}, {"B", "ok"}, {"C", "info"}, {"D", "warn"}, {"F", "fail"},
	}
	for i, g := range grades {
		if err := act.Append(p, &act.Conclusion{
			TaskRef: "feat/" + g.grade, Grade: g.grade, Score: 50, Strength: "Strong",
			CompletedAt: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := AggregateFeed(Options{Root: root}, base.Add(6*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range events.Events {
		if e.Kind == "conclusion" {
			got[e.Grade] = e.Severity
		}
	}
	for _, g := range grades {
		if got[g.grade] != g.want {
			t.Errorf("grade %s severity = %q, want %q", g.grade, got[g.grade], g.want)
		}
	}
}

// TestAggregateFeed_GateRetryDetail: a gate that failed once then passed carries retry info
// in Detail.
//
// TestAggregateFeed_GateRetryDetail：先败后过的 gate 在 Detail 带 retry 信息。
func TestAggregateFeed_GateRetryDetail(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	st := &taskpipeline.TaskState{
		TaskRef: "feat/retry", Branch: "feat/retry", StartedAt: base,
		History: []taskpipeline.TaskGateResult{
			{Gate: "task-verify", Passed: false, CompletedAt: base.Add(time.Hour)},
			{Gate: "task-verify", Passed: true, CompletedAt: base.Add(2 * time.Hour)},
		},
	}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	events, err := AggregateFeed(Options{Root: root}, base.Add(3*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var retried *FeedEvent
	for i := range events.Events {
		if events.Events[i].Kind == "gate" && events.Events[i].Passed != nil && *events.Events[i].Passed {
			retried = &events.Events[i]
		}
	}
	if retried == nil {
		t.Fatalf("应有 verify 通过的 gate 事件: %v", feedKinds(events.Events))
	}
	if !strings.Contains(retried.Detail, "2") {
		t.Errorf("重试通过的 gate Detail 应含第 2 次尝试信息: %q", retried.Detail)
	}
}

// TestAggregateFeed_TaskRefFilter scopes the stream to one task (task.json reuses this).
//
// TestAggregateFeed_TaskRefFilter 把流限定到单个 task（task.json 复用本过滤）。
func TestAggregateFeed_TaskRefFilter(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	for _, ref := range []string{"feat/one", "feat/two"} {
		if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
			TaskRef: ref, Branch: ref, StartedAt: base,
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := AggregateFeed(Options{Root: root}, base.Add(time.Hour), FeedQuery{TaskRef: "feat/one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 || events.Events[0].TaskRef != "feat/one" {
		t.Fatalf("TaskRef 过滤异常: %+v", events.Events)
	}
}

// TestAggregateFeed_Limit caps the stream (default 200) so polling never ships a huge body,
// and Truncated reports the cut so the client can react (full refetch on a truncated
// incremental poll).
//
// TestAggregateFeed_Limit 截断流（默认 200），轮询不会发出大包；Truncated 如实报告
// 截断，客户端可据此反应（增量轮询被截断时全量重拉）。
func TestAggregateFeed_Limit(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	for i := 0; i < 5; i++ {
		if err := act.Append(p, &act.Conclusion{
			TaskRef: "feat/c" + string(rune('a'+i)), Grade: "B", Score: 80, Strength: "Strong",
			CompletedAt: base.Add(time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := AggregateFeed(Options{Root: root}, base.Add(6*time.Hour), FeedQuery{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 3 {
		t.Fatalf("limit=3 应截到 3 条，got %d", len(res.Events))
	}
	if !res.Truncated {
		t.Error("5 条事件 limit=3 应标记 Truncated")
	}
	// Newest kept: the limit must keep the most recent events (desc order head).
	if res.Events[0].TaskRef != "feat/ce" {
		t.Errorf("limit 应保留最新事件（降序头部），首条 = %q", res.Events[0].TaskRef)
	}

	// Exactly at the limit: no truncation (Truncated 只在真的丢了事件时才为真).
	res, err = AggregateFeed(Options{Root: root}, base.Add(6*time.Hour), FeedQuery{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated {
		t.Error("事件数恰等于 limit 不应标记 Truncated")
	}
}

// TestAggregateFeed_Empty: no data anywhere → empty (non-nil) slice, no error, no panic.
//
// TestAggregateFeed_Empty：完全无数据 → 空（非 nil）切片，不报错不 panic。
func TestAggregateFeed_Empty(t *testing.T) {
	res, err := AggregateFeed(Options{Root: t.TempDir()}, time.Now(), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Events == nil {
		t.Fatal("空数据应返回非 nil 空切片（JSON 序列化为 [] 而非 null）")
	}
	if len(res.Events) != 0 {
		t.Errorf("空数据应 0 事件，got %d", len(res.Events))
	}
	if res.Truncated {
		t.Error("空数据不应标记 Truncated")
	}
	// Roots explicitly empty + Root empty: still no panic.
	if _, err := AggregateFeed(Options{}, time.Now(), FeedQuery{}); err != nil {
		t.Fatal(err)
	}
}

// TestFeedEvent_SkillWireShape: the structured skill field rides the wire only when
// set (omitempty) — the same compat discipline as node (feed_node_test.go): a Go-level
// empty string alone would not catch a dropped omitempty.
//
// TestFeedEvent_SkillWireShape：结构化 skill 字段仅在有值时上线（omitempty）——与
// node 同一条兼容纪律（feed_node_test.go）：只断言 Go 层空串逮不到 omitempty 被删。
func TestFeedEvent_SkillWireShape(t *testing.T) {
	withSkill, err := json.Marshal(FeedEvent{Kind: FeedKindSkillTrigger, Skill: "code-review-gate"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(withSkill), `"skill":"code-review-gate"`) {
		t.Errorf("skill 字段未上线: %s", withSkill)
	}
	without, err := json.Marshal(FeedEvent{Kind: FeedKindTaskStart})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), `"skill"`) {
		t.Errorf("空 skill 改变了线上结构: %s", without)
	}
}
