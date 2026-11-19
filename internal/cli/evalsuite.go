package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/evalsuite"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/spf13/cobra"
)

var evalSuiteCmd = &cobra.Command{
	Use:   "suite [--only <change>] [--max N] [--json] [--record]",
	Short: "跑归档 change 的 acceptance 判据（仓库级回归基线）",
	Long: `把已归档 change 的 acceptance 围栏当作回归基线逐条实跑：
改一处、跑所有历史判据，看整体有没有退化。每条判据都在 shell 里跑，
单条超时 10 分钟；失败以非零退出，但不阻断任何门禁。

围栏里写 ` + "`# suite: skip`" + ` 的整条跳过（重判据归 change 自己的 verify）。
默认只跑最近 5 条；--max 0 表示全跑。--record 把结果写进 .forge/evals/ 并追加索引。`,
	Args: cobra.NoArgs,
	RunE: runEvalSuite,
}

var evalTrendCmd = &cobra.Command{
	Use:   "trend [--last N] [--json]",
	Short: "看 eval suite 的通过率趋势",
	Args:  cobra.NoArgs,
	RunE:  runEvalTrend,
}

func init() {
	evalSuiteCmd.Flags().String("only", "", "只跑某个 change 的判据（按 change id）")
	evalSuiteCmd.Flags().Int("max", 5, "最多跑多少条判据（0 = 不限）")
	evalSuiteCmd.Flags().Bool("json", false, "JSON 输出（纯 JSON，可直接 jq）")
	evalSuiteCmd.Flags().Bool("record", false, "把结果写入 .forge/evals/ 并追加索引")
	evalTrendCmd.Flags().Int("last", 10, "看最近多少次运行（0 = 全部）")
	evalTrendCmd.Flags().Bool("json", false, "JSON 输出")
	evalCmd.AddCommand(evalSuiteCmd)
	evalCmd.AddCommand(evalTrendCmd)
}

func runEvalSuite(cmd *cobra.Command, _ []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	only, _ := cmd.Flags().GetString("only")
	max, _ := cmd.Flags().GetInt("max")
	asJSON, _ := cmd.Flags().GetBool("json")
	record, _ := cmd.Flags().GetBool("record")

	cases, err := evalsuite.Load(root, only, max)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		if asJSON {
			fmt.Println("[]")
		} else {
			fmt.Println("没有可跑的回归判据（spec/changes/archive/*/acceptance.md；围栏可写 # suite: skip 跳过）")
		}
		return nil
	}
	head := gitHead(root)
	rec := evalsuite.Run(root, cases, head)

	if asJSON {
		raw, _ := json.MarshalIndent(rec, "", "  ")
		fmt.Println(string(raw))
	} else {
		for _, r := range rec.Results {
			mark := "✅"
			if !r.Passed {
				mark = "❌"
			}
			fmt.Printf("  %s %s#%d  %s（%dms）\n", mark, r.Change, r.Fence, firstLineOfCommand(r.Command), r.DurationMS)
			if !r.Passed {
				if reason := failureReason(r); reason != "" {
					fmt.Printf("     %s\n", reason)
				}
			}
		}
		fmt.Printf("回归基线：%d/%d 通过（HEAD %s）\n", rec.Passed, rec.Total, head)
	}
	if record {
		path, err := evalsuite.Save(root, rec)
		if err != nil {
			return err
		}
		if !asJSON {
			fmt.Printf("已记录：%s\n", path)
		}
	}
	if rec.Passed != rec.Total {
		return fmt.Errorf("回归基线有 %d 条判据失败", rec.Total-rec.Passed)
	}
	return nil
}

// failureReason 优先给执行错误（找不到命令、超时），否则给输出首行——
// 只打印 ❌ 而不给原因，等于让人再去翻 JSON。
func failureReason(r evalsuite.Result) string {
	if r.Error != "" {
		return r.Error
	}
	if out := strings.TrimSpace(r.Output); out != "" {
		lines := strings.Split(out, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if l := strings.TrimSpace(lines[i]); l != "" {
				return l
			}
		}
	}
	return ""
}

func runEvalTrend(cmd *cobra.Command, _ []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	last, _ := cmd.Flags().GetInt("last")
	asJSON, _ := cmd.Flags().GetBool("json")
	rows, err := evalsuite.Trend(root, last)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if asJSON {
			fmt.Println("[]")
		} else {
			fmt.Println("还没有评估记录（先跑 forge eval suite --record）")
		}
		return nil
	}
	if asJSON {
		raw, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(raw))
		return nil
	}
	for _, r := range rows {
		fmt.Printf("  %v  %v/%v 通过（HEAD %v）\n", r["at"], r["passed"], r["total"], r["head"])
	}
	return nil
}

func gitHead(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstLineOfCommand(s string) string {
	s = strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]
	r := []rune(s)
	if len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}
