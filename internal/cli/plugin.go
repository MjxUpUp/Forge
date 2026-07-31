package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginPackCmd)
	pluginCmd.AddCommand(pluginStatusCmd)
	pluginCmd.AddCommand(pluginDedupeCmd)
	pluginDedupeCmd.Flags().Bool("keep-empty", false, "保留 settings.local.json 文件壳（清 forge hooks 后写 {} 而非删）——自动调用（init-suggest SessionStart）传 true,手动 dedupe 不传,删空文件")
	pluginPackCmd.Flags().String("out", "", "输出目录（默认当前目录，即仓库根）")
}

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "plugin marketplace 生成与管理",
}

var pluginPackCmd = &cobra.Command{
	Use:   "pack [--out dir]",
	Short: "生成多 host plugin pack（claude/codex/cursor/copilot）",
	Long: `生成一个 plugin pack，让各 AI 编码 agent 通过自己的 plugin marketplace 一键安装 forge。

写入（默认当前目录）：
  .claude-plugin/marketplace.json   claude+codex+copilot 共享读
  .cursor-plugin/marketplace.json   cursor
  plugins/<name>/
    .claude-plugin/plugin.json      claude manifest（hooks = ForgeHookSpec，与 forge init 字节一致）
    .mcp.json                       共享 MCP（claude/codex 自动发现）
    README.md                       每 host 安装命令

采用多 host 插件市场的通用模式：薄 manifest + 共享内容，单仓即 marketplace。
source 用 plugins/<name> 子目录（forge 是 Go 工具仓，须隔离源码树）。

示例：
  forge plugin pack                 在当前仓库根生成 forge 自身的 marketplace
  forge plugin pack --out ../proj   在指定目录生成`,
	RunE: runPluginPack,
}

func runPluginPack(cmd *cobra.Command, args []string) error {
	out, _ := cmd.Flags().GetString("out")
	if out == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("plugin pack: get cwd: %w", err)
		}
		out = cwd
	}
	spec := agentbridge.DefaultPluginPack(out)
	if err := agentbridge.GeneratePluginPack(spec); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "plugin pack generated at %s\n", out)
	return nil
}

// pluginStatusCmd reports whether the forge plugin is installed at user level. Used by
// scripts/hooks (the dedupe branch of init-suggest) for detection: exit 0 = installed
// (plugin has taken over hooks+MCP at user level), non-zero = not installed.
//
// pluginStatusCmd 报告 forge plugin 是否在 user-level 已装。供脚本/hook（init-suggest
// 的 dedupe 分支）检测：exit 0 = 已装（plugin 已 user-level 接管 hooks+MCP），非零 = 未装。
var pluginStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "报告 forge plugin 是否在 user-level 已装（exit 0=已装，非零=未装）",
	Long: `检测 Claude Code 是否在 user-level 安装了 forge plugin。
读 <claude home>/plugins/installed_plugins.json，找 forge@<marketplace> 的 scope=user 条目
（claude home 优先 CLAUDE_CONFIG_DIR，fallback ~/.claude）。

供脚本/hook 检测：exit 0 = 已装（plugin 已接管 hooks+MCP，project-level 重复可清理），
非零退出 = 未装。SilenceErrors+SilenceUsage 压住 cobra 自身的 Error:/Usage 块（未装时
RunE 仍 return error，cli.Execute root.go:66 会向 stderr 再打一行——裸跑仍 stdout+stderr
两行；init-suggest.sh 用 >/dev/null 2>&1 只看 exit code 不受影响）。`,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		if hooks.IsClaudePluginInstalled() {
			fmt.Fprintln(out, "forge plugin: installed at user level")
			return nil
		}
		fmt.Fprintln(out, "forge plugin: not installed at user level")
		return fmt.Errorf("forge plugin not installed at user level")
	},
}

// pluginDedupeCmd one-shot cleans project-level duplicate hooks (settings.local.json) and the
// forge MCP server (.mcp.json) when the plugin is already installed. The init-suggest SessionStart
// hook calls it automatically (with --keep-empty for in-place migration that keeps the file shell);
// it can also be run manually (default deletes empty files). Idempotent: no-op when nothing is
// duplicate (no output; the hook uses this to avoid prompt noise).
//
// pluginDedupeCmd 在 plugin 已装时一次性清理 project-level 重复的 hooks（settings.local.json）
// 与 forge MCP server（.mcp.json）。init-suggest SessionStart hook 自动调用（传 --keep-empty,
// 存量迁移且保留文件壳）,也可手动跑（默认删空文件）。幂等：无重复时 no-op（无输出，hook 据此不产生提示噪音）。
var pluginDedupeCmd = &cobra.Command{
	Use:   "dedupe [dir]",
	Short: "plugin 已装时清理项目级 + user 级重复 hooks + MCP",
	Long: `当 forge plugin 在 user-level 已装，project-level 的 .claude/settings.local.json hooks
与 .mcp.json forge server 是冗余的（Claude Code 双重加载同名 forge server / 双跑 hook）。
本命令一次性移除这两类 forge 来源的重复注册，保留用户自定义条目。

同时清理 user-level（~/.claude 或 $CLAUDE_CONFIG_DIR）settings.local.json 的 forge hooks：
plugin.json 已在 user-level 注册全部 ForgeHookSpec，此处的 forge hook 必重复（历史 global
forge init 写 home / 旧全局安装残留）。user-level 始终保留文件壳（绝不删用户全局配置），
与 --keep-empty flag 无关。

--keep-empty：清完 forge hooks 后若 [dir]/.claude/settings.local.json 只剩 forge 来源（无
用户字段），默认删整个文件；传 --keep-empty 则写 {} 保留文件壳（settings.local.json 是
gitignored 个人配置,用户常主动放置/编辑,自动调用时不删）。仅影响 project-level
settings.local.json,.mcp.json 清完仍删空。user-level 不受此 flag 影响（始终保留壳）。

仅在 plugin 已装时清理（未装时不动——project-level 是唯一来源）。幂等：无重复时 no-op。
[dir] 默认当前目录。

执行时机（两路径,review S4）：在 forge 项目内,autoSync 的 defer（每条非 init forge 命令末尾,
sync.go:33）会先静默完成 project+user 级 dedupe（dedupeProjectLevelIfPlugin）,本命令 RunE 此时
多为 no-op 无输出。本命令的独立价值在：(a) 在非 forge 项目（如 home）手动跑——autoSync 不触发
（findProjectRoot 失败,root.go:37）,RunE 是唯一清理者并给出可读输出（'cd ~ && forge plugin dedupe'
是清 user 级全局重复的常用入口）；(b) --keep-empty 显式控制项目级是否删空文件。`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPluginDedupe,
}

