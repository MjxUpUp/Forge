package cli

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// next.go —— `forge next`：单命令引导（vNext P1-2，nextSteps 单命令语义——agent 不自选下一步）。
//
// 设计依据（2026-08-31 三轮调研）：8-30 事故的第四层是"任务入口自愿"——agent 自
// 选下一步于是绕开整个生命周期。next 从 git/任务状态推导出**恰好一条**下一步命令
// +理由，agent 的职责从"自己判断"变成"照单执行"（pull 侧引导，与 push 侧 hook 执
// 法互补——巡警 vs 火警的分工）。状态推导只读：git（分支/脏树）+ ActiveTaskState
// （门禁 History/ReviewPassed/CompletedAt）。
func init() {
	rootCmd.AddCommand(nextCmd)
	nextCmd.Flags().Bool("json", false, "JSON 格式输出（agent 协议主形态）")
}

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "推导恰好一条下一步命令（从 git/任务状态——agent 不自选下一步）",
	Long: `Derive the single next command from current git + task state.

覆盖：无任务有脏树 → task start（或 wild 申报）；任务进行中 → 门禁链下一步
（implement → 验收实跑 → verify → review pass → complete 门 → complete）。
--json 是 agent 的机器接口：{"next","reason","state"}。ActiveTaskState 对已
完结任务返回 nil，故完成后的合并收尾不在本命令承诺内（用 forge task list /
git merge）。`,
	RunE: runNext,
}

// nextResult 是 forge next 的输出契约。Next 恒非空（无事可做时回落 status）。
type nextResult struct {
	Next   string         `json:"next"`
	Reason string         `json:"reason"`
	State  map[string]any `json:"state"`
}

func runNext(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	sid := taskpipeline.CurrentSessionID()
	st, _ := taskpipeline.ActiveTaskState(root, sid)
	branch, dirty := taskpipeline.GitBranchDirty(root)
	res := nextDecision(branch, dirty, st)

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(res)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "下一步：%s\n理由：%s\n", res.Next, res.Reason)
	return nil
}

// nextDecision 委托 taskpipeline.NextDecision（设计 B：决策逻辑下沉，gate/status/complete
// 输出点与 `forge next` 共用同一真相源——clitask 不能反向 import cli）。纯函数单测锚点在
// taskpipeline/next_test.go。
func nextDecision(branch string, dirty bool, st *taskpipeline.TaskState) taskpipeline.NextResult {
	return taskpipeline.NextDecision(branch, dirty, st)
}
