package clitask

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// checkpoint 是「动手前的存档点」：不改工作树、不产生可见提交，
// 用 git stash create 把当前 index+worktree 收成一个提交，再用
// refs/forge/checkpoints/<id> 把它钉住（裸 SHA 会被 git gc 回收）；
// 回滚时 git stash apply 原样放回。它治的是「agent 改乱了想退回上一步」
// 这个高频场景——git 自己能做，但状态机需要记账（谁、何时、回过哪次）。
const (
	maxCheckpoints    = 20
	checkpointRefRoot = "refs/forge/checkpoints/"
)

var taskCheckpointCmd = &cobra.Command{
	Use:   "checkpoint [--label <说明>] [--list]",
	Short: "给当前工作树存一个快照（不改工作树、不产生 git log 可见的提交）",
	Long: `把当前 index+worktree 收成一个快照（git stash create 的对象 + refs/forge/checkpoints/<id> 保活），
记在任务状态里；回滚用 forge task rollback <id>。适合「agent 要动大范围改动前先存一点」。

只含已跟踪文件：工作区有未跟踪新文件时直接拒绝（git stash create 收不进去，
静默漏存会让回滚丢文件）。保留最近 20 个；--list 看已有存档点。`,
	Args: cobra.NoArgs,
	RunE: runTaskCheckpoint,
}

var taskRollbackCmd = &cobra.Command{
	Use:   "rollback <checkpoint-id>",
	Short: "把工作树恢复到某个存档点（git stash apply）",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskRollback,
}

func init() {
	taskCheckpointCmd.Flags().String("label", "", "存档点说明")
	taskCheckpointCmd.Flags().Bool("list", false, "只列出现有存档点")
	Root.AddCommand(taskCheckpointCmd)
	Root.AddCommand(taskRollbackCmd)
}

// activeTaskForCheckpoint 取活跃任务；无任务时 ActiveTaskState 返回 (nil,nil)，
// 必须显式判空，否则 --list 与 rollback 都是 nil panic。
func activeTaskForCheckpoint() (string, *taskpipeline.TaskState, error) {
	root, err := projectroot.Find()
	if err != nil {
		return "", nil, err
	}
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return "", nil, fmt.Errorf("没有活跃任务（先 forge task start）：%w", err)
	}
	if state == nil || state.TaskRef == "" {
		return "", nil, fmt.Errorf("没有活跃任务（先 forge task start）")
	}
	return root, state, nil
}

