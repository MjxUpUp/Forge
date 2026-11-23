// Package cli test.go — `forge test` 命令组：测试账本、执行记录、增量变异探针。
//
// 三条子命令的分工：record 负责把 `go test -json` 的真实执行变成可累积的证据；
// ledger 负责按证据给全仓测试定四态（没有记录 = orphaned，不是"通过"）；
// probe 负责对一段 diff 里的源文件抽样变异，回答"改的那几行测试抓得住吗"。
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/mutprobe"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/testledger"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "测试账本与增量变异探针",
	Long: `forge test 管"测试到底跑没跑、跳过有没有交代、改了的地方测试抓不抓得住"。

  forge test record   把 go test -json 的执行结果记进 .forge/test-exec.jsonl
  forge test ledger   按执行记录把全仓测试分成 live / orphaned / skipped / phantom
  forge test probe    只对一段 diff 命中的 Go 源文件做变异抽样（增量 mutation）`,
}

var testLedgerCmd = &cobra.Command{
	Use:   "ledger",
	Short: "按执行记录给全仓测试定四态",
	Long: `读取 go 测试的执行记录（默认 .forge/test-exec.jsonl）与源码里的 t.Skip，
把每条测试判成四态之一：

  live      有声明，且有新鲜执行记录
  orphaned  有声明，但没有（足够新的）执行记录
  skipped   源码里调用了 t.Skip*（理由与审批分开检查）
  phantom   执行记录里有，源码里已经没有了

没有记录的测试一律判 orphaned——"不确定跑没跑"就是一种需要处理的状态。
--strict 会把「Skip 缺理由」「Skip 未审批」「phantom」判为失败（退出码非 0）。`,
	Args: cobra.NoArgs,
	RunE: runTestLedger,
}

var testRecordCmd = &cobra.Command{
	Use:   "record",
	Short: "把 go test -json 的执行结果记进账本",
	Long:  "从 stdin 或 --in 文件读 go test -json 事件流，只留 pass/fail/skip 且带测试名的记录，追加到 --out（默认 .forge/test-exec.jsonl）。",
	Args:  cobra.NoArgs,
	RunE:  runTestRecord,
}

var testProbeCmd = &cobra.Command{
	Use:   "probe",
	Short: "对一段 diff 做增量变异抽样",
	Long: `只取 --since 之后 diff 命中的非测试 Go 源文件，复用变异算子抽样实跑所在包测试。

这与全仓/任务级抽样是同一个引擎，区别只在文件来源：这里由 git 回答"改了什么"，
不由 agent 自报。sample<=0 取默认 3。`,
	Args: cobra.NoArgs,
	RunE: runTestProbe,
}

func init() {
	testCmd.GroupID = "pipeline"
	rootCmd.AddCommand(testCmd)
	testCmd.AddCommand(testLedgerCmd, testRecordCmd, testProbeCmd)

	testLedgerCmd.Flags().Bool("json", false, "JSON 格式输出")
	testLedgerCmd.Flags().String("record", filepath.Join(".forge", "test-exec.jsonl"), "执行记录文件")
	testLedgerCmd.Flags().String("approvals", filepath.Join("spec", "test-skips.yaml"), "Skip 审批清单")
	testLedgerCmd.Flags().Duration("max-age", testledger.DefaultMaxAge, "执行记录的新鲜期（超过即不算 live）")
	testLedgerCmd.Flags().Bool("strict", false, "缺理由 / 未审批 / phantom 判失败")

	testRecordCmd.Flags().String("in", "-", "go test -json 输入（- 表示 stdin）")
	testRecordCmd.Flags().String("out", filepath.Join(".forge", "test-exec.jsonl"), "记录追加到的文件")

	testProbeCmd.Flags().String("since", "HEAD", "只取该 rev 之后 diff 命中的文件")
	testProbeCmd.Flags().Int("sample", 3, "抽样变异体数量（<=0 取默认）")
	testProbeCmd.Flags().Bool("json", false, "JSON 格式输出")
}

