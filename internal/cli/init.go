package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("agents", "auto", "AI 编码工具: auto（自动检测）, 或逗号分隔如 claude-code,cursor")
	// Deprecated no-op flags. 项目级管道删除后 mode/fresh 已无意义，保留为隐藏 no-op
	// 维持向后兼容：旧脚本/测试的 `forge init --mode medium` 不报错，只是无效。
	initCmd.Flags().String("mode", "", "(deprecated, no-op) 旧版项目规模标志，项目级管道删除后无意义")
	initCmd.Flags().Bool("fresh", false, "(deprecated, no-op) 旧版强制重新生成标志")
	_ = initCmd.Flags().MarkHidden("mode")
	_ = initCmd.Flags().MarkHidden("fresh")
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化 Forge 项目（创建 .forge/ 目录 + Skill + Hooks 集成）",
	Long: `forge init 为当前项目创建 .forge/ 目录与质量协议集成。
包含 hooks/、protocol.yml、Claude Code Skill 与跨 agent 协议文件。`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Create .forge/ directory (project-level hooks only). Runtime state
	// (tasks/gates/checklog/...) lives in the user-level DataDir
	// (~/.forge/projects/<key>/) — created on demand by stores' MkdirAll.
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(filepath.Join(forgeDir, "hooks"), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", forgeDir, err)
	}

	// Copy hook templates
	if err := hooks.WriteHookTemplates(forgeDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to copy hooks: %v\n", err)
	}

	// Generate .claude/settings.local.json (Claude Code project-level hooks).
	// Only when plugin is NOT user-level installed — when plugin IS installed,
	// user-level plugin.json already registers ForgeHookSpec machine-wide.
	// Writing project-level hooks is redundant and creates a fragile
	// "write then immediately strip" pattern that can corrupt the file.
	// dedupeProjectLevelIfPlugin still runs at the end to clean up legacy hooks.
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateSettings(dir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate .claude/settings.local.json: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "forge plugin 已 user-level 接管 hooks,跳过 project-level settings.local.json 生成\n")
	}

	// Write quality protocol
	proto := protocol.DefaultProtocol()
	if err := protocol.Save(dir, proto); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write protocol.yml: %v\n", err)
	}

	// Generate quality skill
	if err := skillgen.GenerateQualitySkill(dir, proto); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate quality skill: %v\n", err)
	}

	// Generate .claude/CLAUDE.md with quality protocol reference
	if err := skillgen.GenerateClaudeMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate CLAUDE.md: %v\n", err)
	}

	// Generate project-root AGENTS.md — the cross-agent instruction source
	// (codex/cursor/copilot/windsurf/cline read AGENTS.md; CLAUDE.md is
	// claude-only). Same Forge-managed section contract.
	if err := skillgen.GenerateAgentsMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate AGENTS.md: %v\n", err)
	}

	// Translate for other agents (Cursor, Copilot, Windsurf)
	agentsFlag, _ := cmd.Flags().GetString("agents")
	agents := agentbridge.ParseAgentFlag(dir, agentsFlag)
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

	fmt.Printf("Forge project initialized\n")
	fmt.Println()
	fmt.Println("Created:")
	fmt.Printf("  .forge/hooks/                    — 门禁 Hook 脚本\n")
	fmt.Printf("  .forge/protocol.yml              — 质量协议\n")
	if hooks.IsClaudePluginInstalled() {
		fmt.Println(`  .claude/settings.local.json      — forge plugin 已 user-level 接管 hooks,跳过 project-level 生成`)
	} else {
		fmt.Printf("  .claude/settings.local.json      — Claude Code 集成\n")
	}
	fmt.Printf("  .claude/CLAUDE.md                — 质量协议引用\n")
	fmt.Printf("  AGENTS.md                        — 跨 agent 质量协议（codex/cursor/copilot/windsurf/cline）\n")
	fmt.Printf("  .claude/skills/forge-quality/     — 质量协议 Skill\n")

	fmt.Println()
	fmt.Println("Next step: open Claude Code in this project and describe what you want to build.")
	fmt.Println("Claude Code will read the Forge skill and drive task-tracked development.")
	fmt.Println()
	fmt.Println("Manual commands:")
	fmt.Println("  forge task start  — 开始任务（自动检测分支）")
	fmt.Println("  forge status      — 查看项目状态")

	// plugin 已 user-level 装时,清理 project-level 重复 hooks（GenerateSettings 写）+
	// 旧项目 .mcp.json 的 forge server 残留（StripForgeMCPServer,清历史 init/sync 写过
	// 的旧项目）。在所有写入之后统一去重。
	dedupeProjectLevelIfPlugin(dir)

	// 登记到全局项目注册表（~/.forge/projects.json），供 forge dashboard --global 聚合。
	// 失败仅警告——全局视图是增强，init 本身成功不依赖它。
	if err := registry.Add(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to register project globally: %v\n", err)
	}

	// 写 .sync-version stamp，标记 init 已把资产同步到当前二进制版本。autoSync 下次
	// 命令前读 stamp 匹配即 no-op；不写的话下条命令会因 stamp 缺失强制全量重跑（浪费）。
	// autoSync 本身"除 init 外每命令前跑"——init 不经 autoSync，故这里显式落 stamp。
	stampPath := filepath.Join(dir, ".forge", ".sync-version")
	if err := os.WriteFile(stampPath, []byte(rootCmd.Version), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write sync stamp: %v\n", err)
	}
	return nil
}
