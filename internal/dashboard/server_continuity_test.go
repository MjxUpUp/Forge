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

// TestAggregateContinuity_CardsSorting 验证接续字段投影 + 进行中在前 + 同状态按启动时间倒序。
func TestAggregateContinuity_CardsSorting(t *testing.T) {
	root := t.TempDir()
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

// TestAggregateContinuity_GenericHidesGates：generic 任务不走门禁，看板卡片 GateTotal=0
// （模板 {{if gt .GateTotal 0}} 跳过门禁行），避免误导性的"门禁 0/3 卡在第一道"显示。
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