func projectRootOrCwd() string {
	if root, err := findProjectRoot(); err == nil {
		return root
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func runTestLedger(cmd *cobra.Command, _ []string) error {
	root := projectRootOrCwd()
	recPath, _ := cmd.Flags().GetString("record")
	apPath, _ := cmd.Flags().GetString("approvals")
	maxAge, _ := cmd.Flags().GetDuration("max-age")
	asJSON, _ := cmd.Flags().GetBool("json")
	strict, _ := cmd.Flags().GetBool("strict")

	decls, err := testledger.ScanDecls(root)
	if err != nil {
		return fmt.Errorf("扫描测试声明: %w", err)
	}
	recs, bad, err := testledger.LoadRecords(filepath.Join(root, recPath))
	if err != nil {
		return fmt.Errorf("读取执行记录: %w", err)
	}
	approvals, err := testledger.LoadSkipApprovals(filepath.Join(root, apPath))
	if err != nil {
		return err
	}
	l := testledger.Build(decls, recs, testledger.Options{MaxAge: maxAge, Approvals: approvals})

	w := cmd.OutOrStdout()
	if asJSON {
		out, err := json.MarshalIndent(l, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(out))
	} else {
		renderTestLedger(w, l, len(recs), bad)
	}
	if strict {
		if v := l.Violations(); len(v) > 0 {
			return fmt.Errorf("测试账本 --strict：%d 处违规（见上）", len(v))
		}
	}
	return nil
}

// renderTestLedger 是纯渲染函数，便于单测。
func renderTestLedger(w io.Writer, l *testledger.Ledger, records, bad int) {
	fmt.Fprintf(w, "测试账本（记录 %d 条", records)
	if bad > 0 {
		fmt.Fprintf(w, "，损坏行 %d", bad)
	}
	fmt.Fprintln(w, "）")
	for _, st := range []testledger.State{testledger.StateLive, testledger.StateOrphaned, testledger.StateSkipped, testledger.StatePhantom} {
		fmt.Fprintf(w, "  %-8s %d", st, l.Counts[st])
		if st == testledger.StateSkipped {
			unapproved := 0
			for _, e := range l.Entries {
				if e.State == testledger.StateSkipped && !e.SkipApproved {
					unapproved++
				}
			}
			if unapproved > 0 {
				fmt.Fprintf(w, "（未审批 %d）", unapproved)
			}
		}
		fmt.Fprintln(w)
	}
	guarded := 0
	for _, e := range l.Entries {
		if e.GuardedSkip {
			guarded++
		}
	}
	if guarded > 0 {
		fmt.Fprintf(w, "  条件跳过 %d（if 里的 t.Skip：环境/平台闸门，平时照常跑，不计入 skipped）\n", guarded)
	}
	if records == 0 {
		fmt.Fprintln(w, "\n还没有执行记录——先在仓里跑 go test -json ./... 并把输出接进 forge test record；没有记录的测试一律算 orphaned。")
	}
	if v := l.Violations(); len(v) > 0 {
		fmt.Fprintln(w, "\n需要交代的（--strict 会判失败）：")
		for _, line := range v {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}
}

func runTestRecord(cmd *cobra.Command, _ []string) error {
	in, _ := cmd.Flags().GetString("in")
	out, _ := cmd.Flags().GetString("out")
	var r io.Reader = os.Stdin
	if in != "" && in != "-" {
		f, err := os.Open(in)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	recs, err := testledger.ParseExecJSON(r)
	if err != nil {
		return err
	}
	root := projectRootOrCwd()
	if err := testledger.AppendRecords(filepath.Join(root, out), recs); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "已记录 %d 条执行结果 → %s\n", len(recs), out)
	return nil
}

func runTestProbe(cmd *cobra.Command, _ []string) error {
	root := projectRootOrCwd()
	since, _ := cmd.Flags().GetString("since")
	sample, _ := cmd.Flags().GetInt("sample")
	asJSON, _ := cmd.Flags().GetBool("json")
	files, err := mutprobe.ChangedGoSources(root, since)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "该 rev 之后没有改动的 Go 源文件——没有可抽样的位点。")
		return nil
	}
	res, err := mutprobe.RunOn(root, files, sample)
	if err != nil {
		return err
	}
	if asJSON {
		out, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "增量探针（since %s，文件 %d 个）\n%s", since, len(files), taskpipeline.FormatMutationSummary(res))
	return nil
}

var _ = strings.TrimSpace
var _ = time.Now
