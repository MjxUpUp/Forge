package mcpserver

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestTaskStart_FromIssue_ParsesExternalOrigin：from_issue URL 解析为 ExternalOrigin 并落盘到
// state——proof-of-work 闭环衔接 spawn 式编排器（Symphony 类）的关键入口：编排器把 issue URL
// 传给 forge_task_start，task 天然锚定外部 issue，不靠 branch 名推断。
func TestTaskStart_FromIssue_ParsesExternalOrigin(t *testing.T) {
	root := t.TempDir()
	out, err := taskStartCore(root, taskStartInput{
		Ref:       `feat/abc`,
		Title:     `do X`,
		FromIssue: `https://linear.app/forge/issue/ABC-123`,
	})
	if err != nil {
		t.Fatalf("taskStartCore: %v", err)
	}
	if !out.Created || out.TaskRef != `feat/abc` {
		t.Errorf("Created=%v TaskRef=%q, want true/feat/abc", out.Created, out.TaskRef)
	}
	if out.ExternalOrigin.Tracker != "linear" || out.ExternalOrigin.Identifier != `ABC-123` {
		t.Errorf("ExternalOrigin 解析错: %+v", out.ExternalOrigin)
	}
	// 落盘校验：LoadTaskState 读回，ExternalOrigin 持久化（跨会话/跨工具可读）。
	loaded, err := taskpipeline.LoadTaskState(root, `feat/abc`)
	if err != nil || loaded == nil {
		t.Fatalf("LoadTaskState: loaded=%v err=%v", loaded, err)
	}
	if loaded.ExternalOrigin.Identifier != `ABC-123` {
		t.Errorf("落盘 ExternalOrigin.Identifier=%q, want ABC-123", loaded.ExternalOrigin.Identifier)
	}
}

// TestTaskStart_DuplicateRejected：同 ref 二次 start 报 already exists，防幽灵任务覆盖已有 task
// （taskguard 场景：agent 重复 start 不应静默覆盖之前的进度/门禁）。
func TestTaskStart_DuplicateRejected(t *testing.T) {
	root := t.TempDir()
	if _, err := taskStartCore(root, taskStartInput{Ref: `feat/dup`, Title: `first`}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if _, err := taskStartCore(root, taskStartInput{Ref: `feat/dup`, Title: `second`}); err == nil {
		t.Fatal("重复 start 应报错 already exists")
	}
}

// TestTaskStart_MissingRefOnMain：ref 空 + 无 branch context（t.TempDir 非 git，Detect 返空）
// → 报错。MCP start 必须显式 ref 或在 feature branch 上调，main + 空 ref 是无效起点。
func TestTaskStart_MissingRefOnMain(t *testing.T) {
	root := t.TempDir()
	if _, err := taskStartCore(root, taskStartInput{}); err == nil {
		t.Fatal("ref 空 + 无 branch context 应报错")
	}
}

// TestTaskStart_PlanExtractsAcceptance：Plan markdown 的 Run:/Expected: 自动提取为验收标准
// （与 CLI --plan-file 同源）。plan 提取是 acceptance 维度不空转的根因——agent 不必手抄。
func TestTaskStart_PlanExtractsAcceptance(t *testing.T) {
	root := t.TempDir()
	plan := "## 验收\nRun: go version\nExpected: go version\n"
	out, err := taskStartCore(root, taskStartInput{Ref: `feat/plan`, Plan: plan})
	if err != nil {
		t.Fatalf("taskStartCore: %v", err)
	}
	if out.AcceptanceCount != 1 {
		t.Errorf("AcceptanceCount=%d, want 1（plan 应提取 1 条 Run/Expected）", out.AcceptanceCount)
	}
}

// TestTaskStart_OriginTool_ExplicitDeclaresOrigin：spawn 式非 claude-code 编排器（Symphony 类，不注入
// CLAUDE_CODE_SESSION_ID）调 MCP 起 task 时，用 origin_tool 显式声明发起工具——detectTool env 探测
// 失败时 state.OriginTool 仍落盘显式值，SessionLink 发起方信号不丢（M1 对齐目标的闭环 + L-1 补全：
// 与 CLI --origin-tool 对称，非 claude-code 编排器的唯一 origin 声明路径）。
func TestTaskStart_OriginTool_ExplicitDeclaresOrigin(t *testing.T) {
	root := t.TempDir()
	if _, err := taskStartCore(root, taskStartInput{Ref: `feat/ot`, Title: `origin`, OriginTool: `pi`}); err != nil {
		t.Fatalf("taskStartCore: %v", err)
	}
	loaded, err := taskpipeline.LoadTaskState(root, `feat/ot`)
	if err != nil || loaded == nil {
		t.Fatalf("LoadTaskState: loaded=%v err=%v", loaded, err)
	}
	if loaded.OriginTool != `pi` {
		t.Errorf("state.OriginTool=%q, want pi（显式 origin_tool 应落盘，M1 闭环）", loaded.OriginTool)
	}
}
