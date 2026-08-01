package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// plugin_detect.go — detects whether forge is installed as a Claude Code user-level plugin.
//
// Background: the plugin (user-level, ~/.claude/plugins/cache/forge/forge/<sha>/)
// .claude-plugin/plugin.json registers ForgeHookSpec (hooks). This fully duplicates
// the project-level settings.local.json hooks written by forge init (GenerateSettings)
// — Claude Code merges both registrations → the same hook runs twice (perf ×2 +
// advisory noise ×2; idempotent so no errors, but redundant).
//
// Resolution: GenerateSettings stays a pure function (always writes); plugin
// detection lives only at the command layer (init.go / sync.go's
// dedupeProjectLevelIfPlugin) — after all writes complete, StripForgeHooks is called
// uniformly to clean project-level duplicate hooks so the user-level plugin takes
// over. StripForgeMCPServer additionally cleans leftover .mcp.json in old projects
// where historical init/sync wrote the forge MCP server (the MCP layer has been
// fully removed; the plugin no longer ships .mcp.json, this only cleans old residue).
// Detection is deliberately kept out of Translate / GenerateSettings so unit tests
// do not depend on the global IsClaudePluginInstalled state.
//
// plugin_detect.go — 检测 forge 是否作为 Claude Code user-level plugin 已装。
//
// 背景：plugin（user-level，~/.claude/plugins/cache/forge/forge/<sha>/）的
// .claude-plugin/plugin.json 注册了 ForgeHookSpec（hooks）。这与 forge init 写的
// project-level settings.local.json 的 hooks（GenerateSettings 写）完全重复——
// Claude Code 合并两份注册 → 同一 hook 跑两遍（性能 ×2 + advisory 噪音 ×2，
// 幂等所以不出错，但冗余）。
//
// 解法：GenerateSettings 保持纯函数（永远写），plugin 检测只在命令层
// （init.go / sync.go 的 dedupeProjectLevelIfPlugin）——所有写入完成后统一调
// StripForgeHooks 清理 project-level 重复 hooks，让 plugin user-level 接管。
// StripForgeMCPServer 另清历史 init/sync 写过 forge MCP server 的旧项目 .mcp.json 残留
// （MCP 层已全拆，plugin 不再带 .mcp.json，仅清旧残留）。
// 检测不放 Translate / GenerateSettings 内，避免单元测试依赖全局 IsClaudePluginInstalled 状态。

