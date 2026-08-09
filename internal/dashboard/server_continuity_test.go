package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestAggregateContinuity_Empty: empty root (global mode not focused) and
// empty .forge both do not crash and return an empty board.
//
// TestAggregateContinuity_Empty 空 root（全局模式未聚焦）和空 .forge 都不崩，返空 board。
func TestAggregateContinuity_Empty(t *testing.T) {
	b, err := AggregateContinuity("", time.Now())
	if err != nil {
		t.Fatalf("空 root 不应报错（全局模式边界）: %v", err)
	}
	if len(b.Cards) != 0 {
		t.Errorf("空 root 应 0 卡片，实际 %d", len(b.Cards))
	}
	if _, err := AggregateContinuity(t.TempDir(), time.Now()); err != nil {
		t.Fatalf("空 .forge 不应报错: %v", err)
	}
}

// TestAggregateContinuity_CardsSorting verifies continuity-field projection
// + in-progress first + within the same status sorted by start time descending.
//
// TestAggregateContinuity_CardsSorting 验证接续字段投影 + 进行中在前 + 同状态按启动时间倒序。
func TestAggregateContinuity_CardsSorting(t *testing.T) {
	root := t.TempDir()
	// Completed task (started earlier).
	//
	// 已完成 task（较早启动）
	done := &taskpipeline.TaskState{
		TaskRef: "feat/done", Branch: "feat/done",
		StartedAt: time.Now().Add(-2 * time.Hour), OriginTool: "pi",
	}
	for _, g := range taskpipeline.DefaultGates() {
		done.RecordGateResult(g.ID, true, "")
	}
	done.MarkComplete()
	if err := taskpipeline.SaveTaskState(root, done); err != nil {
		t.Fatal(err)
	}
	// In-progress task (started later, with blocker/goal/decision/session).
	//
	// 进行中 task（较晚启动，带 blocker/goal/decision/session）
	active := &taskpipeline.TaskState{
		TaskRef: "feat/active", Branch: "feat/active", Goal: "做 X",
		StartedAt: time.Now().Add(-1 * time.Hour), OriginTool: "claude-code",
	}
	active.AddBlocker(taskpipeline.Blocker{Content: "卡住"})
	active.AddDecision(taskpipeline.Decision{Content: "选 A"})
	active.AddSession("s1", "claude-code")
	if err := taskpipeline.SaveTaskState(root, active); err != nil {
		t.Fatal(err)
	}

	b, err := AggregateContinuity(root, time.Now())
	if err != nil {
		t.Fatalf("AggregateContinuity: %v", err)
	}
	if len(b.Cards) != 2 {
		t.Fatalf("期望 2 卡片，实际 %d", len(b.Cards))
	}
	if b.Incomplete != 1 || b.Complete != 1 {
		t.Errorf("计数 Incomplete=%d Complete=%d，期望 1/1", b.Incomplete, b.Complete)
	}
	// In-progress must precede completed (the board focuses on running work).
	//
	// 进行中必须排在已完成之前（看板聚焦在跑的工作）
	if b.Cards[0].TaskRef != "feat/active" {
		t.Errorf("首卡片应为进行中的 feat/active，实际 %s", b.Cards[0].TaskRef)
	}
	c0 := b.Cards[0]
	if c0.IsComplete || c0.Goal != "做 X" || c0.OpenBlockers != 1 || c0.Decisions != 1 {
		t.Errorf("进行中卡片接续字段异常: %+v", c0)
	}
	if len(c0.SessionTools) != 1 || c0.SessionTools[0] != "claude-code" {
		t.Errorf("SessionTools 异常: %+v", c0.SessionTools)
	}
	if c0.GateTotal == 0 {
		t.Error("GateTotal 不应为 0（应有 3 道门禁）")
	}
	if !b.Cards[1].IsComplete {
		t.Error("第二卡片应为已完成")
	}
}

// TestServe_ContinuityJSON: /api/continuity.json returns 200 + valid JSON
// containing the task.
//
// TestServe_ContinuityJSON /api/continuity.json 返回 200 + 合法 JSON 含 task。
func TestServe_ContinuityJSON(t *testing.T) {
	root := t.TempDir()
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: "feat/j", Branch: "feat/j", Goal: "JSON 验证",
	}); err != nil {
		t.Fatal(err)
	}
	handler := localhostOnly(securityHeaders(newMux(Options{Root: root})))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/continuity.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("状态码 %d，期望 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var b ContinuityBoard
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, body)
	}
	if len(b.Cards) != 1 || b.Cards[0].TaskRef != "feat/j" || b.Cards[0].Goal != "JSON 验证" {
		t.Errorf("JSON 卡片异常: %+v", b.Cards)
	}
}

// TestServe_ContinuityHTML: /continuity returns 200 + HTML containing the
// task ref plus the board title.
//
// TestServe_ContinuityHTML /continuity 返回 200 + HTML 含 task ref + 看板标题。
func TestServe_ContinuityHTML(t *testing.T) {
	root := t.TempDir()
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: "feat/h", Branch: "feat/h", Goal: "HTML 验证",
	}); err != nil {
		t.Fatal(err)
	}
	handler := localhostOnly(securityHeaders(newMux(Options{Root: root})))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/continuity")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("状态码 %d，期望 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "feat/h") {
		t.Errorf("HTML 应含 task ref feat/h:\n%s", s)
	}
	if !strings.Contains(s, "任务接续看板") {
		t.Errorf("HTML 应含看板标题:\n%s", s)
	}
}

