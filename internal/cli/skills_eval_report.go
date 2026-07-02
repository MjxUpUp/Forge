package cli

// skills_eval_report.go — eval-report 子命令：latest run vs baseline 的回归比对。
// 默认只打 NetRegressions + Regressions + pass-rate delta（信号优先）；--verbose 打
// 全量三态；--json 输出机器可读 RegressionReport。

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var (
	skRepSkill    string
	skRepBaseline string
	skRepVerbose  bool
	skRepJSON     bool
)

var skillsEvalReportCmd = &cobra.Command{
	Use:   "eval-report",
	Short: "比对 latest run vs baseline，输出回归报告（regression 三态 + 健康度/pass-rate delta）",
	RunE:  runSkillsEvalReport,
}

func runSkillsEvalReport(cmd *cobra.Command, args []string) error {
	if skRepSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	dir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}

	latest, err := skillseval.LatestRun(dir, skRepSkill)
	if err != nil {
		return err
	}
	if latest == nil {
		return fmt.Errorf("skill %q 还没有 run——先 eval-record", skRepSkill)
	}

	// baseline 选择：--baseline 显式 run-id > 该 skill 标记的 baseline > 无（绝对分）。
	var baseline *skillseval.EvalRun
	if skRepBaseline != "" {
		baseline, err = skillseval.LoadRunByID(dir, skRepSkill, skRepBaseline)
		if err != nil {
			return err
		}
		if baseline == nil {
			return fmt.Errorf("baseline run %q 不存在", skRepBaseline)
		}
	} else if bl, _ := skillseval.GetBaseline(dir, skRepSkill); bl.RunID != "" {
		baseline, _ = skillseval.LoadRunByID(dir, skRepSkill, bl.RunID)
	}

	rep := skillseval.CompareRuns(latest, baseline)

	if skRepJSON {
		out, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	printEvalReport(rep, latest, baseline, skRepVerbose)
	return nil
}

// printEvalReport 输出人类可读报告。默认精简（信号优先），verbose 打全量三态。
func printEvalReport(rep *skillseval.RegressionReport, latest, baseline *skillseval.EvalRun, verbose bool) {
	fmt.Printf("skill: %s\n", rep.Skill)
	fmt.Printf("latest: %s  health=%.2f\n", rep.LatestRun, latest.HealthScore)
	if !rep.HasBaseline {
		fmt.Println("baseline: 无（绝对分，未锚定——首跑或未 eval-baseline）")
	} else {
		fmt.Printf("baseline: %s\n", rep.BaselineRun)
	}
	if rep.HasBaseline && !rep.Comparable {
		fmt.Printf("⚠️ 不可比：%s（回归数字降级为 advisory）\n", rep.IncomparableReason)
	}
	fmt.Printf("trigger pass-rate:    %s\n", formatRateDelta(rep.TriggerPassRateBaseline, rep.TriggerPassRateLatest, rep.HasBaseline))
	fmt.Printf("not-trigger pass-rate:%s\n", formatRateDelta(rep.NotTriggerPassRateBaseline, rep.NotTriggerPassRateLatest, rep.HasBaseline))

	if rep.HasBaseline {
		fmt.Printf("net regressions: %d（regressions=%d, improvements=%d）\n",
			rep.NetRegressions, len(rep.Regressions), len(rep.Improvements))
		for _, r := range rep.Regressions {
			fmt.Printf("  🔴 回归 %s  actual=%q\n", r.CaseID, r.ActualTriggered)
		}
		if verbose {
			for _, r := range rep.Improvements {
				fmt.Printf("  🟢 改善 %s\n", r.CaseID)
			}
			for _, r := range rep.Stable {
				fmt.Printf("  ⚪ 稳定 %s\n", r.CaseID)
			}
			for _, r := range rep.New {
				fmt.Printf("  ✨ 新增 %s（无 baseline，不计回归）\n", r.CaseID)
			}
			for _, r := range rep.Removed {
				fmt.Printf("  🗑 移除 %s（case 集换血，不计回归）\n", r.CaseID)
			}
		}
	} else {
		// 无 baseline 时 matched/new/removed 无意义，只看绝对 pass 数。
		var pass int
		for _, r := range latest.Results {
			if r.Pass {
				pass++
			}
		}
		fmt.Printf("绝对通过：%d/%d\n", pass, len(latest.Results))
	}
}

// formatRateDelta 把 baseline/latest pass-rate 格式化成可读 delta。无 baseline 时只打 latest。
func formatRateDelta(base, latest float64, hasBaseline bool) string {
	if !hasBaseline {
		return fmt.Sprintf(" %.0f%%", latest*100)
	}
	arrow := "→"
	if latest < base {
		arrow = "↓"
	} else if latest > base {
		arrow = "↑"
	}
	return fmt.Sprintf(" %.0f%% %s %.0f%%", base*100, arrow, latest*100)
}

func init() {
	skillsEvalReportCmd.Flags().StringVar(&skRepSkill, "skill", "", "比对哪个 skill")
	skillsEvalReportCmd.Flags().StringVar(&skRepBaseline, "baseline", "", "baseline run-id（默认用该 skill 标记的 baseline）")
	skillsEvalReportCmd.Flags().BoolVar(&skRepVerbose, "verbose", false, "打全量三态（stable/new/removed）")
	skillsEvalReportCmd.Flags().BoolVar(&skRepJSON, "json", false, "输出机器可读 RegressionReport JSON")
	skillsCmd.AddCommand(skillsEvalReportCmd)
}
