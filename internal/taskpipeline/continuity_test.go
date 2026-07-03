package taskpipeline

import (
	"strings"
	"testing"
)

func TestIsGeneric(t *testing.T) {
	s := &TaskState{}
	if s.IsGeneric() {
		t.Fatal("空 Kind 应走门禁（非 generic），IsGeneric 返回 true 错误")
	}
	s.Kind = "code"
	if s.IsGeneric() {
		t.Fatal("Kind=code 应走门禁，IsGeneric 返回 true 错误")
	}
	s.Kind = TaskKindGeneric
	if !s.IsGeneric() {
		t.Fatal("Kind=generic 应 IsGeneric 返回 true")
	}
}

func TestHasContinuity(t *testing.T) {
	s := &TaskState{}
	if s.HasContinuity() {
		t.Fatal("空 task 不应有接续内容")
	}
	s.Goal = "做 X"
	if !s.HasContinuity() {
		t.Fatal("有 Goal 应判定为有接续内容")
	}
	s.Goal = ""
	s.Blockers = []Blocker{{ID: "b1", Content: "卡在 Y"}}
	if !s.HasContinuity() {
		t.Fatal("有 Blocker 应判定为有接续内容")
	}
}

func TestAddSessionDedupAndBackfill(t *testing.T) {
	s := &TaskState{OriginTool: "claude-code"}
	s.AddSession("s1", "")
	s.AddSession("s1", "") // 重复，不应二次加入
	s.AddSession("s2", "pi")
	if len(s.SessionLinks) != 2 {
		t.Fatalf("期望 2 个 session link，实际 %d", len(s.SessionLinks))
	}
	if s.SessionID != "s1" {
		t.Fatalf("单值 SessionID 应回填首个 s1，实际 %q", s.SessionID)
	}
	if s.SessionLinks[0].Tool != "" {
		t.Fatalf("未指定工具时 Tool 应保持空（存储不回退 OriginTool，避免接手方错误归属），实际 %q", s.SessionLinks[0].Tool)
	}
	tools := s.SessionTools()
	if len(tools) != 2 || tools[0] != "claude-code" || tools[1] != "pi" {
		t.Fatalf("SessionTools 应为 [claude-code pi]，实际 %v", tools)
	}
	before := len(s.SessionLinks)
	s.AddSession("", "x")
	if len(s.SessionLinks) != before {
		t.Fatal("空 sid 不应加入")
	}
}

func TestAddDecisionAutoFill(t *testing.T) {
	s := &TaskState{}
	s.AddDecision(Decision{Content: "用 PostgreSQL"})
	if len(s.Decisions) != 1 {
		t.Fatalf("期望 1 条决策，实际 %d", len(s.Decisions))
	}
	d := s.Decisions[0]
	if d.ID == "" {
		t.Fatal("AddDecision 应自动补 ID")
	}
	if !strings.HasPrefix(d.ID, "d") {
		t.Fatalf("决策 ID 应以 d 开头，实际 %q", d.ID)
	}
	if d.DecidedAt.IsZero() {
		t.Fatal("AddDecision 应自动补 DecidedAt")
	}
}

func TestResolveBlocker(t *testing.T) {
	s := &TaskState{}
	s.AddBlocker(Blocker{Content: "卡住"})
	s.AddBlocker(Blocker{Content: "另一个"})
	if s.Blockers[0].Status != "open" {
		t.Fatalf("新阻塞默认 open，实际 %q", s.Blockers[0].Status)
	}
	id := s.Blockers[0].ID
	if !s.ResolveBlocker(id, "已修复") {
		t.Fatal("ResolveBlocker 已存在的 ID 应返回 true")
	}
	if s.Blockers[0].Status != "resolved" || s.Blockers[0].Resolution != "已修复" {
		t.Fatalf("resolve 后状态/说明错误: %+v", s.Blockers[0])
	}
	if s.ResolveBlocker("nonexistent", "") {
		t.Fatal("ResolveBlocker 不存在的 ID 应返回 false")
	}
}

