// Package cli conventions.go — `forge conventions` 命令组：conventions-profile
// 层 1（发现并建档）的用户入口。init 机械扫描目标仓库已声明的规范
// （AGENTS.md 一族 / lint 配置 / 工具链命令）写入 per-project 档案；show 查看
// 档案与过期状态。注入（层 2）由 hook 侧 hook_conventions.go 承担，不经此命令。
//
// Package cli conventions.go — the `forge conventions` command group: the user
// entry of conventions-profile layer 1 (discover & profile). `init`
// mechanically scans the target repo's DECLARED conventions (AGENTS.md family /
// lint configs / toolchain commands) into the per-project profile; `show`
// prints the profile and its staleness. Injection (layer 2) lives in the
// hook-side hook_conventions.go, not in this command.
package cli

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

var conventionsInitForce bool

// conventionsCmd 是命令组父命令（无自身动作，Short 承担 README 守卫 B 的
// `forge conventions` 收录锚点）。
var conventionsCmd = &cobra.Command{
	Use:   "conventions",
	Short: "项目规范档案：扫描目标仓库既有规范建档（AGENTS.md/lint 配置/工具链），会话注入摘要",
	Long: "项目规范档案（conventions-profile 层 1）：机械扫描目标仓库已声明的编码规范——\n" +
		"AGENTS.md/CLAUDE.md/CONVENTIONS.md 等规范文件、lint/format 配置、stack 工具链命令——\n" +
		"写入 per-project 档案（~/.forge/projects/<key>/conventions/）。\n" +
		"每次会话开始 hook 注入摘要（≤15 行），写源码文件时注入规范文件指针与同目录范例。\n" +
		"源文件变更后指纹翻转，提示重扫。摘要的「提取要点」节由 agent 代码考古后增补。",
}

// conventionsInitCmd 扫描建档。--force 才会覆盖已存在的 summary.md（保护
// agent/人工提炼的内容不被机械重建冲掉——档案元数据可再生成，提炼内容不可）。
var conventionsInitCmd = &cobra.Command{
	Use:   "init",
	Short: "扫描目标仓库已声明的规范，建立/刷新 conventions 档案",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("not a forge project — run `forge init` first (hooks only fire in forge projects; a profile elsewhere would be write-only)")
		}
		p, err := conventions.Scan(root)
		if err != nil {
			return err
		}
		dataDir := forgedata.DataDirFor(root)
		if err := conventions.SaveProfile(dataDir, p); err != nil {
			return fmt.Errorf("save profile: %w", err)
		}
		summaryKept := false
		if conventions.SummaryExists(dataDir) && !conventionsInitForce {
			summaryKept = true
		} else if err := conventions.SaveSummary(dataDir, conventions.GenerateSummary(p)); err != nil {
			return fmt.Errorf("save summary: %w", err)
		}
		printConventionsInitReport(root, dataDir, p, summaryKept)
		return nil
	},
}

