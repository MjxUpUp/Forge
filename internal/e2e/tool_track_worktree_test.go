package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestToolTrack_WorktreeActivityReachesGate 是 finding fdkrxk4e9u3gg 的端到端
// 回归钉：完全在 linked git worktree（其 .git 是指向主 repo common dir 的
// 文件）里推进的任务，agent 明明做了 Read/Edit/Bash，work-activity 门禁却报
// "0 tool uses"——从 worktree 触发的 hook 记录必须流进门禁读取的同一个用户级
// DataDir（一 repo 一 key），且从 worktree 里跑的门禁必须看得到。
//
// 本测试走真实分发：worktree 里 forge task start、以 worktree 为 cwd 的真实
// `forge hook` PostToolUse 调用、再从 worktree 跑 task-verify 门禁——断言门禁
// 不再报零活动阻断，且 toollog 落在主 checkout 的 DataDir。
func TestToolTrack_WorktreeActivityReachesGate(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/wt-tooltrack")
	const sid = "sess-wt-tooltrack"

	// 临时项目的 linked worktree。`git worktree add` 需要已有 commit
	// （freshProjectOnBranch 已提交）。worktree 落在主 checkout 之外，
	// 复现 incident 的兄弟目录布局。
	wt := filepath.Join(filepath.Dir(dir), "wt-linked")
	git(t, dir, "worktree", "add", "-b", "fix/wt-branch", wt)

	envWith := func() []string {
		return append(os.Environ(),
			"CLAUDE_CODE_SESSION_ID="+sid,
			"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
	}
	// runWT 从 worktree 内部跑 forge（incident 姿态：所有 forge 调用都以
	// worktree 为 cwd）。
	runWT := func(args ...string) {
		t.Helper()
		cmd := exec.Command(forgeBin, args...)
		cmd.Dir = wt
		cmd.Env = envWith()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("forge %s (in worktree): %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	runWT("task", "start", "--ref", "WT-TT", "--title", "worktree tool-track test")

	// 工作在 worktree 里发生：新建源码文件 + 宿主会为 Read/Edit 对触发的
	// PostToolUse hook。
	writeFile(t, wt, "internal/widget/click.go", "package widget\n\nfunc Click() {}\n")
	git(t, wt, "add", "internal/widget/click.go")
	forgeHook(t, wt, "tool-track", hookStdin(t, sid, "PostToolUse", "Read", map[string]any{
		"file_path": "internal/widget/click.go",
	}))
	forgeHook(t, wt, "auto-compile", hookStdin(t, sid, "PostToolUse", "Edit", map[string]any{
		"file_path": "internal/widget/click.go",
		"content":   "package widget\n\nfunc Click() {}\n",
	}))

	// 记录必须落在共享 DataDir（主 checkout 的），不是 worktree 专属的——
	// 这正是门禁依赖的归因。
	toollog, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "toollog.jsonl"))
	if err != nil {
		t.Fatalf("toollog not in the shared main-checkout DataDir: %v", err)
	}
	if !strings.Contains(string(toollog), `"task_ref":"WT-TT"`) {
		t.Errorf("toollog in shared DataDir has no WT-TT entries — worktree activity misattributed:\n%s", toollog)
	}

	// 从 worktree 里跑的门禁必须看得到活动：先过 task-implement 前置，
	// 再 task-verify 不得报零活动（可能有别的 advisory；此处只钉零活动
	// 误报）。
	runWT("task", "gate", "task-implement", "--ref", "WT-TT")
	cmd := exec.Command(forgeBin, "task", "gate", "task-verify", "--ref", "WT-TT")
	cmd.Dir = wt
	cmd.Env = envWith()
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "work activity") && strings.Contains(string(out), "minimum 1") {
		t.Fatalf("task-verify in worktree still reports zero work activity:\n%s", out)
	}
}
