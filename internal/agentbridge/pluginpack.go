package agentbridge

// Plugin pack 生成：让 forge 通过各 agent 的 plugin marketplace 一键分发，参考
// obra/superpowers 的多 host 模式（薄 manifest + 共享内容，单仓即 marketplace）。
//
// 生成结构（写入 spec.RepoDir）：
//
//	.claude-plugin/marketplace.json   claude+codex+copilot 共享读（已验证三工具
//	                                  都扫描此路径），source: ./plugins/forge
//	.cursor-plugin/marketplace.json   cursor 独立（cursor 只扫自己的 .cursor-plugin/）
//	plugins/forge/
//	  .claude-plugin/plugin.json      claude plugin manifest：hooks 字段 = ForgeHookSpec，
//	                                  让 `claude plugin install forge` 直接获得与 forge init
//	                                  字节相同的 gate 接线（单一真相源）
//	  .mcp.json                       共享 MCP（claude/codex 自动发现 plugin 目录下）
//	  README.md                       每 host 安装命令（仿 superpowers README 格式）
//
// 与 superpowers 的关键差异：source 用 ./plugins/forge 子目录而非 ./ —— forge 是 Go
// 工具仓（internal/cmd/...），须把插件配置隔离到子目录，避免整个源码树被当插件拉取
// （superpowers 是纯 markdown skills 仓，整个仓库即插件，故用 ./）。
//
// 省略 version 字段：claude marketplace 用 git commit SHA 驱动每次 commit 自动更新
// （claude plugin 文档确认省略 version → SHA），forge v1.0 迭代期合适，且简化 generator
// （无 version 常量 drift）、golden test 更稳。superpowers 用显式 version 因有稳定 release。
//
// 覆盖范围：marketplace 模型的工具（claude/codex/cursor；copilot 复用 claude marketplace）。
// opencode/pi 走各自项目级/包级生成器（opencode.go 的 forge.ts、pi 的 pi install），
// 不在 marketplace 模型内——与 superpowers 一致（opencode 走 INSTALL.md，pi 走 pi install）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// pluginSource 是 marketplace entry 指向 plugin 树的相对路径。用子目录而非 superpowers
// 的 "./"：forge 仓库是 Go 工具链，internal/cmd/... 不能被当插件内容拉取。
const pluginSource = "./plugins/forge"

// PluginPackSpec 配置生成的 plugin pack。Owner/RepoSlug 用于品牌化 marketplace manifest
// 与 README 安装命令。
type PluginPackSpec struct {
	RepoDir         string // 仓库根：marketplaces + plugins/ 写入此目录
	RepoSlug        string // github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	MarketplaceName string // marketplace 标识，如 "forge"
	PluginName      string // plugin 标识，如 "forge"
	Description     string
	OwnerName       string
	OwnerEmail      string
}

// DefaultPluginPack 返回填好 forge 默认值的 spec。调用方可覆盖 OwnerName/OwnerEmail/
// RepoSlug 来品牌化。
func DefaultPluginPack(repoDir string) PluginPackSpec {
	return PluginPackSpec{
		RepoDir:         repoDir,
		RepoSlug:        "MjxUpUp/Forge",
		MarketplaceName: "forge",
		PluginName:      "forge",
		Description:     "Forge loop-engineering quality gates: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion for AI coding agents.",
	}
}

// GeneratePluginPack 在 spec.RepoDir 下写多 host plugin pack（文件布局见文件头注释）。
// 幂等：重跑就地覆盖每个生成文件。
func GeneratePluginPack(spec PluginPackSpec) error {
	// 2 份 marketplace。claude+codex+copilot 都扫 .claude-plugin/；cursor 扫
	// .cursor-plugin/ —— 故两份文件覆盖四个 host（已逐工具文档核实 2026-07），而非四份冗余。
	if err := writeMarketplace(spec, filepath.Join(spec.RepoDir, ".claude-plugin")); err != nil {
		return err
	}
	if err := writeMarketplace(spec, filepath.Join(spec.RepoDir, ".cursor-plugin")); err != nil {
		return err
	}

	pluginDir := filepath.Join(spec.RepoDir, "plugins", spec.PluginName)
	if err := writeClaudePluginManifest(spec, pluginDir); err != nil {
		return err
	}
	if err := writePluginMCP(pluginDir); err != nil {
		return err
	}
	if err := writePluginReadme(spec, pluginDir); err != nil {
		return err
	}
	return nil
}

