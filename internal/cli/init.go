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

装了 forge plugin 的用户通常无需手动跑本命令：init-suggest SessionStart hook
检测到 user-level plugin 已装（= 显式 opt-in）时，会在 git 项目首次打开会话
静默自动 init（declined 标记仍可每项目退出，见 forge suggest decline）。
手动 init 的定位：修复/补登记（自动接管失败、非 plugin 的 npm 用户）、
非 git 环境显式接入、以及下面的团队模式。

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
	// 0. A plain `forge init` on a team-mode project is an explicit switch back to
	//    the default (zero-project-write) mode: drop the team-mode marker so the
	//    stripper in step 8 can converge the project-level assets. Without this,
	//    the marker permanently exempted the project and there was no way back.
	//
	// 0. 在团队模式项目上跑普通 `forge init` = 明确切回默认（零项目写入）模式：
	//    删除 team-mode 标记，让步骤 8 的 stripper 能收敛项目级资产。否则标记
	//    永久豁免该项目，无法回归默认模式。
	if marker := filepath.Join(dir, ".forge", teamModeMarker); fileExists(marker) {
		if err := os.Remove(marker); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove team-mode marker: %v\n", err)
		}
	}

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
	//    override, when present, always wins and is left alone; a corrupt file is
	//    backed aside before rewriting defaults — never silently clobbered).
	//    The effective protocol is then loaded for downstream skill generation.
	//
	// 3. protocol.yml → DataDir 副本，仅缺失时创建（项目级覆盖存在时恒优先且不动；
	//    损坏文件先备份到一边再写默认——绝不静默覆盖）。随后加载生效 protocol
	//    供下游 skill 生成。
	if err := protocol.EnsureDefault(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to ensure protocol.yml: %v\n", err)
	}
	if loaded, err := protocol.Load(dir); err == nil {
		proto = loaded
	}

	// 4. Reference copies of the hook scripts → DataDir/hooks/ (runtime executes the
	//    embedded content, never the disk copies). Deploy stamp first — file-sentinel
	//    exempts .forge/hooks/ drift only within its grace window (see autoSync).
	//
	// 4. hook 脚本的参考副本 → DataDir/hooks/（运行时执行嵌入内容，从不读磁盘副本）。
	//    deploy stamp 先落盘——file-sentinel 只在 grace 窗口内豁免 .forge/hooks/ drift
	//    （见 autoSync）。stamp 失败仅告警。
	if err := hooks.WriteHookDeployStamp(forgedata.DataDirFor(dir), projectTagFor(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write hook deploy stamp: %v\n", err)
	}
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
		if line, ok := agentSummaryLine(a); ok {
			fmt.Print(line)
		}
	}
	fmt.Println()
	fmt.Println("Next step: open your AI coding tool in this project and describe what you want to build.")
	fmt.Println("Manual commands:")
	fmt.Println("  forge task start  — 开始任务（自动检测分支）")
	fmt.Println("  forge status      — 查看项目状态")
	fmt.Println("  forge init --project — 团队模式（资产写项目目录，可 git 共享）")
	// onboarding 触发点（multi-task-concurrency §13）：init 是引导 harness repo 的
	// 天然时机——cooldown 防重复，agent 不得代批的 HITL 在 harness init 本体。
	MaybeOfferHarness("forge init")
	return nil
}

// agentSummaryLine renders the per-agent wiring line of the init summary. ok=false
// for agents with nothing machine-wide to report (claude-code's wiring is printed
// unconditionally above; copilot/cline/codebuddy/reasonix/windsurf have their own
// summary channels). The dsh line is install GUIDANCE, not a written file — the
// DshTranslator is a deliberate no-op (no user-level config file exists to merge
// into), so the summary is the only place `forge init` tells the user how to wire
// DeepSeek Harness.
//
// agentSummaryLine 渲染 init 摘要的 per-agent 接线行。无需报告全机器接线的 agent
// 返回 ok=false（claude-code 的接线在上面无条件打印；copilot/cline/codebuddy/
// reasonix/windsurf 各有摘要通道）。dsh 行是安装**指引**而非已写文件——
// DshTranslator 是刻意的 no-op（没有可合并的用户级配置文件），故摘要是
// `forge init` 告知用户如何接线 DeepSeek Harness 的唯一位置。
func agentSummaryLine(a agentbridge.AgentType) (line string, ok bool) {
	switch a {
	case agentbridge.AgentCodex:
		return "  ~/.codex/hooks.json              — Codex hooks（user-level）\n", true
	case agentbridge.AgentCursor:
		return "  ~/.cursor/hooks.json             — Cursor hooks（user-level）\n", true
	case agentbridge.AgentOpencode:
		return "  ~/.config/opencode/plugins/      — OpenCode plugin（user-level）\n", true
	case agentbridge.AgentKimi:
		return "  ~/.kimi-code/config.toml         — Kimi Code hooks（user-level）\n", true
	case agentbridge.AgentDsh:
		return "  DeepSeek Harness                 — plugin 接线：dsh plugin --profile web add \"github:MjxUpUp/Forge#main&path:/plugins/forge-dsh\"（见 plugins/forge-dsh/README.md）\n", true
	case agentbridge.AgentZcode:
		return "  ~/.zcode/cli/config.json         — ZCode hooks（user-level）\n", true
	}
	return "", false
}

