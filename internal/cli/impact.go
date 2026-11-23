// Package cli impact.go — `forge impact`：按 diff 列影响面的只读入口。
//
// 减法重构的一环：把"改完要同步哪些文档、哪些源码还没有测试"这类能用
// 代码判定的问题从 agent 的脑内推理挪到一条确定性命令里。它不判断语义
// 影响（那需要人/评审），只报告 diff 事实。
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/MjxUpUp/Forge/internal/impact"
	"github.com/spf13/cobra"
)

var impactCmd = &cobra.Command{
	Use:   "impact",
	Short: "按 diff 列出影响面（文档/无测试源码）",
	Long: `forge impact 读取 git 工作区相对 HEAD 的改动与未跟踪文件，报告：
改动的文件、需要同步的文档、改了但同目录没有 _test.go 的源码，以及建议先跑的测试目录。
只做能由代码判定的部分——语义上的影响面（谁依赖我）不在这里猜。`,
	Args: cobra.NoArgs,
	RunE: runImpact,
}

func init() {
	impactCmd.GroupID = "pipeline"
	rootCmd.AddCommand(impactCmd)
	impactCmd.Flags().Bool("json", false, "JSON 格式输出")
}

func runImpact(cmd *cobra.Command, args []string) error {
	// 不在 forge 项目里也允许看：影响面是 git 事实，不依赖仓库契约。
	// findProjectRoot 失败时退到当前目录，Collect 自己会报"不是 git 仓库"。
	root, err := findProjectRoot()
	if err != nil {
		wd, werr := os.Getwd()
		if werr != nil {
			return err
		}
		root = wd
	}

	rep, err := impact.Collect(root)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		out, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(out))
		return nil
	}
	renderImpact(w, rep)
	return nil
}

// renderImpact 渲染文本报告。抽成纯函数便于单测（不依赖 findProjectRoot /
// 全局 cobra 状态）。
func renderImpact(w io.Writer, r *impact.Report) {
	if len(r.Changed) == 0 {
		fmt.Fprintln(w, "没有相对 HEAD 的改动。")
		return
	}
	fmt.Fprintf(w, "改动 %d 个文件\n", len(r.Changed))

	if len(r.Docs) > 0 {
		fmt.Fprintln(w, "文档（需要同步）：")
		for _, p := range r.Docs {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	if len(r.SourcesWithoutTest) > 0 {
		fmt.Fprintln(w, "改了源码但没有测试文件：")
		for _, p := range r.SourcesWithoutTest {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	if len(r.SuggestedTests) > 0 {
		fmt.Fprintln(w, "建议先跑的测试目录：")
		for _, p := range r.SuggestedTests {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	if len(r.TestState) > 0 {
		fmt.Fprintln(w, "测试账本（这些目录里的测试现状）：")
		for _, ts := range r.TestState {
			fmt.Fprintf(w, "  %s  live %d / orphaned %d / skipped %d", ts.Dir, ts.Live, ts.Orphaned, ts.Skipped)
			if ts.SkippedUnapproved > 0 {
				fmt.Fprintf(w, "（未审批 %d）", ts.SkippedUnapproved)
			}
			fmt.Fprintln(w)
		}
	}
}
