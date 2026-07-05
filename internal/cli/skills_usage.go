package cli

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var (
	skUseTop          int
	skUseUndertrigger bool
	skUseJSON         bool
)

var skillsUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "使用度量分析（热门 skill + 从未触发的 undertrigger 候选）",
	Long:  `forge skills usage — 读 ~/.forge/research/skill-usage.jsonl，与 canonical skill 集交叉，输出热门排名与从未触发列表。`,
	RunE:  runSkillsUsage,
}

func runSkillsUsage(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	logPath, err := skillseval.DefaultUsageLog()
	if err != nil {
		return err
	}
	rep, err := skillseval.AnalyzeUsage(canonical, logPath)
	if err != nil {
		return err
	}

	if skUseJSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	if skUseUndertrigger {
		fmt.Printf("=== 从未触发的 skill（%d/%d）— undertrigger 候选 ===\n", len(rep.NeverTriggered), rep.TotalSkills)
		for _, s := range rep.NeverTriggered {
			fmt.Printf("  %s\n", s)
		}
		return nil
	}

	fmt.Printf("Skill 使用度量  (日志: %s)\n", logPath)
	fmt.Printf("总事件: %d  |  skill 数: %d  |  被用过: %d\n\n", rep.TotalEvents, rep.TotalSkills, rep.UsedSkills)

	top := rep.HotSkills
	if skUseTop > 0 && skUseTop < len(top) {
		top = top[:skUseTop]
	}
	fmt.Printf("=== 热门 skill Top %d ===\n", len(top))
	for _, h := range top {
		bar := ""
		for i := 0; i < h.Count && i < 30; i++ {
			bar += "█"
		}
		fmt.Printf("  %-32s %3d %s\n", h.Name, h.Count, bar)
	}
	fmt.Printf("\n=== 从未触发（%d/%d）===\n", len(rep.NeverTriggered), rep.TotalSkills)
	limit := 15
	for i, s := range rep.NeverTriggered {
		if i >= limit {
			fmt.Printf("  ... 还有 %d 个\n", len(rep.NeverTriggered)-limit)
			break
		}
		fmt.Printf("  %s\n", s)
	}
	return nil
}

func init() {
	skillsUsageCmd.Flags().IntVar(&skUseTop, "top", 10, "热门 skill 显示数量")
	skillsUsageCmd.Flags().BoolVar(&skUseUndertrigger, "undertrigger", false, "只看从未触发的 skill")
	skillsUsageCmd.Flags().BoolVar(&skUseJSON, "json", false, "JSON 输出")
	skillsCmd.AddCommand(skillsUsageCmd)
}
