package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
)

// dedupe.go — when the plugin is already installed at user-level, clean up duplicate project-level registrations.
// dedupe.go — plugin 已 user-level 安装时,清理 project-level 重复注册。
//
// Background: forge plugin (user-level, ~/.claude/plugins/cache/...) registers ForgeHookSpec (hooks) in its plugin.json.
// The user-level settings.json written by GenerateUserSettings registers the same spec — Claude Code merges both
// registrations so the same hook runs twice (perf x2 + advisory noise x2; idempotent so no errors, but redundant).
// Project-level settings.local.json forge hooks are no longer written (the GenerateSettings path was removed); any
// found on disk are residue from historical installs and are cleaned here too.
// 背景:forge plugin(user-level,~/.claude/plugins/cache/...)的 plugin.json 注册了
// ForgeHookSpec（hooks）。GenerateUserSettings 写的 user-level settings.json 注册了同一份
// spec——Claude Code 合并两份注册 → 同一 hook 跑两遍（性能 ×2 + advisory 噪音 ×2,幂等
// 所以不出错,但冗余）。project-level settings.local.json 的 forge hooks 已不再写
// （GenerateSettings 路径已删除）；盘上发现的均为历史安装残留,也在此一并清理。
//
// Design: dedup is a [command-layer responsibility]. Translate / GenerateUserSettings stay pure functions (always
// write, never couple to plugin detection) — so unit tests do not depend on the global IsClaudePluginInstalled state.
// init/sync calls this helper for unified cleanup after all writes complete.
// 设计:dedup 是【命令层职责】。Translate / GenerateUserSettings 保持纯函数（总写,不耦合
// plugin 检测）——避免单元测试依赖全局 IsClaudePluginInstalled 状态。init/sync 在所有
// 写入完成后调本 helper 统一清理。
//
// .mcp.json (StripForgeMCPServer): the MCP layer was fully torn out on 2026-07-24; forge no longer generates a forge
// MCP server and the plugin no longer ships a .mcp.json. StripForgeMCPServer is kept only to clean legacy project
// .mcp.json residues where past init/sync wrote a forge server entry.
// .mcp.json（StripForgeMCPServer）:MCP 层已于 2026-07-24 全拆,forge 不再生成 forge
// MCP server,plugin 也不再带 .mcp.json。StripForgeMCPServer 保留仅清理历史 init/sync
// 写过 forge server 的旧项目 .mcp.json 残留。
//
// Idempotent: when there is nothing to dedup, StripForgeHooks / StripForgeMCPServer are both no-ops (they read the file
// and return changed=false if no forge entry exists), so the per-command deferred call in autoSync costs nothing.
// 幂等:无重复时 StripForgeHooks / StripForgeMCPServer 均 no-op（读文件判断 forge 条目
// 不存在即返回 changed=false）,故 autoSync 每命令前 defer 调用开销可忽略。

// dedupeProjectLevelIfPlugin cleans duplicate project-level hooks (settings.local.json) and the forge MCP server
// (.mcp.json), as well as forge hook duplicates in user-level settings.local.json (~/.claude or $CLAUDE_CONFIG_DIR),
// when the forge plugin is already installed at user-level. No-op when not installed.
// Called at the end of init/sync (or via defer).
// dedupeProjectLevelIfPlugin 在 forge plugin 已 user-level 安装时,清理 project-level
// 重复的 hooks（settings.local.json）与 forge MCP server（.mcp.json）,以及 user-level
// settings.local.json（~/.claude 或 $CLAUDE_CONFIG_DIR）的 forge hooks 重复注册。未装时 no-op。
// init/sync 末尾（或 defer）调。
//
// Automated path (SessionStart / autoSync / init·sync), so settings.local.json uses keepEmpty=true
// to keep the file shell (only clears forge hooks, writes {}) — users often place/edit this file, so never silently
// delete it during auto cleanup. .mcp.json is handled by StripForgeMCPServer; its empty-delete logic is unchanged
// (no user request to preserve it).
// 自动路径（SessionStart / autoSync / init·sync）,故 settings.local.json 用 keepEmpty=true
// 保留文件壳（只清 forge hooks,写 {}）——用户常主动放置/编辑这个文件,绝不在自动清理时静默删。
// .mcp.json 由 StripForgeMCPServer 处理,删空逻辑不变（用户未提保留诉求）。
//
// User-level is handled by StripForgeHooksUserLevel: plugin.json already registers all ForgeHookSpec at user-level,
// so user-level settings.local.json forge hooks are always duplicates (legacy global forge init writing home /
// old global install residue). keepEmpty=true is fixed internally — never delete user global config, only clear forge
// hooks and keep the {} shell (unlike project-level which can be deleted). Covers the init/sync auto path; the other
// auto path (init-suggest SessionStart -> forge plugin dedupe) does the same cleanup inside runPluginDedupe.
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