// TestAggregateContinuity_GenericHidesGates: generic tasks skip gates, so the
// board card GateTotal=0 (template {{if gt .GateTotal 0}} skips the gate row),
// avoiding the misleading "gates 0/3 stuck on first" display.
//
// TestAggregateContinuity_GenericHidesGates：generic 任务不走门禁，看板卡片 GateTotal=0
// （模板 {{if gt .GateTotal 0}} 跳过门禁行），避免误导性的「门禁 0/3 卡在第一道」显示。
func TestAggregateContinuity_GenericHidesGates(t *testing.T) {
	root := t.TempDir()
	g := &taskpipeline.TaskState{
		TaskRef: "feat/gen", Branch: "feat/gen", Kind: taskpipeline.TaskKindGeneric,
		Goal: "调研 X", StartedAt: time.Now(),
	}
	if err := taskpipeline.SaveTaskState(root, g); err != nil {
		t.Fatal(err)
	}
	b, err := AggregateContinuity(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Cards) != 1 {
		t.Fatalf("期望 1 卡片，实际 %d", len(b.Cards))
	}
	c := b.Cards[0]
	if c.GateTotal != 0 {
		t.Errorf("generic 任务 GateTotal 应为 0（不走门禁，看板不显门禁行），实际 %d", c.GateTotal)
	}
	if c.Kind != "generic" {
		t.Errorf("Kind 应为 generic，实际 %q", c.Kind)
	}
}

// TestAggregateContinuity_AnnotatesZombie: a delegation that has stalled (offered>7d here) is
// projected onto the card as IsZombie + ZombieReason, and a fresh offer is NOT flagged. This is
// the board surface of the same taskpipeline.IsZombie signal mine/health share (design §12 标黄).
// OfferedAt is forced 8 days into the past because the state machine stamps now.
//
// TestAggregateContinuity_AnnotatesZombie：停滞的分派（此处 offered>7d）投影到卡片为 IsZombie +
// ZombieReason，刚 offered 的不被标记。这是 mine/health 共享的同一 taskpipeline.IsZombie 信号的
// 看板表面（设计 §12 标黄）。OfferedAt 被强制为 8 天前，因状态机盖当前时间。
func TestAggregateContinuity_AnnotatesZombie(t *testing.T) {
	root := t.TempDir()
	// Stalled offer (8 days ago) → offered>7d zombie.
	stalled := &taskpipeline.TaskState{TaskRef: "feat/stalled", Branch: "feat/stalled", Goal: "停滞"}
	if err := stalled.AssignTo("kimi", "frontend", "claude-code"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	stalled.Assignment.OfferedAt = &old
	if err := taskpipeline.SaveTaskState(root, stalled); err != nil {
		t.Fatal(err)
	}
	// Fresh offer → not a zombie (negative control).
	fresh := &taskpipeline.TaskState{TaskRef: "feat/fresh", Branch: "feat/fresh", Goal: "新鲜"}
	if err := fresh.AssignTo("cursor", "frontend", "claude-code"); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SaveTaskState(root, fresh); err != nil {
		t.Fatal(err)
	}

	b, err := AggregateContinuity(root, time.Now())
	if err != nil {
		t.Fatalf("AggregateContinuity: %v", err)
	}
	byRef := map[string]continuityCard{}
	for _, c := range b.Cards {
		byRef[c.TaskRef] = c
	}
	s, ok := byRef["feat/stalled"]
	if !ok {
		t.Fatalf("feat/stalled 卡片应在板上, got %+v", byRef)
	}
	if !s.IsZombie || !strings.Contains(s.ZombieReason, "offered>7d") {
		t.Errorf("feat/stalled 应 IsZombie 且 reason 含 offered>7d, got %+v", s)
	}
	if f, ok := byRef["feat/fresh"]; ok && f.IsZombie {
		t.Errorf("feat/fresh 刚 offered 不应标僵尸, got %+v", f)
	}
}

// TestServe_ContinuityHTML_ZombieBadge: the rendered board HTML carries the zombie badge + zombie
// card class for a stalled delegation (design §12 标黄), proving the template wiring end-to-end
// through the HTTP layer.
//
// TestServe_ContinuityHTML_ZombieBadge：渲染的看板 HTML 对停滞分派带僵尸徽标 + zombie 卡片类
// （设计 §12 标黄），证明模板接线经 HTTP 层端到端生效。
func TestServe_ContinuityHTML_ZombieBadge(t *testing.T) {
	root := t.TempDir()
	stalled := &taskpipeline.TaskState{TaskRef: "feat/stalled", Branch: "feat/stalled", Goal: "停滞"}
	if err := stalled.AssignTo("kimi", "frontend", "claude-code"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	stalled.Assignment.OfferedAt = &old
	if err := taskpipeline.SaveTaskState(root, stalled); err != nil {
		t.Fatal(err)
	}
	handler := localhostOnly(securityHeaders(newMux(Options{Root: root})))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/continuity")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	// The badge renders (html/template escapes the > in the reason to &gt;, so we assert the
	// marker + that the reason token survives in escaped form; the raw reason is checked in the
	// AggregateContinuity card test, which sees the unescaped data field).
	//
	// 徽标已渲染（html/template 把 reason 里的 > 转义成 &gt;，故断言标记 + reason 经转义后仍在；
	// 原始 reason 在 AggregateContinuity 卡片测试里断言，那里看到的是未转义的 data 字段）。
	if !strings.Contains(s, "⚠僵尸") || !strings.Contains(s, "offered&gt;7d") {
		t.Errorf("HTML 应含僵尸徽标 + offered>7d（转义为 offered&gt;7d）reason:\n%s", s)
	}
	if !strings.Contains(s, `card zombie`) {
		t.Errorf("HTML 应给僵尸卡片加 zombie 类（class=\"card zombie\"）:\n%s", s)
	}
}
