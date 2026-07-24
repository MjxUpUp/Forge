package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

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

// IsClaudePluginInstalled 是 IsClaudePluginInstalledAt(ClaudeHome()) 的便捷封装，
// 供 init / claudecode translator / forge plugin dedupe 等调用方检测当前机器。
func IsClaudePluginInstalled() bool {
	return IsClaudePluginInstalledAt(ClaudeHome())
}

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
	return StripForgeHooksAt(filepath.Join(home, "settings.local.json"), true)
}