// ownerMap 构建 owner/author 对象，空字段省略。两者皆空时返回 nil，让 manifest 整个省略
// 该 key。
func ownerMap(spec PluginPackSpec) map[string]string {
	m := map[string]string{}
	if spec.OwnerName != "" {
		m["name"] = spec.OwnerName
	}
	if spec.OwnerEmail != "" {
		m["email"] = spec.OwnerEmail
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// writeMarketplace 写一份 marketplace.json（claude 与 cursor 各一份，格式相同，仅目录不同）。
// 结构仿 superpowers：{name, description, owner?, plugins:[{name, description, source, author?}]}。
// 省略 version（git SHA 驱动自动更新）。
func writeMarketplace(spec PluginPackSpec, dir string) error {
	entry := map[string]any{
		"name":        spec.PluginName,
		"description": spec.Description,
		"source":      pluginSource,
	}
	if owner := ownerMap(spec); owner != nil {
		entry["author"] = owner
	}
	mp := map[string]any{
		"name":        spec.MarketplaceName,
		"description": "Forge plugin marketplace",
		"plugins":     []map[string]any{entry},
	}
	if owner := ownerMap(spec); owner != nil {
		mp["owner"] = owner
	}
	return writeJSONIndent(filepath.Join(dir, "marketplace.json"), mp)
}

// writeClaudePluginManifest 写 plugins/forge/.claude-plugin/plugin.json。hooks 字段是
// hooks.ForgeHookSpec() 返回的同一个对象（也是 GenerateSettings 写到 settings.local.json
// "hooks" key 下的那个），故 `claude plugin install forge` 得到的 gate 接线与 `forge init`
// 字节一致——单一真相源。TestPluginPack_HooksMirrorSettings 守卫此相等性。
func writeClaudePluginManifest(spec PluginPackSpec, pluginDir string) error {
	manifest := map[string]any{
		"name":        spec.PluginName,
		"description": spec.Description,
		"hooks":       hooks.ForgeHookSpec(),
	}
	return writeJSONIndent(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), manifest)
}

// writePluginMCP 写 plugins/forge/.mcp.json。Claude Code 与 Codex 自动发现已装 plugin
// 目录下的 .mcp.json，暴露 forge MCP server（15 工具：resume/decide/attach + task/board/
// experience）。server 定义与 writeClaudeMCP 一致（同 forge command/args）。
func writePluginMCP(pluginDir string) error {
	mcp := map[string]any{
		"mcpServers": map[string]any{
			"forge": map[string]any{
				"command": forgeMCPCommand,
				"args":    forgeMCPArgs,
			},
		},
	}
	return writeJSONIndent(filepath.Join(pluginDir, ".mcp.json"), mcp)
}

func writePluginReadme(spec PluginPackSpec, pluginDir string) error {
	slug := spec.RepoSlug
	if slug == "" {
		slug = "MjxUpUp/Forge"
	}
	return os.WriteFile(filepath.Join(pluginDir, "README.md"), []byte(pluginReadme(slug)), 0644)
}

// pluginReadme 返回按 host 分列安装命令的 README。代码块用 4-space 缩进（非 ``` fence），
// 行内命令裸文本（不用反引号包裹），这样整段能放进 Go raw-string 字面量而不与界定反引号
// 冲突——规避 Windows 输入引号腐蚀（见 memory windows-input-quote-corruption）。
func pluginReadme(repoSlug string) string {
	return `# Forge plugin

Forge brings loop-engineering quality gates to your AI coding agent:
task-tracked source changes, assertion guards, file-sentinel quarantine, and
review-gated completion.

## Install by host

### Claude Code

Register the marketplace, then install. This wires the full gate set
(hooks + MCP) for Claude Code:

    /plugin marketplace add ` + repoSlug + `
    /plugin install forge@forge

### Codex (CLI / App)

Codex reads the same .claude-plugin/marketplace.json. Install via the Codex
plugin search, then run  forge init --agents codex  in your project for .codex
hook wiring (the marketplace entry points here; per-project gates come from
forge init).

### Cursor

    /plugin marketplace add ` + repoSlug + `

Cursor's plugin model carries skills/MCP, not Claude-shape hooks. Run
 forge init --agents cursor  in your project for .cursor MCP wiring.

### GitHub Copilot CLI

Copilot falls back to .claude-plugin/marketplace.json:

    copilot plugin marketplace add ` + repoSlug + `
    copilot plugin install forge@forge

For .github/instructions + MCP, run  forge init --agents copilot .

## Requirements

forge must be on PATH (hooks and the MCP server spawn  forge ...). Install:

    npm install -g @mjxupup/forge

## What the plugin provides (Claude Code)

- hooks (.claude-plugin/plugin.json): PreToolUse/PostToolUse/Stop/SessionStart
  gates - identical to forge init's .claude/settings.local.json.
- MCP (.mcp.json): 15 forge tools (resume/decide/attach + task/board/experience).

Other hosts: the plugin is the distribution entry point; per-project gate
wiring comes from  forge init --agents <host> .
`
}

// writeJSONIndent 以 2-space 缩进写 JSON 到 path（自动建父目录）。所有 plugin pack 文件
// 走此 helper，保证格式一致（golden test 依赖此缩进）。
func writeJSONIndent(path string, v any) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
