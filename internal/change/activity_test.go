package change

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// runGit 在 dir 里跑一条 git 命令；失败直接 t.Fatal（测试环境一定有 git）。
func runGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// LastActivity 取 max(文件 mtime, 最近一次提交时间)：只看 mtime 时，git clone /
// checkout / worktree add 会把陈旧 change 刷成「新鲜」，而换机器正是这个保护最该
// 生效的场景（代码评审 P2-a）。
func TestLastActivity_UsesLaterOfFileAndCommitTime(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, nil, "init", "-q", ".")
	dir := filepath.Join(root, "spec", "changes", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "goal.md")
	if err := os.WriteFile(file, []byte("# g\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 场景一：文件 mtime 五天前，但刚刚提交 → 提交时间算数（检出后 mtime 被刷新的等价形态）。
	old := time.Now().Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, nil, "add", "-A")
	runGit(t, root, nil, "commit", "-qm", "c1")
	got, err := LastActivity(root, "demo")
	if err != nil {
		t.Fatalf("LastActivity: %v", err)
	}
	if time.Since(got) > 2*time.Minute {
		t.Fatalf("刚提交过的 change 应算新鲜，实际活动时间 %v（差 %v）", got, time.Since(got))
	}

	// 场景二：文件 mtime 是现在，但提交是五天前 → mtime 的未提交编辑算数。
	now := time.Now()
	if err := os.Chtimes(file, now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("# g\n\n未提交的编辑\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, nil, "add", "-A")
	runGit(t, root, []string{"GIT_COMMITTER_DATE=" + old.Format(time.RFC3339), "GIT_AUTHOR_DATE=" + old.Format(time.RFC3339)}, "commit", "-qm", "c2")
	got, err = LastActivity(root, "demo")
	if err != nil {
		t.Fatalf("LastActivity: %v", err)
	}
	if time.Since(got) > 2*time.Minute {
		t.Fatalf("工作区刚改过应算新鲜，实际活动时间 %v", got)
	}
}

// 没有 git（或没有提交）时退化为纯 mtime：不能因为 git 缺失就返回零值。
func TestLastActivity_NoGitFallsBackToMtime(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "spec", "changes", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "goal.md")
	if err := os.WriteFile(file, []byte("# g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Now().Add(-10 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(file, want, want); err != nil {
		t.Fatal(err)
	}
	got, err := LastActivity(root, "demo")
	if err != nil {
		t.Fatalf("LastActivity: %v", err)
	}
	if got.IsZero() || got.Sub(want).Abs() > 2*time.Second {
		t.Fatalf("无 git 时应退回 mtime=%v，实际 %v", want, got)
	}
}
