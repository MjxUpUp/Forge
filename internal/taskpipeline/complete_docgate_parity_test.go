package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecuteTaskGate_CompleteBlocksWithoutDocReview 钉住 1.87.2 的修复：
//   - `forge task gate task-complete` 与 `forge task complete` 同口径；
//   - 改了 .md 但没记 L2 回检时，gate 就该 BLOCKED，而不是先绿灯、complete 才拦。
//
// 反例（修复前）：executor 的 GateComplete 分支只跑 review prereqs + test-coverage，
// doc gate 只在 clitask 的 complete 里跑 → 照 next 走的人会在最后一步被打回。
func TestExecuteTaskGate_CompleteBlocksWithoutDocReview(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{"README.md": "# demo\n"}, "add readme")

	base := headRev(t, dir)
	state := &TaskState{TaskRef: "feat/doc", Branch: "feat/testcov", HeadCommit: base, ReviewPassed: true}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")

	// base 之后改了文档、尚未记录 doc-review。
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# demo\n\n补一段说明\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// GateBlocked 以 error 返回（ExecuteTaskGate 的既有契约），两种形态都接受。
	if _, err := ExecuteTaskGate(dir, GateComplete, state); err == nil || !strings.Contains(err.Error(), "doc gate") {
		t.Fatalf("缺 L2 回检时 task-complete gate 应 BLOCKED（与 complete 同口径），got err=%v", err)
	}
}

// TestExecuteTaskGate_CompletePassesWithoutDocs 保证同口径不会误伤：没改 markdown 时
// doc gate 应放行（CheckDocGate 自身语义），gate 不该因为「没有 doc-review」而阻断。
func TestExecuteTaskGate_CompletePassesWithoutDocs(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{"foo.go": "package main\n\nfunc Foo() int { return 1 }\n"}, "add foo")

	base := headRev(t, dir)
	state := &TaskState{TaskRef: "feat/code", Branch: "feat/testcov", HeadCommit: base, ReviewPassed: true}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main\n\nfunc Foo() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ExecuteTaskGate(dir, GateComplete, state)
	if err != nil {
		t.Fatalf("ExecuteTaskGate: %v", err)
	}
	if !res.Passed {
		t.Fatalf("无 markdown 变更时不应被 doc gate 拦，got %q", res.Message)
	}
}

// headRev 读取当前 HEAD 的 commit id。
func headRev(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}
