package registry

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestIsMember_RealGitWorktree 钉住真实 linked worktree 内 hook 侧的项目解析：
// 只登记主 checkout 时，从 worktree cwd 调 IsMember 仍须命中（经共享的 git
// common-dir key），且返回的 root 必须推导出与主 checkout 相同的 DataDir——
// 否则 worktree 内所有项目级 hook 静默空转（allow-and-exit），其中记录的
// toollog/checklog 活动要么不落盘、要么落进门禁从不读取的 DataDir
// （finding fdkrxk4e9u3gg：worktree 内真实工作被 work-activity 门禁报
// 0 tool uses）。
func TestIsMember_RealGitWorktree(t *testing.T) {
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Fatalf("git unavailable, cannot build real worktree: %v", err)
	}
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	main := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(main, "init")
	run(main, "commit", "--allow-empty", "-m", "init")
	if err := Add(main); err != nil {
		t.Fatalf("Add(main): %v", err)
	}
	wt := filepath.Join(filepath.Dir(main), "wt-linked")
	run(main, "worktree", "add", "-b", "wt-branch", wt)

	root, ok := IsMember(wt)
	if !ok {
		t.Fatalf("IsMember(worktree cwd) = false — hooks in a worktree would silently no-op")
	}
	// 返回的 root 是 worktree 自己的 git working-tree 根（git 操作须在那里跑），
	// 但身份推导的 state 必须与主 checkout 收敛。
	if dMain, dWT := forgedata.DataDirFor(main), forgedata.DataDirFor(root); dMain != dWT {
		t.Errorf("DataDir mismatch: main=%s worktree-root=%s — activity recorded in the worktree would be invisible to gates", dMain, dWT)
	}
	// 反向同样：主 checkout 里跑的门禁读到 worktree hook 写入的同一 DataDir。
	if rootMain, ok := IsMember(main); !ok || forgedata.DataDirFor(rootMain) != forgedata.DataDirFor(root) {
		t.Errorf("IsMember(main) root=%s ok=%v diverges from worktree root=%s", rootMain, ok, root)
	}
}
