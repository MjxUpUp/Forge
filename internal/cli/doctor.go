package cli

import (
	"encoding/json"
	"fmt"
	"github.com/MjxUpUp/Forge/internal/cliskills"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/doctor"
	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/spf13/cobra"
)

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

// skillsDriftProbe 支撑 doctor 的 skills 分发节：canonical vs 全部已安装全局目标，
// 以 missing/drift 计数 + 可处置条目呈现。守护的根因（2026-08 审计）：上次
// `forge skills install` 之后新增进 canonical 的 skill（subagent-orchestration，
// 8-16）静默缺 5 个 host 目标——没有东西扫这条缝（doctor 只查 host 二进制版本，
// drift-check 存在但没人跑）。探针错误进摘要 Error 字段（绝不中止审计、也绝不
// 伪装成零计数健康态）；DriftCheck 的 per-target 部分失败经 TargetErrors 呈现。
//
// 已安装目标门控（M-3，2026-08-21）：agent home 不存在的目标（无 ~/.claude、
// ~/.cursor……）记入 Skipped、不审计——审计它只会把每个 canonical skill 报成
// missing 于一台从未装过的 agent，一墙不可处置的噪声淹没真实缺口。已安装 agent
// 但缺 skills 目录的仍被审计（真缺口）。`forge skills drift-check` 在显式全量
// 问询下保留不门控的全目标覆盖。
func skillsDriftProbe() *doctor.SkillsDriftSummary {
	canonical, _, err := cliskills.ResolveCanonical()
	if err != nil {
		return &doctor.SkillsDriftSummary{Error: err.Error()}
	}
	targets, err := cliskills.ParseSkillTargets([]string{"all"})
	if err != nil {
		return &doctor.SkillsDriftSummary{Error: err.Error()}
	}
	// 先解析目录再门控：目标的 agent home（skills 目录的父目录）存在才审计。
	// 排序保证 Skipped 顺序确定。
	dirs, err := skillsdist.TargetDirs(targets, true, "")
	if err != nil {
		return &doctor.SkillsDriftSummary{Error: err.Error()}
	}
	var audited []skillsdist.Target
	var skipped []string
	var damaged []string
	for _, name := range slices.Sorted(maps.Keys(dirs)) {
		// Three states, three signals (review L-5 + follow-up, 2026-08-22): home dir
		// exists → audit; home path absent → Skipped (uninstalled, advisory line);
		// home path is a regular FILE → damage, NOT uninstalled — doctor is the tool
		// that should surface it, so it goes to TargetErrors (⚠ line) instead of
		// masquerading as "跳过未安装目标".
		//
		// 三态三信号（评审 L-5 + 后续，2026-08-22）：home 目录存在→审计；home
		// 路径缺席→Skipped（未安装，advisory 行）；home 路径是普通文件→损坏态
		// 而非未安装——doctor 正是应当发现它的工具，进 TargetErrors（⚠ 行）
		// 而不是冒充「跳过未安装目标」。
		info, statErr := os.Stat(filepath.Dir(dirs[name]))
		switch {
		case statErr == nil && info.IsDir():
			audited = append(audited, skillsdist.Target(name))
		case statErr == nil:
			damaged = append(damaged, name)
		default:
			skipped = append(skipped, name)
		}
	}
	rep, err := skillsdist.DriftCheck(canonical, skillsdist.InstallOpts{Targets: audited, Global: true})
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
		Skipped:      skipped,
	}
	// 损坏态 home 走 TargetErrors：在审计产出输出的每个状态下渲染为 ⚠ 行，且让
	// 渲染器把「全跳过」判定翻成「无可审计目标」而非绿色 in-sync（只有损坏态
	// home 的机器 Skipped 为空——此处零计数绝不能读作健康）。
	for _, name := range damaged {
		s.TargetErrors = append(s.TargetErrors, fmt.Sprintf("%s 的 home 路径是普通文件而非目录（损坏态）——跳过审计，请检查该 agent 安装", name))
	}
	for _, it := range rep.Items {
		if it.State != skillsdist.StateMissing && it.State != skillsdist.StateDrift {
			continue
		}
		s.Items = append(s.Items, doctor.SkillsDriftItem{Skill: it.Name, Target: it.Target, State: it.State})
	}
	// 全量条目走 JSON / `forge skills drift-check`；人类可读渲染层单独截断展示
	// （带截断标记）——在此截断会把真实计数对机器消费方隐藏。
	return s
}

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
			// 死探针绝不能渲染成零计数健康态——审计没跑，就没有可打勾的东西。
			fmt.Fprintf(w, "  ✗ 审计失败: %s\n", s.Error)
		} else if s.Linked == 0 && s.CopySync == 0 && s.Missing == 0 && s.Drifted == 0 && (len(s.Skipped) > 0 || len(s.TargetErrors) > 0) {
			// 全部目标被跳过或损坏（本机无任何可用 agent home）：此处零计数=「没审计到
			// 东西」而非「全部健康」——渲染为独立的跳过态而非绿色 in-sync 行（H-1 伪装
			// 模式）。TargetErrors 纳入判定是因为 home 全是损坏文件的机器 Skipped 为
			// 空——它的零计数同样不能读作健康。
			if len(s.Skipped) > 0 {
				fmt.Fprintf(w, "  · 无已安装目标可审计（跳过: %s）\n", strings.Join(s.Skipped, ", "))
			} else {
				fmt.Fprintln(w, "  · 无已安装目标可审计（全部 home 处于损坏态）")
			}
			for _, e := range s.TargetErrors {
				fmt.Fprintf(w, "  ⚠ %s\n", e)
			}
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
			// 跳过的目标（agent home 不存在——M-3）：一行 advisory，绝不制造 missing
			// 噪声。非错误态都展示，让人读输出时始终看到审计的覆盖边界。
			if len(s.Skipped) > 0 {
				fmt.Fprintf(w, "  · 跳过未安装目标（无 home 目录）: %s\n", strings.Join(s.Skipped, ", "))
			}
		}
	}
}
