package cli

// skills_eval_report.go - eval-report subcommand: regression comparison of latest run vs baseline.
// Defaults to printing only NetRegressions + Regressions + pass-rate delta (signal first);
// --verbose prints the full three-state breakdown; --json emits machine-readable RegressionReport.
//
// skills_eval_report.go — eval-report 子命令：latest run vs baseline 的回归比对。
// 默认只打 NetRegressions + Regressions + pass-rate delta（信号优先）；--verbose 打
// 全量三态；--json 输出机器可读 RegressionReport。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	if err := requireValidSkillName(skRepSkill); err != nil {
		return err
	}
	dir, err := evalDataDir()
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

	// baseline selection: --baseline explicit run-id > skill-marked baseline > none (absolute score).
	//
	// baseline 选择：--baseline 显式 run-id > 该 skill 标记的 baseline > 无（绝对分）。
	baseline, err := resolveReportBaseline(dir, skRepSkill, skRepBaseline)
	if err != nil {
		return err
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

// resolveReportBaseline selects the baseline run for eval-report:
// explicit run-id > skill-marked baseline > none (absolute score).
// GetBaseline/LoadRunByID errors are propagated (previously both were swallowed,
// silently degrading to nil baseline and letting the report lie "未锚定" when the
// anchor read actually failed). A marked baseline whose run file has since been
// cleaned (LoadRunByID returns nil, nil) degrades to absolute scoring with a
// stderr warning — the mark is stale metadata, not a hard failure.
//
// resolveReportBaseline 为 eval-report 选择 baseline run：
// 显式 run-id > 该 skill 标记的 baseline > 无（绝对分）。
// GetBaseline/LoadRunByID 的 error 一律传播（此前双双被 _ 吞掉，baseline 静默变
// nil，报告谎称「未锚定」而实际只是读取失败）。标记的 baseline run 文件已被清理
// （LoadRunByID 返 nil, nil）时 stderr 警告并降级为绝对分——标记是过期元数据，
// 不是硬失败。
func resolveReportBaseline(dir, skill, explicit string) (*skillseval.EvalRun, error) {
	if explicit != "" {
		baseline, err := skillseval.LoadRunByID(dir, skill, explicit)
		if err != nil {
			return nil, fmt.Errorf("加载 baseline run %q 失败: %w", explicit, err)
		}
		if baseline == nil {
			return nil, fmt.Errorf("baseline run %q 不存在", explicit)
		}
		return baseline, nil
	}
	bl, err := skillseval.GetBaseline(dir, skill)
	if err != nil {
		return nil, fmt.Errorf("读取 skill %q 的 baseline 标记失败: %w", skill, err)
	}
	if bl.RunID == "" {
		return nil, nil
	}
	baseline, err := skillseval.LoadRunByID(dir, skill, bl.RunID)
	if err != nil {
		return nil, fmt.Errorf("加载 baseline run %q 失败: %w", bl.RunID, err)
	}
	if baseline == nil {
		fmt.Fprintf(os.Stderr, "⚠️ baseline run %q 已不存在（标记过期），降级为绝对分\n", bl.RunID)
		return nil, nil
	}
	return baseline, nil
}

// printEvalReport emits a human-readable report. Defaults to compact (signal first); verbose prints the full three-state breakdown.
//
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
		// Without a baseline, matched/new/removed are meaningless; only absolute pass counts matter.
		//
		// 无 baseline 时 matched/new/removed 无意义，只看绝对 pass 数。
		var pass int
		for _, r := range latest.Results {
			if r.Pass {
				pass++
			}
		}
		fmt.Printf("绝对通过：%d/%d\n", pass, len(latest.Results))
	}

	// Machine verdict (JudgeSkillAccept): deterministic accept/reject + reasons. The
	// skill-evolution SKILL decides based on this line (not self-report - replaces
	// agent self-reported accept). Always shown (signal first; visible even without --verbose).
	//
	// 机器判据（JudgeSkillAccept）：deterministic accept/reject + reasons。skill-evolution
	// SKILL 据此行决策（非自述，取代 agent 自报 accept）。永远显示（信号优先，非 verbose 也显）。
	fmt.Println(formatJudgeVerdict(rep))
}

// formatJudgeVerdict formats the JudgeSkillAccept verdict into a single-line
// human-readable string. Extracted for unit testing and to keep printEvalReport
// focused on printing. The skill-evolution SKILL reads the machine-verdict
// accept/reject line for its decision.
//
// formatJudgeVerdict 把 JudgeSkillAccept 的判据格式化成单行人类可读串。抽出便于单测 +
// 让 printEvalReport 只管打印。skill-evolution SKILL 读「机器判据：accept/reject」行决策。
func formatJudgeVerdict(rep *skillseval.RegressionReport) string {
	accept, reasons := skillseval.JudgeSkillAccept(rep)
	if accept {
		if len(reasons) == 0 {
			return `机器判据：accept（无退化信号）`
		}
		return fmt.Sprintf(`机器判据：accept（advisory：%s）`, strings.Join(reasons, `; `))
	}
	return fmt.Sprintf(`机器判据：reject（%s）`, strings.Join(reasons, `; `))
}

// formatRateDelta formats the baseline/latest pass-rate into a readable delta. Without a baseline, only the latest is printed.
//
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
