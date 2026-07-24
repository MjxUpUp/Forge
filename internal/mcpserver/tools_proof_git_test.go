package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/review"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// proof 的 git 场景单测：v2 快路径（AcceptedHeadCommit==HEAD 信任快照）+ review drift（审查后改码）。
// 这两个路径需真 git repo（HEAD 非空 + SourceChangesSince 算 change hash），单测内联最小 git helper
// （git -C dir ...）。非 git 的 v1 兜底/门禁/generic 在 tools_proof_test.go 已覆盖。v2 + drift 是
// proof-of-work 防时效漂移 + 防审查后偷改的核心，必须有 git 回归。

// gitExec 在 dir 下跑 git 子命令，失败 t.Fatal。
func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// writeCommit 写一个文件并 commit，推进 HEAD（用于构造 review drift 的"改码"步）。
func writeCommit(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	gitExec(t, dir, "add", name)
	gitExec(t, dir, "commit", "-m", msg)
}

// TestTaskProof_V2FastPath：git repo，acceptance AcceptedHeadCommit==当前 HEAD + Passed=true → v2 快路径
// 命中：item.Fresh=true，信任 c.Passed 不重跑。IsComplete + 无 review drift + acceptance 全过 → done=true。
// 这是 acceptance 防时效漂移的核心：verify-acceptance 实跑留快照（head+passed），proof 信任快照不重跑——
// 避免重跑 flaky/slow 验收，同时快照绑定 HEAD 防止"验收时过、后来改码"的假绿。
func TestTaskProof_V2FastPath(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	gitExec(t, dir, "config", "user.email", "t@e.com")
	gitExec(t, dir, "config", "user.name", "T")
	writeCommit(t, dir, "f.txt", "a", "init")
	head := taskpipeline.GetHeadCommit(dir)

	state := &taskpipeline.TaskState{TaskRef: `feat/v2`, Branch: `feat/v2`}
	for _, g := range taskpipeline.DefaultGates() {
		state.RecordGateResult(g.ID, true, head)
	}
	state.MarkComplete()
	// acceptance 快照：verify-acceptance 实跑时 AcceptedHeadCommit=head, Passed=true, Output 记录。
	state.Acceptance = []taskpipeline.AcceptanceCriterion{{
		Run: `echo ok`, Expected: `ok`, Passed: true, Output: `ok`, AcceptedHeadCommit: head,
	}}
	// review pass at head（无 drift）：SourceChangesSince(head) 在 HEAD==head 时算空 diff。
	hash, _, err := review.SourceChangesSince(dir, head)
	if err != nil {
		t.Fatalf("SourceChangesSince: %v", err)
	}
	state.MarkReviewPassed(head, hash)
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	out, err := taskProofCore(dir, taskProofInput{Ref: `feat/v2`})
	if err != nil {
		t.Fatalf("taskProofCore: %v", err)
	}
	if len(out.Acceptance) != 1 {
		t.Fatalf("acceptance 条数 = %d, want 1", len(out.Acceptance))
	}
	if !out.Acceptance[0].Fresh {
		t.Error("v2 快路径应命中（head==AcceptedHeadCommit），Fresh 应 true（信任快照不重跑）")
	}
	if !out.Acceptance[0].Passed {
		t.Error("v2 应信任 c.Passed=true")
	}
	if out.ReviewDrift {
		t.Error("HEAD 未推进应无 review drift")
	}
	if !out.Done {
		t.Errorf("v2 快路径 + IsComplete + 无 drift 应 done=true，reason=%q", out.Reason)
	}
}

// TestTaskProof_ReviewDrift：review pass 后改码（commit B）→ SourceChangesSince(reviewHead) 重算 change hash
// != 审查时记录的 hash → drift → done=false。proof 把"审查后偷改"显性化——agent 看到 ReviewDrift 知道
// 要重派只读子 agent 复审（对齐 task-complete 门禁的 drift 硬阻断，proof 是 pre-flight 提前暴露）。
func TestTaskProof_ReviewDrift(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	gitExec(t, dir, "config", "user.email", "t@e.com")
	gitExec(t, dir, "config", "user.name", "T")
	writeCommit(t, dir, "f.txt", "a", "init")
	headA := taskpipeline.GetHeadCommit(dir)

	state := &taskpipeline.TaskState{TaskRef: `feat/drift`, Branch: `feat/drift`}
	for _, g := range taskpipeline.DefaultGates() {
		state.RecordGateResult(g.ID, true, headA)
	}
	state.MarkComplete()
	state.Acceptance = []taskpipeline.AcceptanceCriterion{{
		Run: `echo ok`, Expected: `ok`, Passed: true, Output: `ok`, AcceptedHeadCommit: headA,
	}}
	hashA, _, err := review.SourceChangesSince(dir, headA)
	if err != nil {
		t.Fatalf("SourceChangesSince at review: %v", err)
	}
	state.MarkReviewPassed(headA, hashA)
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	// review pass 后偷改：commit B 推进 HEAD（改源码 .go——SourceChangesSince 只算源码文件）。
	writeCommit(t, dir, `main.go`, `package main // sneaky edit after review`, `sneaky edit`)

	out, err := taskProofCore(dir, taskProofInput{Ref: `feat/drift`})
	if err != nil {
		t.Fatalf("taskProofCore: %v", err)
	}
	if !out.ReviewDrift {
		t.Error("审查后改码应检测到 review drift")
	}
	if out.Done {
		t.Errorf("review drift 应 done=false（须复审），reason=%q", out.Reason)
	}
	if !strings.Contains(out.Reason, `审查`) && !strings.Contains(out.Reason, `drift`) {
		t.Errorf("reason 应指向 review drift 复审，got %q", out.Reason)
	}
}
