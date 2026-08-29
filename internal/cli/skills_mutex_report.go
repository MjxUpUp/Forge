package cli

// skills_mutex_report.go — mutex-report 子命令：最新互斥 run vs 当前互斥 case 集的
// 混淆矩阵。混淆行（actual == Negative——prompt 路由到的恰是声明过让渡的 skill）是
// 头号信号；--gate 给它牙齿（任一混淆行存在即 exit 4）。
//
// 门禁契约（对齐 skills battery --gate / skills audit --gate）：非零退出只在 --gate
// 模式；BLOCKED 行走 STDERR——`--json --gate | jq .` 不再吃到非 JSON 字节（退出码 +
// stderr 承载门禁信号；stdout 只做数据通道）。判定本身在
// skillseval.ConfusionMatrix.GateBlocked——os.Exit 留在本薄壳。

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/spf13/cobra"
)

var (
	skMRepGate bool
	skMRepJSON bool
)

var skillsMutexReportCmd = &cobra.Command{
	Use:   "mutex-report",
	Short: "互斥集混淆矩阵（latest run vs case 集；actual==Negative 为头号混淆行）",
	Long: `取最新互斥 run 与当前 case 集，输出 (positive, actual) 混淆矩阵。

  forge skills mutex-report            # 人读报告
  forge skills mutex-report --json     # 机器可读 MutexMatrix JSON
  forge skills mutex-report --gate     # 门禁模式：任一 actual==Negative → BLOCKED(stderr) + exit 4

无 run 时矩阵为空（total=0）：--gate 的 exit 0 意为「没检查任何东西」而非「已验证
无混淆」（该情形会打印显式 advisory 到 stderr）。`,
	RunE: runSkillsMutexReport,
}

func runSkillsMutexReport(cmd *cobra.Command, args []string) error {
	dir, err := evalDataDir()
	if err != nil {
		return err
	}
	cases, err := skillseval.LoadMutexCases(dir)
	if err != nil {
		return err
	}
	latest, err := skillseval.LatestMutexRun(dir)
	if err != nil {
		return err
	}
	m := skillseval.ConfusionMatrix(latest, cases)

	if skMRepJSON {
		out, merr := json.MarshalIndent(m, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Println(string(out))
	} else {
		printMutexMatrix(m)
	}

	if skMRepGate && m.GateBlocked {
		fmt.Fprintln(os.Stderr, "BLOCKED: 互斥集存在混淆行（actual==Negative）——B 域 prompt 路由回了声明让渡的 A，先修路由再放行")
		os.Exit(4)
	}
	// 空矩阵诚实性（理由同 battery）：零结果矩阵没检查任何东西就 exit 0——不该被
	// 读成「已验证无混淆」。
	if skMRepGate && m.Total == 0 {
		fmt.Fprintln(os.Stderr, "ADVISORY: 互斥集为空（无 run）——exit 0 意为未检查任何 case，非已验证无混淆；mutex-record 回填后才有保护")
	}
	return nil
}

// printMutexMatrix 渲染人读报告：混淆行先行（信号优先），再聚合矩阵，再逐 case 行。
func printMutexMatrix(m *skillseval.MutexMatrix) {
	fmt.Printf("互斥集混淆矩阵：%d 个 case（passed=%d confusions=%d）\n", m.Total, m.Passed, len(m.Confusions))
	for _, c := range m.Confusions {
		fmt.Printf("  🔴 confusion %s  应→%s 实→%s（头号混淆：%s 声明过让渡）\n", c.CaseID, c.Positive, c.Actual, c.Negative)
		fmt.Printf("     prompt: %s\n", c.Prompt)
	}
	for _, cell := range m.Cells {
		fmt.Printf("  (%s, %s) × %d\n", cell.Positive, displayActual(cell.Actual), cell.Count)
	}
	if m.Total == 0 {
		fmt.Println("（无互斥 run——mutex-record 回填后报告生效）")
	}
}

// displayActual 把空 actual（未触发任何 skill）在矩阵里可读化。
func displayActual(actual string) string {
	if actual == "" {
		return "(none)"
	}
	return actual
}

func init() {
	skillsMutexReportCmd.Flags().BoolVar(&skMRepGate, "gate", false, "门禁模式：任一 actual==Negative → BLOCKED + exit 4")
	skillsMutexReportCmd.Flags().BoolVar(&skMRepJSON, "json", false, "输出机器可读 MutexMatrix JSON")
	skillsCmd.AddCommand(skillsMutexReportCmd)
}
