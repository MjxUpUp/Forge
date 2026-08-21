package cli

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/doctor"
	"github.com/MjxUpUp/Forge/internal/skillsdist"
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
the classic stray-exe-vs-shim setup. Finally audits skills distribution: canonical
skills vs every global target directory (missing/drift surface with a fix command;
skills added to canonical after the last install are the classic silent gap).

Statuses per host:
  ok       wired, version matches the running forge
  drift    wired, version differs (the headline finding)
  nover    wired, but binary/version could not be resolved
  missing  no forge wiring found

Read-only: never modifies any host configuration. Use --json for machine output.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		rep := doctor.Run(getCurrentVersion(rootCmd.Version), doctor.Options{SkillsDriftProbe: skillsDriftProbe})
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

// skillsDriftProbe backs the doctor skills-distribution section: canonical vs every
// global target, surfaced as missing/drift counts with actionable items. Root cause
// it guards (2026-08 audit): a skill added to canonical after the last
// `forge skills install` (subagent-orchestration, 8-16) silently missed 5 host
// targets — nothing scanned that seam (doctor only checked host binary versions,
// drift-check existed but nothing ran it). Probe errors go into the summary's Error
// field (never abort the audit, never masquerade as a healthy zero-count state);
// per-target partial failures from DriftCheck surface via TargetErrors.
//
// skillsDriftProbe 支撑 doctor 的 skills 分发节：canonical vs 全部全局目标，以
// missing/drift 计数 + 可处置条目呈现。守护的根因（2026-08 审计）：上次
// `forge skills install` 之后新增进 canonical 的 skill（subagent-orchestration，
// 8-16）静默缺 5 个 host 目标——没有东西扫这条缝（doctor 只查 host 二进制版本，
// drift-check 存在但没人跑）。探针错误进摘要 Error 字段（绝不中止审计、也绝不
// 伪装成零计数健康态）；DriftCheck 的 per-target 部分失败经 TargetErrors 呈现。
func skillsDriftProbe() *doctor.SkillsDriftSummary {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return &doctor.SkillsDriftSummary{Error: err.Error()}
	}
	targets, err := parseSkillTargets([]string{"all"})
	if err != nil {
		return &doctor.SkillsDriftSummary{Error: err.Error()}
	}
	rep, err := skillsdist.DriftCheck(canonical, skillsdist.InstallOpts{Targets: targets, Global: true})
	if err != nil {
		return &doctor.SkillsDriftSummary{Error: err.Error()}
	}
	s := &doctor.SkillsDriftSummary{
		Canonical:    rep.Canonical,
		TargetErrors: rep.Errors,
		Linked:       rep.Stats.Linked,
		CopySync:     rep.Stats.CopyInSync,
		Missing:      rep.Stats.Missing,
		Drifted:      rep.Stats.Drift,
		Items:        []doctor.SkillsDriftItem{},
	}
	for _, it := range rep.Items {
		if it.State != skillsdist.StateMissing && it.State != skillsdist.StateDrift {
			continue
		}
		s.Items = append(s.Items, doctor.SkillsDriftItem{Skill: it.Name, Target: it.Target, State: it.State})
	}
	// The full item list rides the JSON / `forge skills drift-check`; the human
	// renderer caps display separately (with a truncation marker) — capping here
	// would hide the true count from machine consumers.
	//
	// 全量条目走 JSON / `forge skills drift-check`；人类可读渲染层单独截断展示
	// （带截断标记）——在此截断会把真实计数对机器消费方隐藏。
	return s
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
	if rep.Skills != nil {
		s := rep.Skills
		fmt.Fprintln(w)
		fmt.Fprintln(w, "skills distribution:")
		if s.Error != "" {
			// A dead probe must never render as a healthy zero-count state — the
			// audit did not run, so there is nothing to green-check.
			//
			// 死探针绝不能渲染成零计数健康态——审计没跑，就没有可打勾的东西。
			fmt.Fprintf(w, "  ✗ 审计失败: %s\n", s.Error)
		} else {
			if s.Missing == 0 && s.Drifted == 0 {
				fmt.Fprintf(w, "  ✓ in sync  (linked=%d copy-in-sync=%d)\n", s.Linked, s.CopySync)
			} else {
				fmt.Fprintf(w, "  ≠ missing=%d drift=%d  (linked=%d copy-in-sync=%d)\n", s.Missing, s.Drifted, s.Linked, s.CopySync)
				shown := s.Items
				if len(shown) > 20 {
					shown = shown[:20]
				}
				for _, it := range shown {
					fmt.Fprintf(w, "    · %-30s [%s] %s\n", it.Skill, it.Target, it.State)
				}
				if len(s.Items) > len(shown) {
					fmt.Fprintf(w, "    …(+%d more，全量见 forge skills drift-check)\n", len(s.Items)-len(shown))
				}
				fmt.Fprintln(w, "  修复：forge skills install --global --target all（详见 forge skills drift-check）")
			}
			for _, e := range s.TargetErrors {
				fmt.Fprintf(w, "  ⚠ %s\n", e)
			}
		}
	}
}
