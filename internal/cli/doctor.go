package cli

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/doctor"
	"github.com/spf13/cobra"
)

// doctorCmd — `forge doctor`: cross-agent environment consistency audit.
//
// Answers the question that only surfaces during incidents: are all my agent hosts
// wired to forge, and do their hooks all resolve to the SAME forge binary version?
// (Real incidents behind this: kimi stuck on 1.28.4 while 1.29.0 shipped; a stray
// manually-built forge.exe in npm-global root winning PATHEXT over the run.js shim;
// stale PATH binaries producing false-positive audits.) Read-only — never mutates any
// host config.
//
// copilot is deliberately absent: its VS Code extension keeps config in extension
// storage with no stable file-path convention to scan (the known caveat since the
// copilot root hooks.json wiring).
//
// doctorCmd —— `forge doctor`：跨 agent 环境一致性审计。
//
// 回答只在事故时才暴露的问题：我所有 agent host 都接上 forge 了吗？它们的 hook
// 解析到的是同一个 forge 二进制版本吗？（背后真实事故：kimi 停在 1.28.4 而
// 1.29.0 已发；npm-global 根下手动 build 的 forge.exe 靠 PATHEXT 抢过 run.js shim；
// PATH 旧二进制产出假阳性审计。）只读——从不改动任何 host 配置。
//
// copilot 刻意缺席：其 VS Code 扩展把配置放在扩展存储里，没有可扫描的稳定文件
// 路径约定（自 copilot 根 hooks.json 接线起的已知 caveat）。
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Audit multi-agent forge environment consistency",
	Long: `Audit cross-agent forge environment consistency.

Scans every supported agent host (claude-code, codex, cursor, windsurf, kimi,
reasonix, codebuddy, cline, opencode) for forge hook wiring, resolves which forge
binary each host's hooks invoke, and compares versions against the running forge.
Also lists every forge executable on PATH in resolution order — multiple hits are
the classic stray-exe-vs-shim setup.

Statuses per host:
  ok       wired, version matches the running forge
  drift    wired, version differs (the headline finding)
  nover    wired, but binary/version could not be resolved
  missing  no forge wiring found

Read-only: never modifies any host configuration. Use --json for machine output.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		rep := doctor.Run(getCurrentVersion(rootCmd.Version), doctor.Options{})
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		printDoctor(cmd, rep)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("json", false, "Output full report as JSON")
}

// statusGlyph maps host status to display glyph. Aligned with Report JSON values —
// display sugar only, the wire values stay ASCII-stable.
//
// statusGlyph 把 host 状态映射为展示字形。与 Report JSON 值对齐——仅展示层糖，线上
// 值保持 ASCII 稳定。
func statusGlyph(s string) string {
	switch s {
	case doctor.StatusOK:
		return "✓"
	case doctor.StatusDrift:
		return "≠"
	case doctor.StatusNoVer:
		return "?"
	default:
		return "·"
	}
}

// printDoctor renders the human-readable report. Compact by design — the interesting
// lines are the drifts and the PATH list; everything ok collapses to one line per host.
//
// printDoctor 渲染人类可读报告。刻意紧凑——有趣的行是 drift 与 PATH 列表；全部 ok
// 时每个 host 压缩到一行。
func printDoctor(cmd *cobra.Command, rep doctor.Report) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "forge %s\n", rep.SelfVersion)
	if rep.Resolved != "" {
		fmt.Fprintf(w, "resolved on PATH: %s\n", rep.Resolved)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "hosts:")
	for _, h := range rep.Hosts {
		line := fmt.Sprintf("  %s %-11s %s", statusGlyph(h.Status), h.Host, h.Status)
		if h.Version != "" {
			line += fmt.Sprintf("  (%s)", h.Version)
		}
		if h.Bin != "" {
			line += fmt.Sprintf("  ← %s", h.Bin)
		}
		if h.Err != "" {
			line += fmt.Sprintf("  [%s]", h.Err)
		}
		fmt.Fprintln(w, line)
	}
	if len(rep.PathForge) > 1 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "PATH 上有 %d 个 forge 可执行文件（按解析顺序）:\n", len(rep.PathForge))
		for _, e := range rep.PathForge {
			v := e.Version
			if v == "" {
				v = "(version unknown)"
			}
			fmt.Fprintf(w, "  %s  %s\n", v, e.Path)
		}
		fmt.Fprintln(w, "多个 forge 并存时，Windows PATHEXT/Unix PATH 顺序决定谁被执行——清理旧副本。")
	}
}
