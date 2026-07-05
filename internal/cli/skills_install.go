package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/spf13/cobra"
)

var (
	skInstGlobal       bool
	skInstProject      bool
	skInstTarget       []string
	skInstSkill        []string
	skInstMode         string
	skInstDriftPolicy  string
	skInstSkipQuality  bool
	skInstWithAdapters bool
	skInstJSON         bool
)

var skillsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "分发 skill 到全局/项目目标目录（link/copy）",
	Long: `forge skills install — 把 canonical skill 库分发到 AI 工具目标目录。

目标：
  --global            (默认) 分发到 ~/.claude/skills 等
  --project           分发到当前 forge 项目 .claude/skills（覆盖 --global）
  --target claude|cursor|codex|copilot|all  选择工具（默认 claude）
    claude   → ~/.claude/skills
    cursor   → ~/.cursor/skills
    codex    → ~/.codex/skills     (OpenAI Codex CLI，2025-12 起 SKILL.md 原生支持)
    copilot  → ~/.copilot/skills   (GitHub Copilot 个人 skill，跨项目)
    all      → 以上全部

模式：
  --mode link   (默认) 目标 = 指向 canonical 的 junction/symlink（单源，改源自动反映）
  --mode copy         目标 = canonical 独立副本

drift 处理（目标与 canonical 内容分叉时）：
  --drift-policy abort     (默认) 报错中止（CI 友好）
  --drift-policy skip      跳过该 skill
  --drift-policy overwrite 以 canonical 强制覆盖

其他：
  --skill NAME       只装指定 skill（可重复）
  --skip-quality     跳过 install 前 registry+audit 双门控
  --with-adapters    同时部署 4 个 skill-routing adapter 单文件`,
	RunE: runSkillsInstall,
}

func runSkillsInstall(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}

	mode := skillsdist.Mode(skInstMode)
	if mode != skillsdist.ModeLink && mode != skillsdist.ModeCopy {
		return fmt.Errorf("--mode 须为 link|copy，got %q", skInstMode)
	}
	driftPolicy := skillsdist.DriftPolicy(skInstDriftPolicy)
	switch driftPolicy {
	case skillsdist.DriftAbort, skillsdist.DriftSkip, skillsdist.DriftOverwrite:
	default:
		return fmt.Errorf("--drift-policy 须为 abort|skip|overwrite，got %q", skInstDriftPolicy)
	}
	targets, err := parseSkillTargets(skInstTarget)
	if err != nil {
		return err
	}

	// --project 显式覆盖 --global（默认全局）；两者互斥时 project 优先
	global := skInstGlobal && !skInstProject
	projectDir := ""
	if !global {
		root, ferr := findProjectRoot()
		if ferr != nil {
			return fmt.Errorf("--project 需在 forge 项目内运行（未找到 .forge/）；用 --global（默认）分发到全局")
		}
		projectDir = filepath.Join(root, ".claude", "skills")
	}

	opts := skillsdist.InstallOpts{
		Mode:             mode,
		DriftPolicy:      driftPolicy,
		Targets:          targets,
		SkillFilter:      skInstSkill,
		SkipQuality:      skInstSkipQuality,
		Global:           global,
		ProjectSkillsDir: projectDir,
	}

	report, instErr := skillsdist.Install(canonical, opts)

	if skInstJSON {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
	} else {
		printInstallReport(report)
	}

	// best-effort 写 manifest（canonical 全量快照，供 system 健康检查与查询）。
	// 失败不阻断 install（manifest 是辅助缓存），但必须留痕——原实现双重吞错
	//（BuildManifest 的 merr 进 if 条件、SaveManifest 的 err 进 _），manifest 静默不更新
	// 会让 system 健康检查读到陈旧数据而无任何提示。
	if mf, merr := skillsdist.BuildManifest(canonical, opts); merr == nil {
		if serr := skillsdist.SaveManifest(mf); serr != nil {
			fmt.Fprintf(os.Stderr, "warn: 保存 skills manifest 失败（不阻断 install）: %v\n", serr)
		}
	} else {
		fmt.Fprintf(os.Stderr, "warn: 构建 skills manifest 失败（不阻断 install）: %v\n", merr)
	}

	if instErr != nil {
		return instErr
	}

	if skInstWithAdapters {
		home, _ := os.UserHomeDir()
		done, plan, aerr := skillsdist.DeployAdapters(canonical, home)
		if aerr != nil {
			return aerr
		}
		if !skInstJSON {
			fmt.Printf("adapters: 部署 %d/%d\n", done, len(plan))
		}
	}
	return nil
}

