package cli

import (
	"encoding/json"
	"fmt"
	"strings"

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
	Long:  `forge skills usage — 合并两路触达信号（toollog.jsonl 的主动 Skill 工具调用 + checklog.jsonl 的被动 skill-trigger 触发），与 canonical skill 集交叉，输出热门排名与从未触发列表。两源均 agent-neutral，替代断链的 pi 旧源（~/.pi/research/skill-usage.jsonl）。被动触发是更大信号——skill-trigger 每个匹配事件都触发，而 Skill 工具只在显式加载时调用，故只数主动会低估触达。`,
	RunE:  runSkillsUsage,
}

func runSkillsUsage(cmd *cobra.Command, args []string) error {
	proj, err := findProject()
	if err != nil {
		return err
	}
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	rep, err := skillseval.AnalyzeUsage(proj.GitRoot, canonical)
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

	fmt.Printf("Skill 使用度量  (源: toollog 主动调用 + checklog 被动触发 · agent-neutral)\n")
	fmt.Printf("总 skill 触达: %d  |  canonical skill 数: %d  |  被用过: %d\n\n", rep.TotalEvents, rep.TotalSkills, rep.UsedSkills)

	top := rep.HotSkills
	if skUseTop > 0 && skUseTop < len(top) {
		top = top[:skUseTop]
	}
	fmt.Printf("=== 热门 skill Top %d ===\n", len(top))
	for _, h := range top {
		bar := strings.Repeat("█", min(h.Count, 30))
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
