package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/worktree"
	"github.com/spf13/cobra"
)

// task_worktree.go implements L4 (multi-task-concurrency design §7): the worktree-per-task
// lifecycle. `task start --worktree` atomically creates worktree + branch + task + binding
// (invariant I2's physical form: the filesystem is a disposable working copy, one per
// task); `task finish` merges and cleans; `worktree janitor` keeps the fleet bounded with
// the never-delete-dirty guarantee.
//
// task_worktree.go 实现 L4（multi-task-concurrency 设计 §7）：worktree-per-task 生命
// 周期。`task start --worktree` 原子地建 worktree + 分支 + 任务 + 绑定（不变式 I2 的
// 物理形态：文件系统是一次性工作副本，每任务一份）；`task finish` 合并清理；
// `worktree janitor` 以「脏的永不删」保上界。

// worktreeBase resolves the base ref for a new task worktree: explicit --base, else the
// repo's main line (main, falling back to master — local first, then origin).
//
// worktreeBase 解析新任务 worktree 的基线：显式 --base，否则仓库主线（main 回落
// master——先本地后 origin）。
func worktreeBase(root, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
		if out, err := exec.Command("git", "-C", root, "rev-parse", "--verify", base).Output(); err == nil && strings.TrimSpace(string(out)) != "" {
			return base, nil
		}
	}
	return "", fmt.Errorf("找不到主线分支（main/master），请用 --base 显式指定")
}

// createTaskWorktree creates the worktree + branch for a task start and returns its path.
// Default location is OUTSIDE the repo tree (`<repo 父目录>/<repo 名>-wt/<分支>`，
// 项目树零写入原则); forge.worktreeinclude (gitignore-syntax) files are copied in.
//
// createTaskWorktree 为 task start 建 worktree + 分支并返回路径。默认位置在 repo
// 树【外】（`<repo 父目录>/<repo 名>-wt/<分支>`，项目树零写入原则）；
// forge.worktreeinclude（gitignore 语法）匹配的文件会被复制进去。
func createTaskWorktree(root, ref, base, wtDirParent string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("--worktree requires --ref")
	}
	if err := validateBranchRef(ref); err != nil {
		return "", fmt.Errorf("invalid ref for worktree branch: %w", err)
	}
	branch := ref
	if !strings.Contains(ref, "/") {
		branch = "feat/" + ref // 分支命名约定：无斜杠 ref 补 feat/ 前缀（detector 可反向解析）
	}
	baseRef, err := worktreeBase(root, base)
	if err != nil {
		return "", err
	}
	parent := wtDirParent
	if parent == "" {
		absRoot, _ := filepath.Abs(root)
		parent = filepath.Join(filepath.Dir(absRoot), filepath.Base(absRoot)+"-wt")
	}
	path := filepath.Join(parent, taskcontext.SanitizeRef(branch))
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("worktree 路径已存在: %s（重名任务？换 --ref 或 --wt-dir）", path)
	}
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "-b", branch, path, baseRef).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add 失败: %v\n%s", err, out)
	}
	copyWorktreeIncludes(root, path)
	return path, nil
}

// copyWorktreeIncludes copies forge.worktreeinclude-listed gitignored files (.env etc.)
// into the new worktree. One line = one path or gitignore-style glob; comments (#) and
// blanks skipped. Best-effort per line — a missing include must not fail the whole start.
//
// copyWorktreeIncludes 把 forge.worktreeinclude 列出的 gitignored 文件（.env 等）复制
// 进新 worktree。一行 = 一个路径或 gitignore 风格 glob；跳过注释（#）与空行。逐行
// 尽力而为——缺一个 include 不得让整个 start 失败。
func copyWorktreeIncludes(srcRoot, dstRoot string) {
	listPath := filepath.Join(srcRoot, "forge.worktreeinclude")
	data, err := os.ReadFile(listPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(srcRoot, line))
		for _, m := range matches {
			rel, err := filepath.Rel(srcRoot, m)
			if err != nil {
				continue
			}
			dst := filepath.Join(dstRoot, rel)
			if info, err := os.Stat(m); err == nil && info.IsDir() {
				continue // 目录不支持——只复制文件（.gitignore 语法子集，文档化限制）
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				continue
			}
			if in, err := os.Open(m); err == nil {
				if out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
					_, _ = io.Copy(out, in)
					out.Close()
				}
				in.Close()
			}
		}
	}
}

