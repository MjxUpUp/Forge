package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHarness_InitTrustBoundary pins the T6 contract (multi-task-concurrency §11.4):
// init establishes the repo, the baseline commit carries process state (tasks), and the
// trust anchors / machine-local stores are gitignored — check-ignore is the assertion
// (what git would actually track), not our string list.
//
// TestHarness_InitTrustBoundary 钉住 T6 契约（multi-task-concurrency §11.4）：init 建立
// 仓库、基线提交携带过程状态（tasks）、信任锚/机器本地 store 被 gitignore——断言用
// check-ignore（git 实际会跟踪什么），不对照我们的字符串清单。
func TestHarness_InitTrustBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	// 非 TTY（go test 的 stdin）+ 无 --yes → 拒绝（agent 不得代批）。
	if stdout, _, code := runForge(t, t.TempDir(), "harness", "init"); code == 0 || !strings.Contains(stdout, "人在终端确认") {
		t.Fatalf("非 TTY 无 --yes 应被拒绝: %s", stdout)
	}

	// 存量数据 + --yes init：基线入库。
	key := "abc123def456"
	tasksDir := filepath.Join(home, "projects", key, "tasks")
	os.MkdirAll(tasksDir, 0o755)
	os.WriteFile(filepath.Join(tasksDir, "feat-x.json"), []byte(`{"task_ref":"feat-x"}`), 0o644)
	stampsDir := filepath.Join(home, "projects", key, "stamps")
	os.MkdirAll(stampsDir, 0o755)
	os.WriteFile(filepath.Join(stampsDir, "dh-deadbeef.stamp"), []byte(`{"reviewed":true}`), 0o644)

	if stdout, _, code := runForge(t, t.TempDir(), "harness", "init", "--from-existing", "--yes"); code != 0 {
		t.Fatalf("harness init --yes 失败: %s", stdout)
	}
	if !HarnessInitialized() {
		t.Fatal("harness repo 未建立")
	}

	// 信任锚被忽略。
	out, err := harnessGit(home, "check-ignore", "-q", filepath.Join(home, "projects", key, "stamps", "dh-deadbeef.stamp"))
	if err != nil {
		t.Fatalf("stamps 应被 gitignore（信任锚永不出本机）: err=%v out=%s", err, out)
	}
	// 过程状态被跟踪。
	if out, err := harnessGit(home, "ls-files"); err != nil || !strings.Contains(out, "tasks/feat-x.json") {
		t.Fatalf("tasks 应入基线提交: err=%v out=%s", err, out)
	}
	// 幂等拒绝。
	if _, _, code := runForge(t, t.TempDir(), "harness", "init", "--yes"); code == 0 {
		t.Fatal("重复 init 应幂等拒绝")
	}
}

// TestHarness_CommitBestEffort: task-boundary batching commits new tracked state and stays
// silent when the harness is absent (pre-init degradation).
//
// TestHarness_CommitBestEffort：任务边界批量提交新的受管状态；harness 缺席时静默
// （init 前降级）。
func TestHarness_CommitBestEffort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)

	// 未 init：静默无操作，不 panic。
	HarnessCommitBestEffort("noop probe")

	if stdout, _, code := runForge(t, t.TempDir(), "harness", "init", "--yes"); code != 0 {
		t.Fatalf("init: %s", stdout)
	}
	key := "key111222333"
	os.MkdirAll(filepath.Join(home, "projects", key, "tasks"), 0o755)
	os.WriteFile(filepath.Join(home, "projects", key, "tasks", "t.json"), []byte(`{}`), 0o644)
	HarnessCommitBestEffort("boundary probe")
	out, err := harnessGit(home, "log", "--oneline", "-1")
	if err != nil || !strings.Contains(out, "boundary probe") {
		t.Fatalf("边界提交缺失: err=%v out=%s", err, out)
	}
}
