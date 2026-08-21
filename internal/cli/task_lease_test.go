package cli

import (
	"strings"
	"testing"
)

// task_lease_test.go — e2e wiring check for the node lease (sync-convergence.md §4):
// `forge task start` must stamp a lease for THIS machine (holder = local node_id,
// fencing 1) into the persisted task state.
//
// task_lease_test.go —— 节点租约的 e2e 接线验证（sync-convergence.md §4）：
// `forge task start` 必须把本机租约（holder = 本机 node_id，fencing 1）落进
// 持久化任务状态。

func TestTaskStart_StampsNodeLease(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "t@t.com")
	runGit(t, tmpDir, "config", "user.name", "T")
	runGit(t, tmpDir, "commit", "--allow-empty", "-m", "init")
	runGit(t, tmpDir, "checkout", "-b", "feat/lease")

	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	if _, stderr, code := runForgeStreams(t, tmpDir, "task", "start", "--ref", "feat/lease", "--title", "lease test"); code != 0 {
		t.Fatalf("task start failed: %s", stderr)
	}
	stdout, _, code := runForgeStreams(t, tmpDir, "task", "context", "--ref", "feat/lease", "--json")
	if code != 0 {
		t.Fatalf("task context failed: %s", stdout)
	}
	if !strings.Contains(stdout, `"lease"`) {
		t.Fatalf("task state has no lease block: %s", stdout)
	}
	if !strings.Contains(stdout, `"holder_node": "fnode_`) {
		t.Errorf("lease holder must be this machine's fnode id: %s", stdout)
	}
	if !strings.Contains(stdout, `"fencing": 1`) {
		t.Errorf("first claim must have fencing 1: %s", stdout)
	}

	runForge(t, tmpDir, "task", "abort", "--ref", "feat/lease")
}