// runTaskFinish is `forge task finish`: gate-complete + clean-tree verified merge and
// worktree cleanup. Merge runs in the MAIN checkout (worktrees cannot check out a branch
// the main checkout holds and vice versa; the main root is derived from the git common dir).
//
// runTaskFinish 即 `forge task finish`：验证门禁完成 + 工作树干净后合并并清理
// worktree。合并在主检出里跑（worktree 与主检出不能同时持有同一分支；主根从 git
// common dir 推导）。
func runTaskFinish(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	mergeTo, _ := cmd.Flags().GetString("merge-to")
	keep, _ := cmd.Flags().GetBool("keep")

	// finish 按【绑定】而非 ActiveTaskState 解析（合并窗口崩溃修复，review B2
	// 关联缺陷）：complete 已解绑或绑定被外部清掉时 ActiveTaskState 不再解析
	// 到该任务（它跳过已完成任务），若 finish 先查活跃状态，用户会永远卡在
	// "无活跃任务"而 worktree 无人清理。先查 binding——它是清理的权威锚。
	binding := worktree.Load(root)
	if binding == nil || binding.TaskRef == "" {
		// 主检出或无绑定任务：无 worktree 可清理。
		sid := taskpipeline.CurrentSessionID()
		if st, _ := taskpipeline.ActiveTaskState(root, sid); st != nil && st.CompletedAt == nil {
			return fmt.Errorf("任务 %s 尚未 complete——先过完三道门禁（当前 %s）", st.TaskRef, st.CurrentGate)
		}
		fmt.Println("无本目录 worktree 绑定，无需清理")
		return nil
	}
	state, err := taskpipeline.LoadTaskState(root, binding.TaskRef)
	if err != nil || state == nil {
		// 绑定指向的任务已消失（abort/删除）：按死锚解绑即可。
		_ = worktree.Clear(root, binding.TaskRef)
		fmt.Printf("绑定指向的任务 %s 已不存在——已解绑死锚\n", binding.TaskRef)
		return nil
	}
	if state.CompletedAt == nil {
		return fmt.Errorf("任务 %s 尚未 complete——先过完三道门禁（当前 %s）", state.TaskRef, state.CurrentGate)
	}
	wtPath := binding.Path

	// 工作树必须干净——脏 worktree 绝不自动处置（免删除条款）。
	if out, err := exec.Command("git", "-C", wtPath, "status", "--porcelain").Output(); err != nil || strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("worktree %s 有未提交变更——先提交或 stash（免删除条款：脏 worktree 绝不自动处置）\n%s", wtPath, out)
	}

	mainRoot, err := gitMainRoot(wtPath)
	if err != nil {
		return fmt.Errorf("推导主检出失败: %w", err)
	}
	// B2 修正（review BLOCKER）：主检出绑定（task start 于主检出或 legacy 桥建立的
	// 绑定）不是 worktree——不做 merge/remove，只解绑提示。否则 git worktree remove
	// 会拒（main working tree），且若合并先行则用户被丢在"已合并但清理失败、且
	// ActiveTaskState 跳过已完成任务使重试无解"的半完成态。
	if wtPath == mainRoot {
		_ = worktree.Clear(wtPath, state.TaskRef)
		fmt.Printf("任务 %s 已完成（主检出绑定，非 worktree——合并/清理不适用）\n", state.TaskRef)
		return nil
	}
	target := mergeTo
	if target == "" {
		target, err = worktreeBase(mainRoot, "")
		if err != nil {
			return err
		}
	}
	// B2 修正：合并必须在【目标分支】上执行——裸 git merge 合进主检出当前 HEAD，
	// 与打印的目标不一致。多任务常态下主检出很可能不在主线上。
	headOut, herr := exec.Command("git", "-C", mainRoot, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if herr != nil {
		return fmt.Errorf("读取主检出分支失败: %v", herr)
	}
	head := strings.TrimSpace(string(headOut))
	if head != target {
		return fmt.Errorf("主检出当前在 %s，不在合并目标 %s——请先 checkout %s 或用 --merge-to %s\n（多任务常态：主检出停在某个任务分支上，盲目合并会把任务分支合进错误分支）", head, target, target, head)
	}
	fmt.Printf("合并 %s → %s（主检出 %s）…\n", binding.Branch, target, mainRoot)
	if out, err := exec.Command("git", "-C", mainRoot, "merge", "--no-edit", binding.Branch).CombinedOutput(); err != nil {
		return fmt.Errorf("合并失败（可手动解决后重试 finish，worktree 未动）: %v\n%s", err, out)
	}

	if keep {
		fmt.Printf("已合并；--keep 保留 worktree %s\n", wtPath)
		return nil
	}
	if out, err := exec.Command("git", "-C", mainRoot, "worktree", "remove", wtPath).CombinedOutput(); err != nil {
		return fmt.Errorf("worktree 清理失败（合并已完成，可手动 git worktree remove）: %v\n%s", err, out)
	}
	_ = worktree.Clear(wtPath, state.TaskRef)
	fmt.Printf("任务 %s 完成：已合并 %s，worktree 已清理\n", state.TaskRef, binding.Branch)
	return nil
}

