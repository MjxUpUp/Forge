package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("agents", "auto", "AI 编码工具: auto（自动检测）, 或逗号分隔如 claude-code,cursor,kimi")
	initCmd.Flags().Bool("project", false, "团队模式：资产写入项目目录（.forge/protocol.yml 等可 git 共享），默认零项目写入")
	// Deprecated no-op flags. After project-level pipeline removal mode/fresh are meaningless, kept as hidden no-op
	// for backward compatibility: old scripts/tests running `forge init --mode medium` don't error, just have no effect.
	//
	// Deprecated no-op flags. 项目级管道删除后 mode/fresh 已无意义，保留为隐藏 no-op
	// 维持向后兼容：旧脚本/测试的 `forge init --mode medium` 不报错，只是无效。
	initCmd.Flags().String("mode", "", "(deprecated, no-op) 旧版项目规模标志，项目级管道删除后无意义")
	initCmd.Flags().Bool("fresh", false, "(deprecated, no-op) 旧版强制重新生成标志")
	_ = initCmd.Flags().MarkHidden("mode")
	_ = initCmd.Flags().MarkHidden("fresh")
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化 Forge 项目（默认零项目写入，全部资产在用户级）",
	Long: `forge init 把当前项目登记到 Forge 全局注册表，并把全部资产写到用户级：
hooks 注册进各 agent 的用户级配置（~/.claude/settings.json、~/.codex/hooks.json 等），
质量协议写入用户级 CLAUDE.md / AGENTS.md（备份+追加，可回滚），
protocol.yml 与 runtime state 在 ~/.forge/projects/<key>/。
项目目录默认零写入——不会被 git add 误提交。

--project 团队模式：资产写入项目目录（.forge/protocol.yml、.claude/ 等），
供团队经 git 共享同一份质量协议。`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	teamMode, _ := cmd.Flags().GetBool("project")
	agentsFlag, _ := cmd.Flags().GetString("agents")
	agents := agentbridge.ParseAgentFlag(dir, agentsFlag)
	proto := protocol.DefaultProtocol()

	if teamMode {
		return runInitTeamMode(dir, agents, proto)
	}
	return runInitUserLevel(dir, agents, proto)
}

