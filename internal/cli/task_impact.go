package cli

import (
	"fmt"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// forge task impact 写任务的跨仓影响声明（TaskState.CrossRepoImpact）——
// task-verify cross-repo-impact 门禁（crossrepo.go）的写入侧。声明走
// MutateTaskState（per-task 锁，load→mutate→save），与其他改状态的 task
// 子命令一致。level=none 不携带 repo 列表（单仓改动声明「不波及其他仓」
// ——声明动作本身即意义）。

var taskImpactCmd = &cobra.Command{
	Use:   `impact`,
	Short: `声明当前任务的跨仓影响（多仓 workspace 的 verify 前置）`,
	Long: `forge task impact 声明当前活跃任务的跨仓影响，写入 task state 供 task-verify 门禁检查。

本 repo 属于多仓 workspace（forge workspace）时，task-verify 要求显式声明：
  forge task impact --level none                          改动纯本仓，不波及其他 repo
  forge task impact --level multi --repo <key> [--note]   波及指定成员 repo（可重复 --repo）

默认 advisory（未声明只提醒）；protocol.yml 配 cross_repo_impact: required 后未声明会阻断 verify。
详见 docs/design/multi-repo-workspace.md。`,
	RunE: runTaskImpact,
}

func runTaskImpact(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString(`ref`)
	level, _ := cmd.Flags().GetString(`level`)
	repos, _ := cmd.Flags().GetStringArray(`repo`)
	note, _ := cmd.Flags().GetString(`note`)

	if level != taskpipeline.CrossRepoNone && level != taskpipeline.CrossRepoMulti {
		return fmt.Errorf(`--level 必填且只接受 none | multi，got %q`, level)
	}
	// level=none 按契约忽略 --repo：「无影响」不携带目标列表——静默丢弃会掩盖
	// 用户手误，故明说。
	if level == taskpipeline.CrossRepoNone && len(repos) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), `提示：level=none 不携带 repo 列表，--repo 已忽略`)
		fmt.Fprintln(cmd.ErrOrStderr())
		repos = nil
	}

	var state *taskpipeline.TaskState
	if explicitRef != `` {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return err
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return fmt.Errorf("failed to load task state: %w", err)
		}
	}
	if state == nil {
		return fmt.Errorf("no active task. Run 'forge task start' first")
	}

	decl := &taskpipeline.CrossRepoImpact{
		Level:      level,
		Repos:      repos,
		Note:       note,
		DeclaredAt: time.Now(),
	}
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		s.CrossRepoImpact = decl
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}

	if level == taskpipeline.CrossRepoNone {
		fmt.Fprintf(cmd.OutOrStdout(), `✅ 已声明任务 %s 跨仓影响：none（改动限定本仓）`, state.TaskRef)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), `✅ 已声明任务 %s 跨仓影响：multi（波及 %d 个 repo）`, state.TaskRef, len(repos))
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
