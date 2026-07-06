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
	f := pluginPackCmd.Flags()
	f.String("out", "", "输出目录（默认当前目录，即仓库根）")
	f.String("repo-slug", "MjxUpUp/Forge", "github owner/repo（README 安装命令用）")
	f.String("marketplace-name", "forge", "marketplace 标识")
	f.String("plugin-name", "forge", "plugin 标识")
	f.String("description", agentbridge.DefaultPluginDescription, "plugin 描述")
	f.String("owner-name", "MjxUpUp", "owner 名（marketplace owner / plugin author，schema required）")
	f.String("owner-email", "", "owner 邮箱")
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
	spec := agentbridge.PluginPackSpec{
		RepoDir:         out,
		RepoSlug:        flagString(cmd, "repo-slug"),
		MarketplaceName: flagString(cmd, "marketplace-name"),
		PluginName:      flagString(cmd, "plugin-name"),
		Description:     flagString(cmd, "description"),
		OwnerName:       flagString(cmd, "owner-name"),
		OwnerEmail:      flagString(cmd, "owner-email"),
	}
	if err := agentbridge.GeneratePluginPack(spec); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "plugin pack generated at %s\n", out)
	return nil
}

// flagString 取 string flag 值（错误忽略——cobra 保证已注册）。plugin.go 局部 helper。
func flagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

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

// pluginDedupeCmd 在 plugin 已装时一次性清理 project-level 重复的 hooks（settings.local.json）
// 与 forge MCP server（.mcp.json）。init-suggest SessionStart hook 自动调用（存量迁移），
// 也可手动跑。幂等：无重复时 no-op（无输出，hook 据此不产生提示噪音）。
var pluginDedupeCmd = &cobra.Command{
	Use:   "dedupe [dir]",
	Short: "plugin 已装时清理项目级重复 hooks + MCP",
	Long: `当 forge plugin 在 user-level 已装，project-level 的 .claude/settings.local.json hooks
与 .mcp.json forge server 是冗余的（Claude Code 双重加载同名 forge server / 双跑 hook）。
本命令一次性移除这两类 forge 来源的重复注册，保留用户自定义条目。

仅在 plugin 已装时清理（未装时不动——project-level 是唯一来源）。幂等：无重复时 no-op。
[dir] 默认当前目录。`,
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
	hooksChanged, err := hooks.StripForgeHooks(abs)
	if err != nil {
		return fmt.Errorf("strip hooks: %w", err)
	}
	mcpChanged, err := agentbridge.StripForgeMCPServer(abs)
	if err != nil {
		return fmt.Errorf("strip mcp: %w", err)
	}
	if !hooksChanged && !mcpChanged {
		// 无重复 → no-op，不输出（init-suggest hook 据空输出不产生提示噪音）。
		return nil
	}
	var parts []string
	if hooksChanged {
		parts = append(parts, "hooks")
	}
	if mcpChanged {
		parts = append(parts, "MCP")
	}
	fmt.Fprintln(out, `plugin 已 user-level 接管，移除项目级重复 `+strings.Join(parts, "+")+`（`+abs+`）`)
	return nil
}