// runInitTeamMode is the legacy project-level path (`forge init --project`): assets
// are written into the project directory so a team can git-share one quality
// protocol. The .forge/team-mode marker exempts the project from the autoSync
// stripper.
//
// Hooks are NOT written to a project-level .claude/settings.local.json: hook
// commands invoke the forge binary, so every teammate must install forge anyway —
// and installing + running any init once already registered the USER-LEVEL hooks
// machine-wide. A project-level copy would only double-run every hook (autoSync's
// user-level settings.json registration is unconditional). The real value of team
// mode is the git-shared protocol.yml / CLAUDE.md / AGENTS.md instruction layer.
//
// runInitTeamMode 是遗留的项目级路径（`forge init --project`）：资产写入项目目录，
// 供团队经 git 共享同一份质量协议。.forge/team-mode 标记让项目豁免 autoSync 清理。
//
// hooks 不写项目级 .claude/settings.local.json：hook 命令调用的是 forge 二进制，
// 队友反正必须装 forge——装好并跑过任意一次 init 即已在全机器注册**用户级**
// hooks。项目级副本只会让每条 hook 双跑（autoSync 的用户级 settings.json 注册
// 是无条件的）。团队模式的真正价值是 git 共享的 protocol.yml / CLAUDE.md /
// AGENTS.md 指令层。
func runInitTeamMode(dir string, agents []agentbridge.AgentType, proto *protocol.Protocol) error {
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(filepath.Join(forgeDir, "hooks"), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", forgeDir, err)
	}
	// Deploy stamp first: this rewrites PROJECT-level .forge/hooks/*.sh — exactly the
	// drift file-sentinel's manifest watches; the grace marker keeps a concurrent
	// sentinel run from quarantining Forge's own write (2026-08-02 incident).
	//
	// deploy stamp 先落盘：此处重写的是项目级 .forge/hooks/*.sh——正是 file-sentinel
	// manifest 盯防的 drift；grace marker 防止并发 sentinel 把 Forge 自身写入
	// quarantine（2026-08-02 事故）。stamp 失败仅告警。
	if err := hooks.WriteHookDeployStamp(forgedata.DataDirFor(dir), projectTagFor(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write hook deploy stamp: %v\n", err)
	}
	if err := hooks.WriteHookTemplates(forgeDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to copy hooks: %v\n", err)
	}
	// Hooks go to the USER-LEVEL settings.json (same as the default init path) —
	// never to a project-level settings.local.json, which would double-run every
	// hook against autoSync's user-level registration.
	//
	// hooks 写用户级 settings.json（与默认 init 路径相同）——绝不写项目级
	// settings.local.json，那会与 autoSync 的用户级注册双跑每条 hook。
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateUserSettings(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate user-level claude settings: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "forge plugin 已 user-level 接管 hooks,跳过 user-level settings.json 生成\n")
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
	// Team mode wires claude at USER level (above); the ClaudeCodeTranslator is
	// user-level-only after the refactor, so it is excluded from the bridge pass
	// (it would redo the same user-level writes).
	//
	// 团队模式已在上方用户级接线 claude；ClaudeCodeTranslator 重构后只在用户级
	// 接线，故 bridge pass 排除它（否则会重复同样的用户级写入）。
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

	fmt.Printf("Forge project initialized（团队模式：指令资产写入项目目录，hooks 在用户级）\n")
	fmt.Println()
	fmt.Println("Created:")
	fmt.Printf("  .forge/protocol.yml              — 质量协议（团队共享覆盖层）\n")
	fmt.Printf("  .forge/team-mode                 — 团队模式标记（豁免自动清理）\n")
	fmt.Printf("  .claude/CLAUDE.md                — 质量协议引用\n")
	fmt.Printf("  AGENTS.md                        — 跨 agent 质量协议\n")
	fmt.Printf("  .claude/skills/forge-quality/    — 质量协议 Skill\n")
	fmt.Println()
	fmt.Println("Hooks 注册在用户级（~/.claude/settings.json）——hook 命令调用 forge 二进制，")
	fmt.Println("队友各自安装 forge 并跑过一次 init 即具备同样的 hooks；项目级不再写")
	fmt.Println("settings.local.json（避免与用户级注册双跑）。")
	return nil
}
