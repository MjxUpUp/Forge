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

// conventionalBranchPrefixes is the shared prefix table for the ref→branch
// derivation path (hasConventionalPrefix → deriveBranchName →
// createTaskWorktree). It is a deliberately NARROWER mirror of
// validateBranchRef's accepted set (task.go also takes feature/, bugfix/,
// hotfix/): a ref with one of those wider prefixes still derives feat/<ref>
// here — legal, because the derived name is subsequently validated by
// validateBranchRef itself (the derivation is only the resolution chain's
// fallback; the true ref rides pointers/bindings). Hoisted into one named
// constant so the two derivation entries (--worktree and --branch via
// deriveBranchName) and any future caller share a single table — the inline
// copy in createTaskWorktree was deleted (dogfood #6 class: two copies drift).
//
// conventionalBranchPrefixes 是 ref→分支派生路径（hasConventionalPrefix →
// deriveBranchName → createTaskWorktree）的共享前缀表。它刻意是
// validateBranchRef 接收集的【窄】镜像（task.go 还接受 feature/、bugfix/、
// hotfix/）：带这些宽前缀的 ref 在此仍派生 feat/<ref>——合法，因为派生名随后
// 由 validateBranchRef 自行校验（派生只是解析链的兜底，真 ref 由指针/绑定承载）。
// 收敛为单一具名常量，使两个派生入口（--worktree 与经 deriveBranchName 的
// --branch）及未来调用方共享一张表——createTaskWorktree 里的内联副本已删除
// （dogfood #6 类问题：两份副本会漂移）。
var conventionalBranchPrefixes = []string{"feat/", "fix/", "refactor/", "test/", "chore/", "docs/", "ci/", "perf/", "build/", "style/"}

// hasConventionalPrefix mirrors validateBranchRef's accepted prefixes — kept adjacent
// to its main consumer; drift with the validator fails the subsequent validateBranchRef call.
//
// hasConventionalPrefix 镜像 validateBranchRef 接受的前缀集——贴着主要消费方放；
// 与校验器漂移会被随后的 validateBranchRef 兜住。
func hasConventionalPrefix(ref string) bool {
	for _, p := range conventionalBranchPrefixes {
		if strings.HasPrefix(ref, p) {
			return true
		}
	}
	return false
}

