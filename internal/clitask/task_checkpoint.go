package clitask

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// checkpoint 是「动手前的存档点」：不改工作树、不产生可见提交，
// 用 git stash create 把当前 index+worktree 收成一个不可达提交，
// 回滚时 git stash apply 原样放回。它治的是「agent 改乱了想退回上一步」
// 这个高频场景——git 自己能做，但从状态机角度需要记账（谁、何时、回滚过没有）。
const (
	maxCheckpoints = 20
)

var taskCheckpointCmd = &cobra.Command{
	Use:   "checkpoint [--label <说明>] [--list]",
	Short: "给当前工作树存一个快照（不改工作树、不产生提交）",
	Long: `把当前 index+worktree 收成一个不可达提交记在任务状态里，回滚用 forge task rollback <id>。
适合「agent 要动大范围改动前先存一点」。保留最近 20 个；--list 看已有存档点。`,
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

func loadCheckpointTask() (string, *taskpipeline.TaskState, error) {
	root, err := projectroot.Find()
	if err != nil {
		return "", nil, err
	}
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return "", nil, fmt.Errorf("没有活跃任务（先 forge task start）：%w", err)
	}
	return root, state, nil
}

func runTaskCheckpoint(cmd *cobra.Command, _ []string) error {
	root, state, err := loadCheckpointTask()
	if err != nil {
		return err
	}
	if list, _ := cmd.Flags().GetBool("list"); list {
		printCheckpoints(state)
		return nil
	}
	label, _ := cmd.Flags().GetString("label")
	head, err := ckGitOut(root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return err
	}
	// stash create 不修改 worktree：它返回一个提交对象，内容=当前 index+worktree。
	ref, err := ckGitOut(root, "stash", "create")
	if err != nil {
		return fmt.Errorf("工作树快照失败：%w", err)
	}
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("工作区没有可快照的改动——先改点东西，或用 git commit 记录已有进度")
	}
	cp := taskpipeline.Checkpoint{
		ID:    fmt.Sprintf("cp-%d", len(state.Checkpoints)+1),
		Ref:   strings.TrimSpace(ref),
		Head:  strings.TrimSpace(head),
		At:    time.Now().UTC().Format(time.RFC3339),
		Label: label,
	}
	state.Checkpoints = append(state.Checkpoints, cp)
	if len(state.Checkpoints) > maxCheckpoints {
		state.Checkpoints = state.Checkpoints[len(state.Checkpoints)-maxCheckpoints:]
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return err
	}
	fmt.Printf("已存档：%s（%s）· HEAD %s · `forge task rollback %s` 放回\n", cp.ID, refShort(cp.Ref), cp.Head, cp.ID)
	fmt.Println("注意：快照是 git 层面的 index+worktree，不含构建产物、数据库等副产物。")
	return nil
}

func runTaskRollback(_ *cobra.Command, args []string) error {
	root, state, err := loadCheckpointTask()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	var target *taskpipeline.Checkpoint
	for i := range state.Checkpoints {
		if strings.EqualFold(state.Checkpoints[i].ID, id) {
			target = &state.Checkpoints[i]
			break
		}
	}
	if target == nil {
		printCheckpoints(state)
		return fmt.Errorf("没有 %q 这个存档点", id)
	}
	if _, err := ckGitOut(root, "cat-file", "-e", target.Ref+"^{commit}"); err != nil {
		return fmt.Errorf("存档点 %s 的 git 对象已不可达（可能被 gc 清理）：%w", id, err)
	}
	if err := ckGitRun(root, "stash", "apply", target.Ref); err != nil {
		return fmt.Errorf("回滚失败（工作树可能有冲突）：%w", err)
	}
	state.RolledBackTo = id
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return err
	}
	fmt.Printf("已回滚到 %s（%s）。stash apply 只放回内容，冲突需手工处理。\n", id, refShort(target.Ref))
	return nil
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
		fmt.Printf("  %s  %s  HEAD %s  %s%s\n", cp.ID, cp.At, cp.Head, cp.Label, mark)
	}
}

func refShort(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

func ckGitOut(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func ckGitRun(root string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