// conventionsShowCmd 打印档案元数据 + 过期状态 + 摘要全文。
var conventionsShowCmd = &cobra.Command{
	Use:   "show",
	Short: "查看 conventions 档案（元数据/过期状态/摘要全文）",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("not a forge project — run `forge init` first")
		}
		dataDir := forgedata.DataDirFor(root)
		p, err := conventions.LoadProfile(dataDir)
		if err != nil {
			// Corrupt profile: plain init repairs it (SaveProfile always
			// rewrites metadata and KEEPS the enriched summary) — pointing at
			// --force here would needlessly destroy the curated digest
			// (adversarial-review finding, 2026-08-28).
			//
			// 档案损坏：普通 init 即可修复（SaveProfile 恒重写元数据且**保留**
			// 已提炼摘要）——此处指向 --force 会无谓摧毁提炼内容
			// （2026-08-28 对抗审查发现）。
			return fmt.Errorf("conventions profile unreadable (rebuild with `forge conventions init`): %w", err)
		}
		if p == nil {
			fmt.Println("无 conventions 档案——先运行 `forge conventions init` 扫描建档。")
			return nil
		}
		stale := conventions.Stale(root, p)
		var b strings.Builder
		fmt.Fprintf(&b, "root: %s\n", root)
		fmt.Fprintf(&b, "stack: %s", p.Stack)
		if p.Stack == "" {
			b.WriteString("（未识别）")
		}
		b.WriteString("\n")
		for _, kv := range []struct{ k, v string }{
			{"lint", p.LintCmd}, {"test", p.TestCmd}, {"build", p.BuildCmd},
		} {
			if kv.v != "" {
				fmt.Fprintf(&b, "%s: %s\n", kv.k, kv.v)
			}
		}
		if len(p.Instructions) > 0 {
			fmt.Fprintf(&b, "规范声明文件: %s\n", strings.Join(conventions.Paths(p.Instructions), ", "))
		} else {
			b.WriteString("规范声明文件: （无——仓库未声明 AGENTS.md/CLAUDE.md 等）\n")
		}
		if len(p.StyleConfigs) > 0 {
			fmt.Fprintf(&b, "lint/format 配置: %s\n", strings.Join(conventions.Paths(p.StyleConfigs), ", "))
		}
		if p.CursorRules > 0 {
			fmt.Fprintf(&b, "cursor rules: %d 个\n", p.CursorRules)
		}
		fmt.Fprintf(&b, "fingerprint: %s（updated %s）\n", p.Fingerprint, p.Updated)
		if stale {
			b.WriteString("过期状态: STALE——规范源文件已变化，重跑 `forge conventions init` 刷新档案\n")
		} else {
			b.WriteString("过期状态: 与当前树一致\n")
		}
		if summary := conventions.LoadSummary(dataDir); summary != "" {
			b.WriteString("\n──── summary.md ────\n")
			b.WriteString(summary)
		} else {
			b.WriteString("\n（summary.md 不存在——`forge conventions init` 生成骨架）\n")
		}
		fmt.Print(b.String())
		return nil
	},
}

func init() {
	conventionsInitCmd.Flags().BoolVar(&conventionsInitForce, "force", false, "覆盖已存在的 summary.md（默认保留 agent/人工提炼的内容，只刷新档案元数据）")
	conventionsCmd.AddCommand(conventionsInitCmd)
	conventionsCmd.AddCommand(conventionsShowCmd)
	rootCmd.AddCommand(conventionsCmd)
}

// printConventionsInitReport prints the scan report plus the agent-enrichment
// next step (the P1 hook: mechanical scan states WHAT is declared; extraction
// of unwritten conventions from the code itself is the agent's archaeology).
//
// printConventionsInitReport 打印扫描报告 + agent 增补的下一步（P1 挂点：
// 机械扫描陈述「已声明了什么」；从代码本身提取未成文的惯例是 agent 的考古活）。
func printConventionsInitReport(root, dataDir string, p *conventions.Profile, summaryKept bool) {
	fmt.Printf("扫描完成: %s\n", root)
	if p.Stack != "" {
		fmt.Printf("  stack: %s", p.Stack)
		for _, kv := range []struct{ k, v string }{
			{"lint", p.LintCmd}, {"test", p.TestCmd}, {"build", p.BuildCmd},
		} {
			if kv.v != "" {
				fmt.Printf(" · %s: %s", kv.k, kv.v)
			}
		}
		fmt.Println()
	}
	if len(p.Instructions) > 0 {
		fmt.Printf("  规范声明文件: %s\n", strings.Join(conventions.Paths(p.Instructions), ", "))
	}
	if len(p.StyleConfigs) > 0 {
		fmt.Printf("  lint/format 配置: %s\n", strings.Join(conventions.Paths(p.StyleConfigs), ", "))
	}
	fmt.Printf("已写入档案: %s\n", conventions.ProfilePath(dataDir))
	if summaryKept {
		fmt.Printf("summary.md 已存在，保留未动（--force 可覆盖重建）: %s\n", conventions.SummaryPath(dataDir))
	} else {
		fmt.Printf("已生成摘要骨架: %s\n", conventions.SummaryPath(dataDir))
	}
	fmt.Println()
	fmt.Println("下一步（agent）：对仓库做代码考古，把命名/错误处理/目录结构/import 与注释风格等")
	fmt.Println("惯例逐条写进 summary.md 的「提取要点」节（整份保持 ≤15 行）——每次会话开始 forge")
	fmt.Println("会注入这份摘要；规范源文件变化后指纹翻转，forge conventions show 会提示重扫。")
}