func TestOpenBlockers(t *testing.T) {
	s := &TaskState{}
	s.AddBlocker(Blocker{Content: "open1"})
	s.AddBlocker(Blocker{Content: "resolved1"})
	s.Blockers[1].Status = "resolved"
	s.AddBlocker(Blocker{Content: "wontfix1"})
	s.Blockers[2].Status = "wontfix"
	open := s.OpenBlockers()
	if len(open) != 1 {
		t.Fatalf("期望 1 个 open 阻塞，实际 %d", len(open))
	}
	if open[0].Content != "open1" {
		t.Fatalf("应返回 open1，实际 %q", open[0].Content)
	}
}

func TestAddFindingAutoFill(t *testing.T) {
	s := &TaskState{}
	s.AddFinding(Finding{Content: "内存泄漏", Source: "[pi]"})
	f := s.Findings[0]
	if f.ID == "" || !strings.HasPrefix(f.ID, "f") {
		t.Fatalf("finding ID 应以 f 开头且非空，实际 %q", f.ID)
	}
	if f.Status != "open" {
		t.Fatalf("新 finding 默认 open，实际 %q", f.Status)
	}
	if !s.ResolveFinding(f.ID) {
		t.Fatal("ResolveFinding 已存在 ID 应返回 true")
	}
	if s.Findings[0].Status != "fixed" {
		t.Fatal("ResolveFinding 后应为 fixed")
	}
}

// TestNewContinuityID_NoCollision：连续调用生成不同 ID（进程内原子 seq 去重 + 随机后缀跨进程
// 去碰撞）。ResolveBlocker/ResolveFinding 按 ID 精确命中首条，碰撞会让"解决第二条"命中首条——
// 真实 bug，故 ID 必须唯一。Windows 时钟低精度下连续调用 UnixNano 相同，靠 seq+随机区分。
func TestNewContinuityID_NoCollision(t *testing.T) {
	seen := make(map[string]bool, 500)
	for i := 0; i < 500; i++ {
		id := newContinuityID("d")
		if id == "" || !strings.HasPrefix(id, "d") {
			t.Fatalf("ID 格式异常（应以 d 开头）: %q", id)
		}
		if seen[id] {
			t.Fatalf("ID 碰撞（resolve 会命中错误项）: %q", id)
		}
		seen[id] = true
	}
	// 不同前缀各自唯一，且互不干扰。
	b1 := newContinuityID("b")
	f1 := newContinuityID("f")
	if !strings.HasPrefix(b1, "b") || !strings.HasPrefix(f1, "f") {
		t.Fatalf("前缀异常: b=%q f=%q", b1, f1)
	}
}

// TestExecuteTaskGate_GenericSkipsChecks：generic task 走 ExecuteTaskGate 直接标 Passed=true，
// 跳过 ReviewPassed 硬前置 + 前置 gate 检查 + 工作活动/advisory 全部。固化"generic 不走门禁"
// 语义——防后续误改让 generic task 被门禁卡住（generic 承载调研/接续，无代码可门禁）。
func TestExecuteTaskGate_GenericSkipsChecks(t *testing.T) {
	root := t.TempDir()
	// generic + task-complete：跳过 ReviewPassed 硬前置 + 前置 gate，直接 Passed=true。
	g := &TaskState{TaskRef: "feat/g", Branch: "feat/g", Kind: TaskKindGeneric}
	res, err := ExecuteTaskGate(root, "task-complete", g)
	if err != nil {
		t.Fatalf("generic task-complete 不应报错（跳过 ReviewPassed/前置 gate）: %v", err)
	}
	if !res.Passed {
		t.Fatalf("generic task-complete 应直接 Passed=true，实际 %+v", res)
	}
	if !strings.Contains(res.Message, "generic") {
		t.Errorf("跳过信息应含 generic，实际 %q", res.Message)
	}
	// 对比：非 generic 过了前置 gate 后，task-complete 仍因无 ReviewPassed 报错——
	// 固化 generic 跳过 ReviewPassed 硬前置的分流差异（防误改让 generic 被卡）。
	code := &TaskState{TaskRef: "feat/c", Branch: "feat/c"}
	code.RecordGateResult("task-implement", true, "")
	code.RecordGateResult("task-verify", true, "")
	if _, err := ExecuteTaskGate(root, "task-complete", code); err == nil {
		t.Fatal("非 generic task-complete 无 ReviewPassed 应报错（generic 才跳过此硬前置）")
	}
}