// runInitUserLevel is the DEFAULT init path: zero writes into the project directory.
// Everything lands at user level — the project is registered in the global registry
// (the "is this a forge project" anchor), config/runtime state goes to the per-project
// DataDir, hooks go into each agent's user-level config, instructions are appended to
// user-level CLAUDE.md/AGENTS.md (backup-then-append, rollback via forge uninstall).
//
// runInitUserLevel 是默认 init 路径：项目目录零写入。全部资产在用户级——项目登记进
// 全局注册表（"是不是 forge 项目"的锚点），配置/runtime state 进 per-project
// DataDir，hooks 进各 agent 的用户级配置，指令以备份+追加方式进用户级
// CLAUDE.md/AGENTS.md（forge uninstall 可回滚）。
func runInitUserLevel(dir string, agents []agentbridge.AgentType, proto *protocol.Protocol) error {
	// 1. Register in the global project registry (~/.forge/projects.json) — THE anchor
	//    of project membership after the refactor (replaces the .forge/ marker).
	//
	// 1. 登记到全局项目注册表（~/.forge/projects.json）——重构后项目成员资格的锚点
	//    （取代 .forge/ 标记）。
	if err := registry.Add(dir); err != nil {
		return fmt.Errorf("failed to register project: %w", err)
	}

	// 2. Ensure the per-project DataDir (~/.forge/projects/<key>/).
	//
	// 2. 确保 per-project DataDir（~/.forge/projects/<key>/）。
	proj, err := forgedata.ProjectFor(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve project: %w", err)
	}
	if err := proj.Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create DataDir: %v\n", err)
	}

	// 3. protocol.yml → DataDir copy, created only when missing (a project-level
	//    override, when present, always wins and is left alone; re-init must not
	//    clobber user edits).
	//
	// 3. protocol.yml → DataDir 副本，仅缺失时创建（项目级覆盖存在时恒优先且不动；
	//    重复 init 不得覆盖用户改动）。
	if _, err := protocol.Load(dir); err != nil {
		if err := protocol.Save(dir, proto); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write protocol.yml: %v\n", err)
		}
	}

	// 4. Reference copies of the hook scripts → DataDir/hooks/ (runtime executes the
	//    embedded content, never the disk copies).
	//
	// 4. hook 脚本的参考副本 → DataDir/hooks/（运行时执行嵌入内容，从不读磁盘副本）。
	if err := hooks.WriteHookTemplates(forgedata.DataDirFor(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write hook copies: %v\n", err)
	}

	// 5. Claude Code: user-level settings.json hooks — skipped when the plugin is
	//    user-level installed (plugin.json already registers ForgeHookSpec machine-wide).
	//
	// 5. Claude Code：用户级 settings.json hooks——plugin 已 user-level 安装时跳过
	//    （plugin.json 已全机器注册 ForgeHookSpec）。
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateUserSettings(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate user-level claude settings: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "forge plugin 已 user-level 接管 hooks,跳过 user-level settings.json 生成\n")
	}

	// 6. User-level instructions (backup+append) + user-level quality skill.
	//
	// 6. 用户级指令（备份+追加）+ 用户级 quality skill。
	if err := skillgen.GenerateUserClaudeMD(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update user-level CLAUDE.md: %v\n", err)
	}
	if err := skillgen.GenerateUserAgentsMD(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update user-level AGENTS.md: %v\n", err)
	}
	if err := skillgen.GenerateUserQualitySkill(proto); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate user-level quality skill: %v\n", err)
	}

	// 7. Other agents → user-level bridge files (codex/cursor/opencode/windsurf/kimi).
	//
	// 7. 其他 agent → 用户级 bridge 文件（codex/cursor/opencode/windsurf/kimi）。
	if len(agents) > 0 {
		bridgeInput := &agentbridge.TranslationInput{
			Protocol:  proto,
			HookNames: hooks.HookNames(),
		}
		if errs := agentbridge.TranslateForAgents(dir, agents, bridgeInput); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "Warning: agent translation failed: %v\n", e)
			}
		}
	}

	// 8. Strip legacy project-level forge writes (converges projects init'd by older
	//    versions on re-init; no-op for fresh projects).
	//
	// 8. 剥除遗留的项目级 forge 写入（老版本 init 的项目在重复 init 时收敛；
	//    新项目 no-op）。
	stripProjectLevelForgeAssets(dir)

	// 9. Sync stamp → DataDir (drives autoSync no-op detection).
	//
	// 9. sync 戳 → DataDir（驱动 autoSync no-op 检测）。
	stampPath := filepath.Join(forgedata.DataDirFor(dir), ".sync-version")
	if err := os.WriteFile(stampPath, []byte(rootCmd.Version), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write sync stamp: %v\n", err)
	}

	fmt.Printf("Forge project initialized（项目目录零写入）\n")
	fmt.Println()
	fmt.Println("User-level assets:")
	fmt.Printf("  ~/.forge/projects.json           — 项目注册表（forge 项目锚点）\n")
	fmt.Printf("  %s  — protocol.yml + runtime state\n", forgedata.DataDirFor(dir))
	if hooks.IsClaudePluginInstalled() {
		fmt.Println(`  ~/.claude/plugins/forge/         — forge plugin 已接管 hooks`)
	} else {
		fmt.Printf("  ~/.claude/settings.json          — Claude Code hooks（user-level）\n")
	}
	fmt.Printf("  ~/.claude/CLAUDE.md              — 质量协议（条件激活，备份+追加）\n")
	fmt.Printf("  ~/.codex/AGENTS.md               — 跨 agent 质量协议（条件激活）\n")
	fmt.Printf("  ~/.claude/skills/forge-quality/  — 质量协议 Skill\n")
	for _, a := range agents {
		switch a {
		case agentbridge.AgentCodex:
			fmt.Printf("  ~/.codex/hooks.json              — Codex hooks（user-level）\n")
		case agentbridge.AgentCursor:
			fmt.Printf("  ~/.cursor/hooks.json             — Cursor hooks（user-level）\n")
		case agentbridge.AgentOpencode:
			fmt.Printf("  ~/.config/opencode/plugins/      — OpenCode plugin（user-level）\n")
		case agentbridge.AgentKimi:
			fmt.Printf("  ~/.kimi-code/config.toml         — Kimi Code hooks（user-level）\n")
		}
	}
	fmt.Println()
	fmt.Println("Next step: open your AI coding tool in this project and describe what you want to build.")
	fmt.Println("Manual commands:")
	fmt.Println("  forge task start  — 开始任务（自动检测分支）")
	fmt.Println("  forge status      — 查看项目状态")
	fmt.Println("  forge init --project — 团队模式（资产写项目目录，可 git 共享）")
	return nil
}

