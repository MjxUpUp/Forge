package mcpserver

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// proof 单元测试在非 git 的 t.TempDir 下覆盖核心分支：IsComplete 门禁判定、acceptance v1 重跑
// 兜底（非 git head="" → v2 快路径永不命中，必走 RunTestCommand 重跑）、generic kind 特判、
// 无任务错误。v2 快路径（AcceptedHeadCommit==HEAD）与 review drift 需 git 仓库真 HEAD/真 commit，
// 留 internal/e2e/proof_mcp_test.go（Task 6）端到端覆盖——单元测试不造 git fixture，保持快与隔离。

// TestTaskProof_NotComplete_NoAcceptance：空 state（无门禁 history、无 acceptance）→ done=false，
// reason 指向下一道门禁。proof 把"还没过门禁就声明 done"显性化——agent 看到 reason 知道下一步。
func TestTaskProof_NotComplete_NoAcceptance(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: `feat/nc`, Branch: `feat/nc`}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskProofCore(root, taskProofInput{Ref: `feat/nc`})
	if err != nil {
		t.Fatalf("taskProofCore: %v", err)
	}
	if out.Done {
		t.Error("无门禁 history 应 done=false")
	}
	if out.IsComplete {
		t.Error("空 state IsComplete 应 false")
	}
	if !strings.Contains(out.Reason, `门禁`) {
		t.Errorf("reason 应指向门禁，got %q", out.Reason)
	}
}

// TestTaskProof_AcceptanceVRerun：非 git（head=""）→ v2 快路径不命中，走 v1 重跑兜底。
// go version 重跑 exit 0 + 输出含子串 → Passed=true，但 Fresh=false（重跑非快照信任）。
// IsComplete=false → done 仍 false（门禁未过）。
func TestTaskProof_AcceptanceVRerun(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{
		TaskRef:    `feat/ar`,
		Branch:     `feat/ar`,
		Acceptance: taskpipeline.ParseAcceptance([]string{`go version :: go version`}),
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskProofCore(root, taskProofInput{Ref: `feat/ar`})
	if err != nil {
		t.Fatalf("taskProofCore: %v", err)
	}
	if len(out.Acceptance) != 1 {
		t.Fatalf("Acceptance 条数 = %d, want 1", len(out.Acceptance))
	}
	if out.Acceptance[0].Fresh {
		t.Error("非 git 应走 v1 重跑，Fresh 应 false（非快照信任）")
	}
	if !out.Acceptance[0].Passed {
		t.Error("go version 重跑应 Passed=true（exit 0 + 子串命中）")
	}
	if out.Done {
		t.Error("IsComplete=false 应 done=false（即使 acceptance 过）")
	}
}

// TestTaskProof_AcceptanceV1_ExpectedMismatch：v1 重跑兜底的核心回归——命令退出 0 但输出不含
// Expected 子串时，必须判 Passed=false。历史 bug：proof v1 曾只判 RunTestCommand 退出码（漏
// Expected 子串比对），导致 `echo wrong :: right` 这种“退出 0 但输出不含 right”的验收假绿——
// 击穿 proof “deterministic 断言”主张，触发场景正是 v1 设计场景（非 git / 提交后复查）。
// 修复：v1 与 VerifyAcceptance 同调 JudgeAcceptance 三态判定。本用例机械守护该语义不回退。
func TestTaskProof_AcceptanceV1_ExpectedMismatch(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{
		TaskRef:    `feat/mm`,
		Branch:     `feat/mm`,
		Acceptance: taskpipeline.ParseAcceptance([]string{`echo wrong :: right`}),
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskProofCore(root, taskProofInput{Ref: `feat/mm`})
	if err != nil {
		t.Fatalf("taskProofCore: %v", err)
	}
	if len(out.Acceptance) != 1 {
		t.Fatalf("Acceptance 条数 = %d, want 1", len(out.Acceptance))
	}
	if out.Acceptance[0].Fresh {
		t.Error("非 git 应走 v1 重跑，Fresh 应 false")
	}
	if out.Acceptance[0].Passed {
		t.Error("echo wrong（exit 0）但输出不含 Expected right，应 Passed=false（防 H1 假绿回退）")
	}
	if out.Done {
		t.Error("acceptance 未过应 done=false")
	}
}

// TestTaskProof_Generic_Done：generic kind 不走门禁/review，无 acceptance → done=true。
// generic 是调研/设计任务，proof 只判 acceptance（空则视为过），IsComplete 记 true（无门禁阻断）。
func TestTaskProof_Generic_Done(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: `feat/g`, Branch: `feat/g`, Kind: "generic"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskProofCore(root, taskProofInput{Ref: `feat/g`})
	if err != nil {
		t.Fatalf("taskProofCore: %v", err)
	}
	if !out.Done {
		t.Errorf("generic 无 acceptance 应 done=true，reason=%q", out.Reason)
	}
	if !out.IsComplete {
		t.Error("generic IsComplete 应 true（无门禁阻断概念）")
	}
}

// TestTaskProof_Generic_AcceptanceFailed：generic + acceptance 重跑失败 → done=false。
// generic 仍须 acceptance 过（若有）——proof 对 generic 不是无脑 true。
func TestTaskProof_Generic_AcceptanceFailed(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{
		TaskRef:    `feat/gf`,
		Branch:     `feat/gf`,
		Kind:       "generic",
		Acceptance: taskpipeline.ParseAcceptance([]string{`go forge-nope-nope ::`}),
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskProofCore(root, taskProofInput{Ref: `feat/gf`})
	if err != nil {
		t.Fatalf("taskProofCore: %v", err)
	}
	if out.Done {
		t.Error("generic + acceptance 失败应 done=false")
	}
	if out.Acceptance[0].Passed {
		t.Error("go forge-nope-nope 重跑应 Passed=false（非零退出）")
	}
}

// TestTaskProof_NoActiveTask：无对应 ref 的 task → error（proof 不能凭空断言不存在的 task）。
func TestTaskProof_NoActiveTask(t *testing.T) {
	root := t.TempDir()
	if _, err := taskProofCore(root, taskProofInput{Ref: `nonexistent`}); err == nil {
		t.Fatal("不存在的 task ref 应报错")
	}
}
