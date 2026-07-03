package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginPackCmd)
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
