package change

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestActivities_BatchMatchesExpected 批量结果必须与「逐 id 起一次 git log -1」的
// 独立算法一致（评审 P3-d：不能拿 LastActivity 自比自——它内部就是 Activities）。
func TestActivities_BatchMatchesExpected(t *testing.T) {
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
	cmd := exec.Command("git", "commit", "-m", "both")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+time.Now().Add(-6*24*time.Hour).Format(time.RFC3339))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	batch := Activities(dir, []string{"a", "b"})
	for _, id := range []string{"a", "b"} {
		out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%ct", "--", "spec/changes/"+id).Output()
		if err != nil {
			t.Fatal(err)
		}
		var sec int64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &sec); err != nil {
			t.Fatal(err)
		}
		want := time.Unix(sec, 0)
		if !batch[id].Equal(want) {
			t.Fatalf("%s: 批量 %v != 独立计算 %v", id, batch[id], want)
		}
	}
}

// TestActivities_ShallowCloneIsStale 浅克隆下 `git log -- path` 只会返回被 graft 的
// 形近 HEAD 时间，无法判定历史——按陈旧处理（评审 P2-a：置零也必须被 next 消费）。
func TestActivities_ShallowCloneIsStale(t *testing.T) {
	base := t.TempDir()
	initRepo(t, base)
	chDir := filepath.Join(base, "spec", "changes", "old")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chDir, "goal.md"), []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "old")
	cmd.Dir = base
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+old, "GIT_COMMITTER_DATE="+old)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	// 再来一个新提交：浅克隆 graft 后 `git log -- spec/changes/old` 会返回这个提交的
	// 时间（now），于是旧 change 被判新鲜——这正是 P2-a 的现场。
	if err := os.WriteFile(filepath.Join(base, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, base, "add", "-A")
	gitRun(t, base, "commit", "-m", "touch root")

	cloneDir := t.TempDir()
	cmd = exec.Command("git", "clone", "--depth", "1", "file://"+filepath.ToSlash(base), cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("clone --depth 1 不可用: %v\n%s", err, out)
	}
	shallowOut, err := exec.Command("git", "-C", cloneDir, "rev-parse", "--is-shallow-repository").Output()
	if err != nil || strings.TrimSpace(string(shallowOut)) != "true" {
		t.Skipf("clone 不是浅克隆，跳过: %v %s", err, shallowOut)
	}
	if got := Activities(cloneDir, []string{"old"})["old"]; !got.IsZero() {
		t.Fatalf("浅克隆里无法判定历史的 change 必须置零（陈旧），got %v", got)
	}
}

// TestActivities_RenameDoesNotPolluteIDs rename 的第二条 record 是旧路径原文
// （评审 P3-b：`git mv spec/changes/newc/goal.md …` 曾把 id 解析成 `c`）。
func TestActivities_RenameDoesNotPolluteIDs(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	for _, id := range []string{"c", "newc"} {
		chDir := filepath.Join(dir, "spec", "changes", id)
		if err := os.MkdirAll(chDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(chDir, "goal.md"), []byte(id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	old := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", "commit", "-m", "old changes")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+old, "GIT_COMMITTER_DATE="+old)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	gitRun(t, dir, "mv", "spec/changes/newc/goal.md", "spec/changes/newc/goal2.md")

	acts := Activities(dir, []string{"c", "newc"})
	if time.Since(acts["c"]) < 4*24*time.Hour {
		t.Fatalf("rename 不应把相邻的旧 change c 标成新鲜，got %v", acts["c"])
	}
	if time.Since(acts["newc"]) > time.Hour {
		t.Fatalf("被 rename 的 change 自己确实是刚动过，应算新鲜，got %v", acts["newc"])
	}
}

// TestActivities_ShallowCloneDirtyIsFresh 浅克隆里「无法判定历史」只在干净时成立：
// 人正在改（脏或未跟踪）应以 mtime 为准算新鲜，否则浅克隆会把正在做的活判成陈旧
// （评审 P3-e：dirty-shallow 这一态此前只有手工端到端覆盖）。
func TestActivities_ShallowCloneDirtyIsFresh(t *testing.T) {
	base := t.TempDir()
	initRepo(t, base)
	chDir := filepath.Join(base, "spec", "changes", "old")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chDir, "goal.md"), []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "old")
	cmd.Dir = base
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+old, "GIT_COMMITTER_DATE="+old)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(base, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, base, "add", "-A")
	gitRun(t, base, "commit", "-m", "touch root")

	cloneDir := t.TempDir()
	cmd = exec.Command("git", "clone", "--depth", "1", "file://"+filepath.ToSlash(base), cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("clone --depth 1 不可用: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", cloneDir, "rev-parse", "--is-shallow-repository").Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Skipf("clone 不是浅克隆，跳过: %v %s", err, out)
	}
	// 在浅克隆里改 change 自己的文件（未提交）。
	cloneChDir := filepath.Join(cloneDir, "spec", "changes", "old")
	if err := os.WriteFile(filepath.Join(cloneChDir, "goal.md"), []byte("# old\n# edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Activities(cloneDir, []string{"old"})["old"]
	if time.Since(got) > time.Hour {
		t.Fatalf("浅克隆里正在改的 change 应算新鲜（mtime），got %v", got)
	}
}