// ClaudeHome returns the Claude Code configuration home directory. It prefers the
// CLAUDE_CONFIG_DIR env var (a custom config directory supported by Claude Code),
// falling back to ~/.claude. Used by IsClaudePluginInstalled to resolve the location
// of installed_plugins.json. An empty string means the home could not be resolved
// (callers should treat this as not installed).
//
// ClaudeHome 返回 Claude Code 配置 home 目录。优先 CLAUDE_CONFIG_DIR env（Claude Code
// 支持的自定义配置目录），fallback ~/.claude。供 IsClaudePluginInstalled 解析
// installed_plugins.json 的位置。空串表示无法解析 home（调用方应视为未装）。
func ClaudeHome() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// IsClaudePluginInstalledAt reports whether the forge plugin is installed at the
// user level under the given Claude home. It reads
// <claudeHome>/plugins/installed_plugins.json and looks for an entry whose plugin
// name is forge (key like forge@<marketplace>) and whose scope is user.
//
// It matches forge@<any marketplace>: the plugin name forge is fixed (pluginpack
// PluginName=`forge`), while the marketplace name may vary (users may install from a
// fork; marketplace.json name is still generator-controlled, but for robustness we
// do not assume an exact value). Only scope=user counts (a project-scope install
// does not take over the whole machine like a user-level install).
//
// Any read/parse error returns false (fail-safe: on detection failure we treat it
// as not installed and the caller takes the conservative write-project-level path —
// the worst case is mere duplication, never missed hooks due to a detection fault).
//
// IsClaudePluginInstalledAt 报告给定 Claude home 下是否在 user-level 安装了 forge plugin。
// 读 <claudeHome>/plugins/installed_plugins.json，找 plugin 名为 forge（key 形如
// forge@<marketplace>）且 scope=user 的条目。
//
// 匹配 forge@<任意 marketplace>：plugin 名 forge 固定（pluginpack PluginName="forge"），
// marketplace 名可变（用户可从 fork 安装，marketplace.json name 仍由生成器定但稳健起见不
// 假设精确值）。仅认 scope=user（project-scope 装的不是 user-level 全机器接管）。
//
// 任何读/解析错误均返回 false（fail-safe：检测失败时视为未装，调用方走"写 project-level"
// 的保守路径，最坏只是重复，不会因检测故障而漏接 hooks）。
func IsClaudePluginInstalledAt(claudeHome string) bool {
	if claudeHome == "" {
		return false
	}
	path := filepath.Join(claudeHome, "plugins", "installed_plugins.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var reg struct {
		Plugins map[string][]struct {
			Scope string `json:"scope"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return false
	}
	for key, installs := range reg.Plugins {
		if !strings.HasPrefix(key, "forge@") {
			continue
		}
		for _, inst := range installs {
			if inst.Scope == "user" {
				return true
			}
		}
	}
	return false
}

// IsClaudePluginInstalled is a convenience wrapper around
// IsClaudePluginInstalledAt(ClaudeHome()) for callers such as init, the claudecode
// translator, and forge plugin dedupe to detect on the current machine.
//
// IsClaudePluginInstalled 是 IsClaudePluginInstalledAt(ClaudeHome()) 的便捷封装，
// 供 init / claudecode translator / forge plugin dedupe 等调用方检测当前机器。
func IsClaudePluginInstalled() bool {
	return IsClaudePluginInstalledAt(ClaudeHome())
}

// StripForgeHooksUserLevel removes forge hooks from the user-level
// settings.local.json (ClaudeHome()/settings.local.json). plugin.json already
// registers every ForgeHookSpec at user level → any forge hook here is necessarily a
// duplicate (Claude Code runs the same hook twice; idempotent so no errors, but
// redundant + advisory noise ×2). Sources: historical global `forge init` writing to
// home / leftover from old npm global installs / user-placed entries — after plugin
// install these duplicate the plugin manifest.
//
// It always passes keepEmpty=true (the parameter is not accepted): user-level
// settings.local.json is a personal global config and must never be deleted wholesale;
// we only clear forge hooks and keep the {} shell (unlike project-level manual dedupe
// where the file may be deleted — project files are local and rebuildable, user files
// are global and personal and not rebuildable). When ClaudeHome() is empty
// (CLAUDE_CONFIG_DIR unset and os.UserHomeDir failed — very rare; the outer
// IsClaudePluginInstalled guard in dedupeProjectLevelIfPlugin/runPluginDedupe already
// fails fast, this guard is belt-and-suspenders) it is a no-op. Used by
// dedupeProjectLevelIfPlugin (init/sync) and runPluginDedupe (plugin dedupe /
// init-suggest SessionStart auto-call) to clean up uniformly once the plugin is
// installed.
//
// Concurrency/TOCTOU (review S1, a known trade-off not fixed here): this function
// touches the user-level file at the end of every non-init forge command via
// autoSync's defer, and that file is shared by all forge projects. StripForgeHooksAt
// does read-modify-write (os.WriteFile is non-atomic, no temp+rename) — when two
// forge processes (e.g. terminal A `forge status` + terminal B hook callback) or a
// process and the user's editor write concurrently, the later writer overwrites the
// earlier one based on a stale buffer and may lose the user's intermediate edits. The
// project level has the same nature but the blast radius is limited to a single
// project; the user level is globally shared and riskier. Idempotency limits actual
// disk writes to the single run that still has forge hooks to clear (subsequent runs
// are read-only no-ops), compressing the window to a single cleanup. Full convergence
// is deferred: switch to os.WriteFile(tmp)+os.Rename(tmp,path) on write (same-directory
// rename is atomic; on Windows it holds within the same volume).
//
// StripForgeHooksUserLevel 移除 user-level settings.local.json（ClaudeHome()/settings.local.json）
// 中的 forge hooks。plugin.json 已在 user-level 注册全部 ForgeHookSpec → 此处的 forge hook
// 必然重复（Claude Code 双跑同 hook,幂等不出错但冗余 + advisory 噪音 ×2）。来源:历史 global
// forge init 写 home / 旧 npm 全局安装残留 / 用户手动放过——plugin install 后这些与 plugin
// manifest 重复。
//
// 始终 keepEmpty=true（不接受参数）:user-level settings.local.json 是用户个人全局配置,绝不
// 删整个文件,只清 forge hooks 保留 {} 壳（与 project-level 手动 dedupe 可删文件不同——project
// 文件是项目局部可重建,user 文件是全局个人不可重建）。ClaudeHome()=空（CLAUDE_CONFIG_DIR 未设
// 且 os.UserHomeDir 失败,极罕见；调用方 dedupeProjectLevelIfPlugin/runPluginDedupe 的更外层
// IsClaudePluginInstalled guard 已先 fail-skip,本 guard 是 belt-and-suspenders）时 no-op。
// 供 dedupeProjectLevelIfPlugin（init/sync）与 runPluginDedupe（plugin dedupe / init-suggest
// SessionStart 自动调用）在 plugin 已装时统一清理。
//
// 并发/TOCTOU（review S1,已知权衡非本次修）：本函数经 autoSync 的 defer 在每条非 init forge
// 命令末尾触达 user 级文件,而该文件被所有 forge 项目共享。StripForgeHooksAt 是 read-modify-write
// （os.WriteFile 非原子,无 temp+rename）——两个 forge 进程（如终端 A forge status + 终端 B hook
// 回调）或进程与用户编辑器同时改写时,后写者基于旧 buffer 覆盖先写者,可能丢用户中间的编辑。
// project 级同性质但爆炸半径限单项目；user 级是全局共享,风险更高。幂等性把实际写盘限制在
// "仍有 forge hook 待清"的那一次（清完后续 read-only no-op）,把窗口压到单次清理。彻底收敛留待
// 后续：写时改 os.WriteFile(tmp)+os.Rename(tmp,path)（同目录 rename 原子,Windows 同卷成立）。
func StripForgeHooksUserLevel() (changed bool, err error) {
	home := ClaudeHome()
	if home == "" {
		return false, nil
	}
	// After user-level-assets, forge's canonical user-level hook registration is
	// settings.json (written by GenerateUserSettings); settings.local.json only
	// carries legacy residue. Strip both.
	//
	// user-level-assets 之后，forge 的用户级 hook 注册正主是 settings.json
	// （GenerateUserSettings 写）；settings.local.json 只剩遗留残留。两者都剥。
	c1, err := StripForgeHooksAt(filepath.Join(home, "settings.json"), true)
	if err != nil {
		return c1, err
	}
	c2, err := StripForgeHooksAt(filepath.Join(home, "settings.local.json"), true)
	return c1 || c2, err
}
