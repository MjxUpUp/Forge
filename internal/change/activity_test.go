package change

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "t")
}

// TestActivities_CleanChangeUsesCommitTime 钉住评审 P1-a 的修法方向：换机器 /
// clone / worktree add 会把目录 mtime 刷成检出时刻，所以「干净」的 change 必须用
// 提交时间判定——否则三天没动的 old 会在 clone 后立刻变成「新鲜」。
func TestActivities_CleanChangeUsesCommitTime(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	chDir := filepath.Join(dir, "spec", "changes", "old")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chDir, "goal.md"), []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	env := append(os.Environ(),
		"GIT_AUTHOR_DATE="+old,
		"GIT_COMMITTER_DATE="+old,
	)
	cmd = exec.Command("git", "commit", "-m", "old change")
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	// 模拟 clone：文件 mtime 现在，提交时间 5 天前，工作区干净。
	if err := os.Chtimes(filepath.Join(chDir, "goal.md"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	got := Activities(dir, []string{"old"})["old"]
	if time.Since(got) < 4*24*time.Hour {
		t.Fatalf("干净的旧 change 必须按提交时间判陈旧，got %v（距今 %v）", got, time.Since(got))
	}
}

// TestActivities_DirtyChangeCountsAsFresh 有人正在动它（有未提交改动）时算新鲜。
func TestActivities_DirtyChangeCountsAsFresh(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	chDir := filepath.Join(dir, "spec", "changes", "wip")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chDir, "goal.md"), []byte("# wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "wip")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+old, "GIT_COMMITTER_DATE="+old)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	// 未提交的改动（评审细节：含未跟踪文件）→ 正在推进。
	if err := os.WriteFile(filepath.Join(chDir, "change.md"), []byte("# wip change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Activities(dir, []string{"wip"})["wip"]
	if time.Since(got) > time.Hour {
		t.Fatalf("有未提交改动的 change 应算新鲜，got %v（距今 %v）", got, time.Since(got))
	}
}

// TestActivities_NoGitFallsBackToMtime 没有 git 时退回文件 mtime（可能偏新，已写进注释）。
func TestActivities_NoGitFallsBackToMtime(t *testing.T) {
	dir := t.TempDir()
	chDir := filepath.Join(dir, "spec", "changes", "plain")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chDir, "goal.md"), []byte("# plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Activities(dir, []string{"plain"})["plain"]
	if time.Since(got) > time.Hour {
		t.Fatalf("无 git 时应退回文件 mtime，got %v（距今 %v）", got, time.Since(got))
	}
}

// TestActivities_BatchMatchesSingle 批量的结果必须与单查一致（next 用批量，测试用单查）。
func TestActivities_BatchMatchesSingle(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	for _, id := range []string{"a", "b"} {
		chDir := filepath.Join(dir, "spec", "changes", id)
		if err := os.MkdirAll(chDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(chDir, "goal.md"), []byte(id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "both")
	batch := Activities(dir, []string{"a", "b"})
	for _, id := range []string{"a", "b"} {
		single, err := LastActivity(dir, id)
		if err != nil {
			t.Fatal(err)
		}
		if !single.Equal(batch[id]) {
			t.Fatalf("%s: 单查 %v != 批量 %v", id, single, batch[id])
		}
	}
}