func runPluginDedupe(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	if !hooks.IsClaudePluginInstalled() {
		fmt.Fprintln(out, `plugin 未装，无需 dedupe（project-level 是唯一来源）`)
		return nil
	}
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	// --keep-empty: automatic invocation (init-suggest SessionStart) passes true — keep the
	// settings.local.json file shell (users often place/edit it actively, never silently delete);
	// manual dedupe defaults to false, deleting empty files (explicit cleanup semantics).
	//
	// --keep-empty: 自动调用（init-suggest SessionStart）传 true——保留 settings.local.json
	// 文件壳（用户常主动放置/编辑,绝不静默删）;手动 dedupe 默认 false,删空文件（显式清理语义）。
	keepEmpty, _ := cmd.Flags().GetBool("keep-empty")
	hooksChanged, err := hooks.StripForgeHooks(abs, keepEmpty)
	if err != nil {
		return fmt.Errorf("strip hooks: %w", err)
	}
	mcpChanged, err := agentbridge.StripForgeMCPServer(abs)
	if err != nil {
		return fmt.Errorf("strip mcp: %w", err)
	}
	// user-level: plugin.json registers all ForgeHookSpec at user level → forge hooks in
	// ~/.claude/settings.local.json are guaranteed duplicates (legacy global forge init writing
	// home / stale global install residue). Claude Code runs them twice. keepEmpty is fixed to
	// true (inside StripForgeHooksUserLevel) — user global config is never deleted, only forge
	// hooks are cleared keeping the shell, independent of the project-level keepEmpty flag.
	//
	// return err (consistent with the two project-level sites, unlike dedupeProjectLevelIfPlugin's
	// warn): this command is run explicitly by the user for cleanup, so failures should propagate
	// rather than be swallowed; the autoSync defer path does the opposite, degrading to warn so it
	// does not block the main command.
	//
	// user-level: plugin.json 在 user-level 注册全部 ForgeHookSpec → ~/.claude/settings.local.json
	// 的 forge hook 必重复（历史 global forge init 写 home / 旧全局安装残留）。Claude Code 双跑。
	// keepEmpty 固定 true（StripForgeHooksUserLevel 内部）——用户全局配置绝不删,只清 forge hooks
	// 保留壳,与 project-level keepEmpty flag 无关。
	//
	// return err（与 project 级两处一致,区别于 dedupeProjectLevelIfPlugin 的 warn）：本命令是用户
	// 专门为清理而显式跑的,失败应上报而非吞掉;autoSync defer 路径相反,降级 warn 不阻断主命令。
	userChanged, err := hooks.StripForgeHooksUserLevel()
	if err != nil {
		return fmt.Errorf("strip user-level hooks: %w", err)
	}
	if !hooksChanged && !mcpChanged && !userChanged {
		// No duplicates → no-op, no output (init-suggest hook uses empty output to avoid prompt noise).
		//
		// 无重复 → no-op，不输出（init-suggest hook 据空输出不产生提示噪音）。
		return nil
	}
	if hooksChanged || mcpChanged {
		var parts []string
		if hooksChanged {
			parts = append(parts, "hooks")
		}
		if mcpChanged {
			parts = append(parts, "MCP")
		}
		fmt.Fprintln(out, `plugin 已 user-level 接管，移除项目级重复 `+strings.Join(parts, "+")+`（`+abs+`）`)
	}
	if userChanged {
		// user-level gets its own line: different location (global ~/.claude rather than the project
		// directory), a separate line lets the user know the global config was cleaned.
		//
		// user-level 单独提示:位置不同（全局 ~/.claude 而非项目目录）,独立一行让用户知晓全局配置被清。
		fmt.Fprintln(out, `plugin 已 user-level 接管，移除 user-level 重复 hooks（`+hooks.ClaudeHome()+`）`)
	}
	return nil
}