func runTaskCheckpoint(cmd *cobra.Command, _ []string) error {
	root, state, err := activeTaskForCheckpoint()
	if err != nil {
		return err
	}
	if list, _ := cmd.Flags().GetBool("list"); list {
		printCheckpoints(state)
		return nil
	}
	label, _ := cmd.Flags().GetString("label")

	untracked, err := ckGitOut(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	if strings.TrimSpace(untracked) != "" {
		return fmt.Errorf("工作区有未跟踪文件（%s）——git stash create 收不进去，回滚会丢它们；先 git add 或提交，再 checkpoint",
			firstLines(untracked, 3))
	}

	head, err := ckGitOut(root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	// stash create 不修改 worktree：它返回一个提交对象，内容=当前 index+worktree。
	ref, err := ckGitOut(root, "stash", "create")
	if err != nil {
		return fmt.Errorf("工作树快照失败：%w", err)
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("工作区没有可快照的改动——先改点东西，或用 git commit 记录已有进度")
	}

	var cpID, refName string
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(st *taskpipeline.TaskState) error {
		cpID = nextCheckpointID(st.Checkpoints)
		refName = checkpointRefRoot + cpID
		// refs/ 命名空间保活：裸 SHA 会在 gc 后失效，用户回滚时只剩「对象不可达」。
		if _, err := ckGitOut(root, "update-ref", refName, ref); err != nil {
			return fmt.Errorf("钉住存档点引用失败：%w", err)
		}
		st.Checkpoints = append(st.Checkpoints, taskpipeline.Checkpoint{
			ID:    cpID,
			Ref:   refName,
			Head:  strings.TrimSpace(head),
			At:    time.Now().UTC().Format(time.RFC3339),
			Label: label,
		})
		for len(st.Checkpoints) > maxCheckpoints {
			drop := st.Checkpoints[0]
			st.Checkpoints = st.Checkpoints[1:]
			if strings.HasPrefix(drop.Ref, checkpointRefRoot) {
				_, _ = ckGitOut(root, "update-ref", "-d", drop.Ref)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("已存档：%s（%s）· HEAD %s · `forge task rollback %s` 放回\n", cpID, refName, shortSHA(head), cpID)
	fmt.Println("注意：只含已跟踪文件（index+worktree），不含构建产物、数据库等副产物；暂存状态回滚后变回未暂存。")
	return nil
}

func runTaskRollback(_ *cobra.Command, args []string) error {
	root, state, err := activeTaskForCheckpoint()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	target, dup := findCheckpoint(state.Checkpoints, id)
	if target == nil {
		printCheckpoints(state)
		return fmt.Errorf("没有 %q 这个存档点", id)
	}
	if dup {
		return fmt.Errorf("存档点 ID %q 有多条（历史重复 ID）——请用 --list 看时间与 ref，或先清空重存；拒绝猜测回滚目标", id)
	}
	if _, err := ckGitOut(root, "cat-file", "-e", target.Ref+"^{commit}"); err != nil {
		return fmt.Errorf("存档点 %s 的 git 对象已不可达：%w", id, err)
	}
	if err := ckGitRun(root, "stash", "apply", target.Ref); err != nil {
		return fmt.Errorf("回滚失败（工作树可能有冲突，请在 git status 里处理）：%w", err)
	}
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(st *taskpipeline.TaskState) error {
		st.RolledBackTo = target.ID
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("已回滚到 %s（%s）。stash apply 只放回内容，冲突需手工处理。\n", target.ID, target.Ref)
	return nil
}

func nextCheckpointID(cps []taskpipeline.Checkpoint) string {
	max := 0
	for _, cp := range cps {
		if strings.HasPrefix(cp.ID, "cp-") {
			if n, err := strconv.Atoi(strings.TrimPrefix(cp.ID, "cp-")); err == nil && n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("cp-%d", max+1)
}

func findCheckpoint(cps []taskpipeline.Checkpoint, id string) (*taskpipeline.Checkpoint, bool) {
	var hit *taskpipeline.Checkpoint
	dup := false
	for i := range cps {
		if strings.EqualFold(cps[i].ID, id) {
			if hit != nil {
				dup = true
				continue
			}
			hit = &cps[i]
		}
	}
	return hit, dup
}

func printCheckpoints(state *taskpipeline.TaskState) {
	if len(state.Checkpoints) == 0 {
		fmt.Println("还没有存档点（forge task checkpoint --label <说明>）")
		return
	}
	for _, cp := range state.Checkpoints {
		mark := ""
		if state.RolledBackTo == cp.ID {
			mark = "  ← 最近回滚到此"
		}
		fmt.Printf("  %s  %s  HEAD %s  %s  %s%s\n", cp.ID, cp.At, shortSHA(cp.Head), cp.Ref, cp.Label, mark)
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		return strings.Join(lines[:n], ", ") + " 等"
	}
	return strings.Join(lines, ", ")
}

func ckGitOut(root string, args ...string) (string, error) {
	return ckGitCombined(root, args...)
}

func ckGitRun(root string, args ...string) error {
	_, err := ckGitCombined(root, args...)
	return err
}

// ckGitCombined 保留 stderr：git 的报错（如 fatal: Not a valid object name）是
// 用户唯一能看懂的那句话，丢掉后只剩 exit status 128。
func ckGitCombined(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}
