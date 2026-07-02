package cli

// skills_eval_baseline.go — eval-baseline 子命令：显式标记某 skill 的 baseline run。
// baseline = 已验证可发布点，是人工决策——不自动。不带 --run-id 默认标 latest run。

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var (
	skBaseSkill string
	skBaseRun   string
)

var skillsEvalBaselineCmd = &cobra.Command{
	Use:   "eval-baseline",
	Short: "标记某 skill 的 baseline run（已验证可发布点；eval-report 的回归基准）",
	RunE:  runSkillsEvalBaseline,
}

func runSkillsEvalBaseline(cmd *cobra.Command, args []string) error {
	if skBaseSkill == "" {
		return fmt.Errorf("需要 --skill NAME")
	}
	dir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}

	runID := skBaseRun
	if runID == "" {
		// 默认标 latest run。
		latest, err := skillseval.LatestRun(dir, skBaseSkill)
		if err != nil {
			return err
		}
		if latest == nil {
			return fmt.Errorf("skill %q 还没有 run，无法设 baseline——先 eval-record", skBaseSkill)
		}
		runID = latest.RunID
	} else {
		// 显式 run-id 需校验存在。
		got, err := skillseval.LoadRunByID(dir, skBaseSkill, runID)
		if err != nil {
			return err
		}
		if got == nil {
			return fmt.Errorf("run %q 不存在", runID)
		}
	}

	if err := skillseval.SetBaseline(dir, skBaseSkill, runID, "cli"); err != nil {
		return err
	}
	fmt.Printf("✅ baseline: %s → %s\n", skBaseSkill, runID)
	return nil
}

func init() {
	skillsEvalBaselineCmd.Flags().StringVar(&skBaseSkill, "skill", "", "为哪个 skill 设 baseline")
	skillsEvalBaselineCmd.Flags().StringVar(&skBaseRun, "run-id", "", "baseline run-id（默认 latest run）")
	skillsCmd.AddCommand(skillsEvalBaselineCmd)
}
