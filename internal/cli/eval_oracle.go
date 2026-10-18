package cli

// eval_oracle.go — `forge eval oracle` 子命令：实跑沙箱 oracle 考卷
// （internal/evalkit/oracle.go 的 CLI 面）。默认 --arm=both：cheat 臂扫确定性
// 检测器，acceptance 臂在容器里跑可见/held-out 两遍。
//
// eval_oracle.go — CLI surface for the sandbox oracle suite.

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/evalkit"
	"github.com/spf13/cobra"
)

func newEvalOracleCmd() *cobra.Command {
	oracle := &cobra.Command{
		Use:   "oracle",
		Short: "沙箱 oracle v0（cheat 召回 / held-out 逃逸；考卷指纹可复算）",
	}
	run := &cobra.Command{
		Use:   "run [--suite <dir>] [--arm both|scanner|acceptance] [--repeats N] [--json] [--out <md>]",
		Short: "实跑考卷：确定性 cheat-scan 臂 + 容器化验收臂，输出指标与逐条证据",
		RunE:  runEvalOracle,
	}
	run.Flags().String("suite", "evals/oracle", "考卷目录（含 cases/、fixtures/、heldout/）")
	run.Flags().String("arm", "both", "臂：both|scanner|acceptance")
	run.Flags().Int("repeats", 1, "cheat 用例重复次数（>1 用于确定性漂移检测）")
	run.Flags().String("image", "", "验收臂镜像（默认 golang:1.25-alpine）")
	run.Flags().Bool("json", false, "输出 JSON 证据")
	run.Flags().String("out", "", "Markdown 报告落盘路径")
	oracle.AddCommand(run)
	return oracle
}

func runEvalOracle(cmd *cobra.Command, args []string) error {
	suite, _ := cmd.Flags().GetString("suite")
	arm, _ := cmd.Flags().GetString("arm")
	repeats, _ := cmd.Flags().GetInt("repeats")
	image, _ := cmd.Flags().GetString("image")
	asJSON, _ := cmd.Flags().GetBool("json")
	out, _ := cmd.Flags().GetString("out")

	switch arm {
	case "both", "scanner", "acceptance":
	default:
		return fmt.Errorf("BLOCKED: --arm %q 非法（both|scanner|acceptance）", arm)
	}

	rep, err := evalkit.RunOracleSuite(suite, evalkit.OracleOptions{
		Arm: arm, Repeats: repeats, Image: image,
	})
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}

	if asJSON {
		data, err := evalkit.MarshalOracleJSON(rep)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(evalkit.RenderOracleMarkdown(rep))
	}
	if out != "" {
		if err := os.WriteFile(out, []byte(evalkit.RenderOracleMarkdown(rep)), 0o644); err != nil {
			return fmt.Errorf("写报告失败: %w", err)
		}
		fmt.Printf("\n报告已写入 %s\n", out)
	}
	if rep.ErrorCases > 0 {
		return fmt.Errorf("BLOCKED: %d 条用例执行错误（见逐条结果）", rep.ErrorCases)
	}
	return nil
}
