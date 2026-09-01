package cliskills

// skills_analyze.go — analyze 子命令：一站式只读弱点挖掘报告。把项目的确定信号
//（维度弱点/验证盲区/从未触发/低成效 skill）聚成一册供人选题。只报告——无门禁、不自动改。

import (
	"encoding/json"
	"fmt"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var skAnaJSON bool

var skillsAnalyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "弱点挖掘报告（维度弱点/盲区率/从未触发/低成效 skill 聚簇，只读不决策）",
	Long: `把项目既有确定信号 join 成一册弱点报告（Self-Harness 弱点挖掘阶段，只报告）：

  维度弱点      跨任务复现 >=2 次的低分维度（<70）——共性缺口方向
  盲区率        完成声明靠 agent 自述（Unverified/Weak）的任务占比
  从未触发      canonical 里从未被触达的 skill——undertrigger/死重候选
  低成效 skill  涉及任务 >=2 且（弱证据占比>=50% 或 平均分<70）的 skill

报告只列证据不裁决；由此产生的修改走 decisions（--prediction 声明预测）+
battery（回归门禁）闭环。`,
	RunE: runSkillsAnalyze,
}

func runSkillsAnalyze(cmd *cobra.Command, args []string) error {
	proj, err := projectroot.FindProject()
	if err != nil {
		return err
	}
	canonical, _, err := ResolveCanonical()
	if err != nil {
		return err
	}
	rep, err := skillseval.AnalyzeWeaknesses(proj, canonical)
	if err != nil {
		return err
	}

	if skAnaJSON {
		out, merr := json.MarshalIndent(rep, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Println(string(out))
		return nil
	}
	printWeaknessReport(rep)
	return nil
}

// printWeaknessReport 渲染人读报告：每个信号簇带证据，caveat 收尾（报告看不见什么的诚实性）。
func printWeaknessReport(rep *skillseval.WeaknessReport) {
	fmt.Printf("弱点挖掘报告（只读证据，不裁决——修改走 decide --prediction + battery 闭环）\n")
	fmt.Printf("任务结论: %d 个  盲区率: %.0f%% (%d)  趋势: %s", rep.TotalTasks, rep.BlindSpotRate*100, rep.BlindSpotCount, rep.Trend)
	if rep.Trend != "" && rep.Trend != "insufficient" {
		fmt.Printf("（前半 %.1f → 后半 %.1f）", rep.EarlierAvg, rep.RecentAvg)
	}
	fmt.Println()

	if len(rep.WeakDims) > 0 {
		fmt.Printf("\n=== 维度弱点（复现 >=2 次）===\n")
		for _, d := range rep.WeakDims {
			fmt.Printf("  %-24s %d 次\n", d.Dimension, d.Count)
		}
	}
	if len(rep.NeverTriggered) > 0 {
		fmt.Printf("\n=== 从未触发的 skill（%d 个，undertrigger/死重候选）===\n", len(rep.NeverTriggered))
		limit := 15
		for i, s := range rep.NeverTriggered {
			if i >= limit {
				fmt.Printf("  ... 还有 %d 个\n", len(rep.NeverTriggered)-limit)
				break
			}
			fmt.Printf("  %s\n", s)
		}
	}
	if len(rep.LowEffectiveness) > 0 {
		fmt.Printf("\n=== 低成效 skill（>=2 任务且 弱证据>=50%% 或 均分<70）===\n")
		for _, e := range rep.LowEffectiveness {
			fmt.Printf("  %-28s tasks=%d hits=%d avg=%.1f weak=%.0f%%\n", e.Skill, e.TaskCount, e.HitCount, e.AvgScore, e.WeakRate*100)
		}
	}
	if len(rep.WeakDims) == 0 && len(rep.NeverTriggered) == 0 && len(rep.LowEffectiveness) == 0 {
		fmt.Printf("\n（三簇信号均无候选——见 caveat，可能是数据不足而非无弱点）\n")
	}
	if len(rep.DataCaveats) > 0 {
		fmt.Printf("\n=== 数据 caveat（防过度解读）===\n")
		for _, c := range rep.DataCaveats {
			fmt.Printf("  ⚠ %s\n", strings.TrimSpace(c))
		}
	}
}

func init() {
	skillsAnalyzeCmd.Flags().BoolVar(&skAnaJSON, "json", false, "JSON 输出")
	Root.AddCommand(skillsAnalyzeCmd)
}
