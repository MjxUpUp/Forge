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
// ForgeHookSpec（13 hooks）,.mcp.json 注册了 forge MCP server。这与 forge init/sync
// 写的 project-level 资产重复:
//   - .claude/settings.local.json 的 hooks（hooks.GenerateSettings 写）
//   - 项目根 .mcp.json 的 forge server（agentbridge.writeClaudeMCP 写,ClaudeCodeTranslator.Translate 调）
//
// Claude Code 合并两份注册 → 同一 hook/MCP 跑两遍（性能 ×2 + advisory 噪音 ×2,幂等所以
// 不出错,但冗余）。
//
// 设计:dedup 是【命令层职责】。Translate / GenerateSettings / writeClaudeMCP 保持纯函数
// （总写,不耦合 plugin 检测）——避免单元测试依赖全局 IsClaudePluginInstalled 状态
// （TestTranslatorsEmitMCP 期望 Translate 总写 .mcp.json,检测放进 Translate 会让测试
// 随机器是否装 plugin 而变）。init/sync 在所有写入完成后调本 helper 统一清理,无论写入
// 路径（直接 GenerateSettings / 经 TranslateForAgents 的 writeClaudeMCP）都能去重。
//
// 幂等:无重复时 StripForgeHooks / StripForgeMCPServer 均 no-op（读文件判断 forge 条目
// 不存在即返回 changed=false）,故 autoSync 每命令前 defer 调用开销可忽略。

// dedupeProjectLevelIfPlugin 在 forge plugin 已 user-level 安装时,清理 project-level
// 重复的 hooks（settings.local.json）与 forge MCP server（.mcp.json）。未装时 no-op。
// init/sync 末尾（或 defer）调。
//
// 自动路径（SessionStart / autoSync / init·sync）,故 settings.local.json 用 keepEmpty=true
// 保留文件壳（只清 forge hooks,写 {}）——用户常主动放置/编辑这个文件,绝不在自动清理时静默删。
// .mcp.json 由 StripForgeMCPServer 处理,删空逻辑不变（用户未提保留诉求）。
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
}
