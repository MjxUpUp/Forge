package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func ckGitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setupCheckpointRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ckGitRun(t, dir, "init", "-q", ".")
	ckGitRun(t, dir, "config", "user.email", "t@example.com")
	ckGitRun(t, dir, "config", "user.name", "t")
	if _, _, code := runForgeStreams(t, dir, "init"); code != 0 {
		t.Fatalf("forge init failed")
	}
	if _, _, code := runForgeStreams(t, dir, "task", "start", "--ref", "feat/ck", "--accept", "test -f keep.txt ::"); code != 0 {
		t.Fatalf("task start failed")
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ckGitRun(t, dir, "add", "-A")
	ckGitRun(t, dir, "commit", "-qm", "base")
	return dir
}

func TestTaskCheckpoint_RollbackRestoresWorktree(t *testing.T) {
	dir := setupCheckpointRepo(t)
	// 改一处不提交，存档，再丢弃改动
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := runForgeStreams(t, dir, "task", "checkpoint", "--label", "before refactor")
	if code != 0 || !strings.Contains(out, "cp-1") {
		t.Fatalf("checkpoint failed: %s", out)
	}
	ckGitRun(t, dir, "checkout", "--", "keep.txt")
	if b, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); strings.TrimSpace(string(b)) != "v1" {
		t.Fatalf("expected v1 after checkout, got %q", b)
	}
	out, _, code = runForgeStreams(t, dir, "task", "rollback", "cp-1")
	if code != 0 {
		t.Fatalf("rollback failed: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); strings.TrimSpace(string(b)) != "v2" {
		t.Fatalf("rollback did not restore content, got %q", b)
	}

	list, _, code := runForgeStreams(t, dir, "task", "checkpoint", "--list")
	if code != 0 || !strings.Contains(list, "cp-1") || !strings.Contains(list, "最近回滚到此") {
		t.Fatalf("list wrong: %s", list)
	}
}

func TestTaskCheckpoint_RefusesCleanTreeAndUnknownID(t *testing.T) {
	dir := setupCheckpointRepo(t)
	if _, _, code := runForgeStreams(t, dir, "task", "checkpoint"); code == 0 {
		t.Fatalf("clean worktree must refuse checkpoint")
	}
	out, errOut, code := runForgeStreams(t, dir, "task", "rollback", "cp-99")
	if code == 0 || !strings.Contains(out+errOut, "cp-99") {
		t.Fatalf("unknown id must fail and be named: %s", out+errOut)
	}
}
