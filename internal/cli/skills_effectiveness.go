package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var skEffJSON bool

var skillsEffectivenessCmd = &cobra.Command{
	Use:   "effectiveness",
	Short: "skill 命中×task 成效关联（agent-neutral 评估信号）",
	Long: `forge skills effectiveness — 关联 toollog 的 Skill 调用与 act conclusion 的 task 成效，
产出每个 skill 的平均评分、证据强度 ratio、弱证据(Weak/Unverified)占比。全 deterministic，
无 agent 评分——MUSE/Voyager 式"复用率+成功率"信号在 Forge 的实现。

数据源（agent-neutral）：toollog（tool-track 采集）+ act 结论（评分+证据链）。任何 agent
跑的 task 都有 act 结论，任何装了 forge hook 的 host 都记 toollog，缺数据时返回空不报错。`,
	RunE: runSkillsEffectiveness,
}

func runSkillsEffectiveness(cmd *cobra.Command, args []string) error {
	proj, err := findProject()
	if err != nil {
		return err
	}
	effs, err := skillseval.AnalyzeEffectiveness(proj)
	if err != nil {
		return err
	}
	if len(effs) == 0 {
		fmt.Println("尚无 skill 成效数据（需要：toollog 记录了 Skill 调用 + 对应 task 有 act 结论）。")
		return nil
	}
	if skEffJSON {
		b, _ := json.MarshalIndent(effs, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Println("Skill 成效画像（命中 × task 成效，agent-neutral）：")
	fmt.Println(strings.Repeat("─", 72))
	fmt.Printf("  %-28s %5s %6s %7s %7s %8s\n", "skill", "hits", "tasks", "avg分", "ratio", "弱占比")
	for _, e := range effs {
		fmt.Printf("  %-28s %5d %6d %7.0f %7.2f %7.0f%%\n",
			e.Skill, e.HitCount, e.TaskCount, e.AvgScore, e.AvgRatio, e.WeakRate*100)
	}
	fmt.Println(strings.Repeat("─", 72))
	fmt.Println("弱占比 = 该 skill 涉及 task 中 Strength=Weak/Unverified 的比例（高分但没真验证的盲区）。")
	return nil
}

func init() {
	skillsEffectivenessCmd.Flags().BoolVar(&skEffJSON, "json", false, "JSON 输出")
	skillsCmd.AddCommand(skillsEffectivenessCmd)
}
