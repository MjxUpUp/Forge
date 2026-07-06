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
// .claude-plugin/plugin.json 注册了 ForgeHookSpec（13 hooks），.mcp.json 注册了
// forge MCP server。这与 forge init 写的 project-level 资产完全重复：
//   - .claude/settings.local.json 的 hooks（GenerateSettings 写）
//   - 项目根 .mcp.json 的 forge server（agentbridge.writeClaudeMCP 写）
// Claude Code 合并两份注册 → 同一 hook/MCP 跑两遍（性能 ×2 + advisory 噪音 ×2，
// 幂等所以不出错，但冗余）。
//
// 解法：GenerateSettings / writeClaudeMCP 保持纯函数（永远写），plugin 检测只在命令层
// （init.go / sync.go 的 dedupeProjectLevelIfPlugin）——所有写入完成后统一调
// StripForgeHooks / StripForgeMCPServer 清理 project-level 重复，让 plugin user-level 接管。
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
