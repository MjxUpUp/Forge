package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
)

// dedupe.go — plugin 已 user-level 安装时,清理 project-level 重复注册。
//
// 背景:forge plugin(user-level,~/.claude/plugins/cache/...)的 plugin.json 注册了
// ForgeHookSpec（hooks）。这与 forge init/sync 写的 project-level settings.local.json
// 的 hooks（hooks.GenerateSettings 写）完全重复——Claude Code 合并两份注册 → 同一 hook
// 跑两遍（性能 ×2 + advisory 噪音 ×2,幂等所以不出错,但冗余）。
//
// 设计:dedup 是【命令层职责】。Translate / GenerateSettings 保持纯函数（总写,不耦合
// plugin 检测）——避免单元测试依赖全局 IsClaudePluginInstalled 状态。init/sync 在所有
// 写入完成后调本 helper 统一清理。
//
// .mcp.json（StripForgeMCPServer）:MCP 层已于 2026-07-24 全拆,forge 不再生成 forge
// MCP server,plugin 也不再带 .mcp.json。StripForgeMCPServer 保留仅清理历史 init/sync
// 写过 forge server 的旧项目 .mcp.json 残留。
//
// 幂等:无重复时 StripForgeHooks / StripForgeMCPServer 均 no-op（读文件判断 forge 条目
// 不存在即返回 changed=false）,故 autoSync 每命令前 defer 调用开销可忽略。

// dedupeProjectLevelIfPlugin 在 forge plugin 已 user-level 安装时,清理 project-level
// 重复的 hooks（settings.local.json）与 forge MCP server（.mcp.json）,以及 user-level
// settings.local.json（~/.claude 或 $CLAUDE_CONFIG_DIR）的 forge hooks 重复注册。未装时 no-op。
// init/sync 末尾（或 defer）调。
//
// 自动路径（SessionStart / autoSync / init·sync）,故 settings.local.json 用 keepEmpty=true
// 保留文件壳（只清 forge hooks,写 {}）——用户常主动放置/编辑这个文件,绝不在自动清理时静默删。
// .mcp.json 由 StripForgeMCPServer 处理,删空逻辑不变（用户未提保留诉求）。
//
// user-level 由 StripForgeHooksUserLevel 处理:plugin.json 已在 user-level 注册全部
// ForgeHookSpec,user-level settings.local.json 的 forge hook 必重复（历史 global forge
// init 写 home / 旧全局安装残留）。内部固定 keepEmpty=true——用户全局配置绝不删,只清 forge
// hooks 保留 {} 壳（与 project-level 可删不同）。覆盖 init/sync 这条 auto 路径;另一条 auto
// 路径（init-suggest SessionStart → forge plugin dedupe）在 runPluginDedupe 内同样清理。
func dedupeProjectLevelIfPlugin(dir string) {
	if !hooks.IsClaudePluginInstalled() {
		return
	}
	if _, err := hooks.StripForgeHooks(dir, true); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to strip duplicate project hooks: %v\n", err)
	}
	if _, err := agentbridge.StripForgeMCPServer(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to strip duplicate project MCP: %v\n", err)
	}
	// user-level strip（与上面 project 级两处一致）warn-not-return：本函数跑在 autoSync 的 defer
	// 里（每条非 init forge 命令末尾），dedupe 失败绝不能阻断用户实际要跑的命令——降级为 stderr
	// warning 让命令照常完成。显式 `forge plugin dedupe` 路径（runPluginDedupe）相反：return err
	// 把失败上报给用户（plugin.go），因为那是用户专门为清理而跑的命令。
	if _, err := hooks.StripForgeHooksUserLevel(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to strip duplicate user-level hooks: %v\n", err)
	}
}