// deriveBranchName is the SINGLE ref→branch derivation shared by --worktree and
// --branch（dogfood 发现 #6，2026-08-28：另一会话 task start --branch 踩到前缀校验
// 拒绝——#2 只对齐了 --worktree 入口，第二入口漏改）。规则：ref 已带惯例前缀则
// 同名；否则派生 feat/<ref 斜杠转连字>（分支映射只是解析链兜底，真 ref 由指针/
// 绑定承载）。
//
// deriveBranchName is the SINGLE ref→branch derivation shared by --worktree and
// --branch (dogfood finding #6, 2026-08-28: another session's `task start --branch`
// hit the prefix validation refusal — #2 aligned only the --worktree entry, the
// second one was missed). Rule: conventionally-prefixed ref maps to itself; anything
// else derives feat/<slashes→dashes> (the branch mapping is only the resolution
// chain's fallback — the true ref rides pointers/bindings).
func deriveBranchName(ref string) (string, error) {
	branch := ref
	if !hasConventionalPrefix(ref) {
		branch = "feat/" + strings.ReplaceAll(ref, "/", "-")
	}
	if err := validateBranchRef(branch); err != nil {
		return "", fmt.Errorf("invalid derived branch %q (from ref %q): %w", branch, ref, err)
	}
	return branch, nil
}

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
	// Single ref→branch derivation (dogfood #6 class fix, 2026-08-29): the inline
	// copy of the derivation lived on after deriveBranchName was extracted for the
	// --branch entry — two copies of the same rule drift (exactly how #6 happened).
	// Rule: a conventionally-prefixed ref maps to itself; anything else derives
	// feat/<slashes→dashes> (the branch mapping is only the resolution chain's
	// fallback — the true ref rides pointers/bindings).
	//
	// 单一 ref→分支派生（dogfood #6 类修复，2026-08-29）：deriveBranchName 为
	// --branch 入口抽出后，这里的内联派生副本继续存活——同一规则两份副本必漂移
	// （#6 正是这样发生的）。规则：ref 已带惯例前缀则同名；否则派生
	// feat/<斜杠转连字>（分支映射只是解析链的兜底环节，真 ref 由指针/绑定承载）。
	branch, err := deriveBranchName(ref)
	if err != nil {
		return "", err
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
// blanks skipped. Best-effort per line — a missing include must not fail the whole start
// — but per-file COPY failures are accumulated and reported as ONE stderr warning after
// the walk (fix/cleanup-batch, 2026-08-29): before this, an unreadable/locked source or
// an unwritable destination vanished silently, and the first session inside the worktree
// discovered the missing .env with an opaque failure far from the cause. The warning
// lists every failed include so the user can fix the cause before the session starts.
//
// copyWorktreeIncludes 把 forge.worktreeinclude 列出的 gitignored 文件（.env 等）复制
// 进新 worktree。一行 = 一个路径或 gitignore 风格 glob；跳过注释（#）与空行。逐行
// 尽力而为——缺一个 include 不得让整个 start 失败——但逐文件的【复制】失败会被累积，
// 遍历结束后以一条 stderr 警告汇总上报（fix/cleanup-batch，2026-08-29）：此前源文件
// 不可读/被锁或目标不可写都会静默蒸发，第一个进入 worktree 的会话才发现 .env 缺失，
// 在远离根因的地方撞上一个莫名其妙的失败。警告列出每个失败的 include，让用户在
// 会话开始前修复根因。
func copyWorktreeIncludes(srcRoot, dstRoot string) {
	listPath := filepath.Join(srcRoot, "forge.worktreeinclude")
	data, err := os.ReadFile(listPath)
	if err != nil {
		return
	}
	var failed []string
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
				failed = append(failed, fmt.Sprintf("%s (mkdir: %v)", rel, err))
				continue
			}
			in, inErr := os.Open(m)
			if inErr != nil {
				failed = append(failed, fmt.Sprintf("%s (open: %v)", rel, inErr))
				continue
			}
			out, outErr := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o644)
			if outErr != nil {
				in.Close()
				failed = append(failed, fmt.Sprintf("%s (write: %v)", rel, outErr))
				continue
			}
			if _, cpErr := io.Copy(out, in); cpErr != nil {
				failed = append(failed, fmt.Sprintf("%s (copy: %v)", rel, cpErr))
			}
			out.Close()
			in.Close()
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "[forge] warning: %d 个 forge.worktreeinclude 条目复制失败（worktree 已创建，start 不中断；缺的文件会让首个会话踩坑，建议先修复）:\n  %s\n",
			len(failed), strings.Join(failed, "\n  "))
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
		// 绑定指向的任务已消失（abort/删除）：按任务清扫全部死锚绑定。
		_ = worktree.ClearAllForTask(root, binding.TaskRef)
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
	// EvalSymlinks 两侧比较（review LOW 残留）：binding.Path 存 Abs 未解链接
	//（macOS /tmp→/private/tmp），文本比较会假阴性。
	if sameResolvedPath(wtPath, mainRoot) {
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
	if err := removeWorktree(mainRoot, wtPath); err != nil {
		return fmt.Errorf("worktree 清理失败（合并已完成，可手动 git worktree remove）: %w", err)
	}
	// dogfood 发现 #4（二次修订）：按 ID 经主检出解绑——worktree 目录已消失，
	// Clear(原路径) 的 DataDirFor 身份推导漂移成静默 no-op，绑定残留。首次修订
	// 因替换未落盘而假修（教训：字符串替换必须断言命中数），本行由 ClearByID
	// 按绑定存储的 wtid 直删 + TaskRef 比对。
	_ = worktree.ClearByID(mainRoot, binding.ID, state.TaskRef)
	fmt.Printf("任务 %s 完成：已合并 %s，worktree 已清理\n", state.TaskRef, binding.Branch)
	return nil
}

// sameResolvedPath compares two paths after EvalSymlinks on both sides.
//
// sameResolvedPath 双侧 EvalSymlinks 后比较路径。
func sameResolvedPath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return a == b
}

// removeWorktree deletes a worktree with the Windows CWD-lock guard: POSIX allows
// deleting a process's current directory, Windows refuses ("Permission denied") — the
// finish/janitor caller very often sits INSIDE the worktree being removed (its own
// project window). Chdir to the main checkout first when so (branch CI 2026-08-27
// Windows failure). Clean-tree is verified by callers beforehand; the --force retry
// only bypasses git's dirty-refusal on an already-verified-clean tree (OS-level lock
// transients), never content.
//
// removeWorktree 删除 worktree，带 Windows CWD 锁防护：POSIX 允许删除进程当前目
// 录、Windows 拒绝（"Permission denied"）——finish/janitor 的调用方极常正坐在被删
// 的 worktree 里（自己的任务窗口）。命中时先 chdir 到主检出（2026-08-27 分支 CI
// Windows 失败）。调用方已先行验证树干净；--force 重试只绕过 git 对【已验证干净】
// 树的脏拒绝（OS 级锁瞬态），绝不绕过内容检查。
func removeWorktree(mainRoot, wtPath string) error {
	if cwd, err := os.Getwd(); err == nil && sameResolvedPath(cwd, wtPath) || isUnderDir(cwd, wtPath) {
		_ = os.Chdir(mainRoot)
	}
	out, err := exec.Command("git", "-C", mainRoot, "worktree", "remove", wtPath).CombinedOutput()
	if err == nil {
		return nil
	}
	out2, err2 := exec.Command("git", "-C", mainRoot, "worktree", "remove", "--force", wtPath).CombinedOutput()
	if err2 == nil {
		return nil
	}
	return fmt.Errorf("%v\n%s\n%v\n%s", err, out, err2, out2)
}

// isUnderDir reports whether path is strictly inside dir.
//
// isUnderDir 报告 path 是否严格位于 dir 内。
func isUnderDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
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
		if err := removeWorktree(mainRoot, b.Path); err != nil {
			reported++
			fmt.Printf("⚠ 清理失败: %s: %v\n", b.Path, err)
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