// printInstallReport 渲染 install 结果（统计 + blocked/drift-skip/backup 明细）。
func printInstallReport(r *skillsdist.InstallReport) {
	if r == nil {
		return
	}
	fmt.Printf("install mode=%s canonical=%s\n", r.Mode, r.Canonical)
	fmt.Printf("  installed=%d  skipped=%d  drifted=%d  failed=%d  total=%d\n",
		r.Stats.Installed, r.Stats.Skipped, r.Stats.Drifted, r.Stats.Failed, r.Stats.Total)
	if r.Aborted != "" {
		fmt.Printf("  ✗ 中止: %s\n", r.Aborted)
	}

	driftSkipCount := 0
	for _, s := range r.Skills {
		for _, t := range s.Targets {
			switch t.Action {
			case "blocked":
				fmt.Printf("  ✗ %s [%s]: %s\n", s.Name, t.Target, t.Detail)
				for _, iss := range s.Issues {
					fmt.Printf("      - %s\n", iss)
				}
			case "reserved":
				fmt.Printf("  ⊘ %s: %s\n", s.Name, t.Detail)
			}
			// drift skip 明细：用户本地改动被保留（区别于"已同步"的 skip——后者不打印）。
			if t.State == skillsdist.StateDrift && t.Action == "skipped" {
				fmt.Printf("  ⊘ %s [%s]: 检测到本地改动，已保留未覆盖\n", s.Name, t.Target)
				driftSkipCount++
			}
			// overwrite 备份明细：旧版本留底位置，便于回滚。
			// Backup 仅 overwrite 路径填充（Action=linked/copied），限定 Action 防止未来其他路径误填导致重复打印。
			if t.Backup != "" && (t.Action == "linked" || t.Action == "copied") {
				fmt.Printf("  ↺ %s [%s]: 旧版本已备份 → %s\n", s.Name, t.Target, t.Backup)
			}
		}
	}

	// drift skip 提醒：告诉用户如何同步（若想放弃本地定制）。
	if driftSkipCount > 0 {
		fmt.Printf("  → 以上 %d 个 skill 保留了你的本地改动。如需同步 canonical 最新版（会先备份再覆盖）：\n", driftSkipCount)
		fmt.Printf("    forge skills install --drift-policy overwrite\n")
	}
}

func parseSkillTargets(raw []string) ([]skillsdist.Target, error) {
	if len(raw) == 0 {
		return []skillsdist.Target{skillsdist.TargetClaude}, nil
	}
	var out []skillsdist.Target
	for _, t := range raw {
		switch t {
		case "claude", "cursor", "codex", "copilot", "all":
			out = append(out, skillsdist.Target(t))
		default:
			return nil, fmt.Errorf("--target 须为 claude|cursor|codex|copilot|all，got %q", t)
		}
	}
	return out, nil
}

func init() {
	skillsInstallCmd.Flags().BoolVar(&skInstGlobal, "global", true, "分发到全局目录（~/.claude/skills 等）")
	skillsInstallCmd.Flags().BoolVar(&skInstProject, "project", false, "分发到当前 forge 项目 .claude/skills（覆盖 --global）")
	skillsInstallCmd.Flags().StringSliceVar(&skInstTarget, "target", []string{"claude"}, "目标工具 claude|cursor|codex|copilot|all")
	skillsInstallCmd.Flags().StringSliceVar(&skInstSkill, "skill", nil, "只装指定 skill（可重复）")
	skillsInstallCmd.Flags().StringVar(&skInstMode, "mode", "link", "分发模式 link|copy")
	skillsInstallCmd.Flags().StringVar(&skInstDriftPolicy, "drift-policy", "abort", "drift 处理 abort|skip|overwrite")
	skillsInstallCmd.Flags().BoolVar(&skInstSkipQuality, "skip-quality", false, "跳过 install 前质量门控")
	skillsInstallCmd.Flags().BoolVar(&skInstWithAdapters, "with-adapters", false, "同时部署 skill-routing adapter 单文件")
	skillsInstallCmd.Flags().BoolVar(&skInstJSON, "json", false, "JSON 输出")
	skillsCmd.AddCommand(skillsInstallCmd)
}