// runInitTeamMode is the legacy project-level path (`forge init --project`): assets are
// written into the project directory so a team can git-share one quality protocol.
// The .forge/team-mode marker exempts the project from the autoSync stripper.
//
// runInitTeamMode 是遗留的项目级路径（`forge init --project`）：资产写入项目目录，
// 供团队经 git 共享同一份质量协议。.forge/team-mode 标记让项目豁免 autoSync 清理。
func runInitTeamMode(dir string, agents []agentbridge.AgentType, proto *protocol.Protocol) error {
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(filepath.Join(forgeDir, "hooks"), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", forgeDir, err)
	}
	if err := hooks.WriteHookTemplates(forgeDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to copy hooks: %v\n", err)
	}
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateSettings(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate .claude/settings.local.json: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "forge plugin 已 user-level 接管 hooks,跳过 project-level settings.local.json 生成\n")
	}
	if err := protocol.SaveProjectLevel(dir, proto); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write protocol.yml: %v\n", err)
	}
	if err := skillgen.GenerateQualitySkill(dir, proto); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate quality skill: %v\n", err)
	}
	if err := skillgen.GenerateClaudeMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate CLAUDE.md: %v\n", err)
	}
	if err := skillgen.GenerateAgentsMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate AGENTS.md: %v\n", err)
	}
	// Team mode wires claude project-level (above); the ClaudeCodeTranslator is
	// user-level-only after the refactor, so it is excluded from the bridge pass.
	//
	// 团队模式已在上方项目级接线 claude；ClaudeCodeTranslator 重构后只在用户级
	// 接线，故 bridge pass 排除它。
	var bridgeAgents []agentbridge.AgentType
	for _, a := range agents {
		if a != agentbridge.AgentClaudeCode {
			bridgeAgents = append(bridgeAgents, a)
		}
	}
	if len(bridgeAgents) > 0 {
		bridgeInput := &agentbridge.TranslationInput{
			Protocol:  proto,
			HookNames: hooks.HookNames(),
		}
		if errs := agentbridge.TranslateForAgents(dir, bridgeAgents, bridgeInput); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "Warning: agent translation failed: %v\n", e)
			}
		}
	}

	// Team-mode marker: exempts this project from stripProjectLevelForgeAssets.
	//
	// 团队模式标记：让本项目豁免 stripProjectLevelForgeAssets。
	if err := os.WriteFile(filepath.Join(forgeDir, teamModeMarker), []byte(rootCmd.Version+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write team-mode marker: %v\n", err)
	}

	dedupeProjectLevelIfPlugin(dir)

	if err := registry.Add(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to register project globally: %v\n", err)
	}
	stampPath := filepath.Join(dir, ".forge", ".sync-version")
	if err := os.WriteFile(stampPath, []byte(rootCmd.Version), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write sync stamp: %v\n", err)
	}

	fmt.Printf("Forge project initialized（团队模式：资产写入项目目录）\n")
	fmt.Println()
	fmt.Println("Created:")
	fmt.Printf("  .forge/protocol.yml              — 质量协议（团队共享覆盖层）\n")
	fmt.Printf("  .forge/team-mode                 — 团队模式标记（豁免自动清理）\n")
	fmt.Printf("  .claude/CLAUDE.md                — 质量协议引用\n")
	fmt.Printf("  AGENTS.md                        — 跨 agent 质量协议\n")
	fmt.Printf("  .claude/skills/forge-quality/    — 质量协议 Skill\n")
	return nil
}