// gitMainRoot derives the main checkout root from a worktree path via the git common dir
// (`<common>/.git` 的父目录). For the main checkout itself this is the same root.
//
// gitMainRoot 从 worktree 路径经 git common dir 推导主检出根（`<common>/.git` 的父
// 目录）。对主检出自身即原根。
func gitMainRoot(root string) (string, error) {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(string(out))
	return filepath.Dir(common), nil // <repo>/.git → <repo>
}

// runWorktreeJanitor is `forge worktree janitor`: bounded cleanup with the
// never-delete-dirty guarantee — only completed-task or expired (14d default) workspaces
// whose tree is CLEAN get removed; dirty ones are reported, never touched. Bindings whose
// path vanished are dropped unconditionally (dead anchors).
//
// runWorktreeJanitor 即 `forge worktree janitor`：带「脏的永不删」保证的有界清理
// ——只清理任务已完成或超期（默认 14d）且树干净的 workspace；脏的只报告绝不碰。
// 路径已消失的绑定无条件删除（死锚）。
func runWorktreeJanitor(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	wsDir := filepath.Join(forgedata.DataDirFor(root), "workspaces")
	entries, err := os.ReadDir(wsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("无 workspace 绑定（未使用过 --worktree）")
			return nil
		}
		return err
	}
	states, _ := taskpipeline.ListTaskStates(root)
	completed := map[string]bool{}
	for _, s := range states {
		if s != nil && s.CompletedAt != nil {
			completed[s.TaskRef] = true
		}
	}
	const staleAfter = 14 * 24 * time.Hour
	removed, reported := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(wsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var b worktree.Binding
		if err := json.Unmarshal(data, &b); err != nil || b.Path == "" {
			_ = os.Remove(path) // 损坏绑定 = 死锚
			continue
		}
		if _, err := os.Stat(b.Path); os.IsNotExist(err) {
			_ = os.Remove(path)
			removed++
			fmt.Printf("已清死锚: %s（路径已消失）\n", b.Path)
			continue
		}
		eligible := completed[b.TaskRef] || time.Since(b.LastSeenAt) > staleAfter
		if !eligible {
			continue
		}
		if out, err := exec.Command("git", "-C", b.Path, "status", "--porcelain").Output(); err != nil || strings.TrimSpace(string(out)) != "" {
			reported++
			fmt.Printf("⚠ 脏 worktree 保留（免删除条款）: %s（任务 %s）\n", b.Path, b.TaskRef)
			continue
		}
		mainRoot, err := gitMainRoot(b.Path)
		if err != nil {
			continue
		}
		if out, err := exec.Command("git", "-C", mainRoot, "worktree", "remove", b.Path).CombinedOutput(); err != nil {
			reported++
			fmt.Printf("⚠ 清理失败: %s: %v\n", b.Path, err)
			_ = out
			continue
		}
		_ = os.Remove(path)
		removed++
		fmt.Printf("已清理: %s（任务 %s）\n", b.Path, b.TaskRef)
	}
	fmt.Printf("janitor 完成：清理 %d，保留报告 %d\n", removed, reported)
	return nil
}

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "workspace/worktree 生命周期（multi-task-concurrency L4）",
}

var worktreeJanitorCmd = &cobra.Command{
	Use:   "janitor",
	Short: "清理已完成/超期且干净的 worktree（脏的永不删），删除死锚绑定",
	RunE:  runWorktreeJanitor,
}

var taskFinishCmd = &cobra.Command{
	Use:   "finish [--merge-to <branch>] [--keep]",
	Short: "完成收尾：验证门禁后合并分支并清理 worktree（免删除条款：脏树拒绝）",
	RunE:  runTaskFinish,
}

func init() {
	taskCmd.AddCommand(taskFinishCmd)
	taskFinishCmd.Flags().String("merge-to", "", "合并目标分支（默认主线 main/master）")
	taskFinishCmd.Flags().Bool("keep", false, "合并后保留 worktree（默认清理）")

	worktreeCmd.AddCommand(worktreeJanitorCmd)
	rootCmd.AddCommand(worktreeCmd)
}
