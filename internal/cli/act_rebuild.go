package cli

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

func init() {
	actCmd.AddCommand(actRebuildCmd)
}

var actRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "从 tasks/*.json 重建 conclusions.jsonl（迁移 act 上线前的旧任务）",
	Long: `forge act rebuild 从已完成任务的 TaskState（.forge/tasks/*.json）重建 .forge/act/conclusions.jsonl。

背景：act 结论落盘是后加功能，act 上线前完成的任务没有 conclusions.jsonl，导致 forge
dashboard / act list 对旧项目显示空——dashboard 只读 conclusions.jsonl，不读 tasks/*.json。
rebuild 遍历所有有 Score+CompletedAt 的任务，从 TaskState + checklog 证据链重建结论
（复用 task complete 时的 appendConclusion 逻辑，单一真相源）。

现有 conclusions.jsonl 先备份到 .bak。rebuild 全量重建（幂等：多次跑结果一致，按完成时间排序）。`,
	RunE: runActRebuild,
}

func runActRebuild(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return err
	}
	backup, err := act.ResetForRebuild(root)
	if err != nil {
		return err
	}
	n := 0
	for _, state := range states {
		if state.Score == nil || state.CompletedAt == nil {
			continue
		}
		appendConclusion(root, state)
		n++
	}
	if backup != "" {
		fmt.Printf("已备份原 conclusions.jsonl → %s\n", backup)
	}
	fmt.Printf("重建 %d 条结论 → .forge/act/conclusions.jsonl（已完成任务 %d 个，跳过 %d 个未评分）\n",
		n, len(states), len(states)-n)
	fmt.Println("运行 forge dashboard 或 forge act list 查看数据。")
	return nil
}
