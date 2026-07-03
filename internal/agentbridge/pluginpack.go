package agentbridge

// Plugin pack 生成：让 forge 通过各 agent 的 plugin marketplace 一键分发。采用多 host
// 插件市场的通用模式：薄 manifest + 共享内容，单仓即 marketplace。
//
// 生成结构（写入 spec.RepoDir）：
//
//	.claude-plugin/marketplace.json   claude+copilot 官方文档确认扫描此目录；codex
//	                                  (OpenAI 未明确路径)按兼容性假设——README 指引 codex
//	                                  用户额外跑 forge init --agents codex，故即使 entry
//	                                  对 codex 无效，安装路径仍可达
//	.cursor-plugin/marketplace.json   cursor 独立（只扫自己的 .cursor-plugin/）
//	plugins/<PluginName>/
//	  .claude-plugin/plugin.json      claude plugin manifest：hooks 字段 = ForgeHookSpec，
//	                                  让 `claude plugin install <name>` 直接获得与 forge init
//	                                  字节相同的 gate 接线（单一真相源）
//	  .mcp.json                       共享 MCP（claude/codex 自动发现 plugin 目录下）
//	  README.md                       每 host 一段安装命令
//
// 关键设计：source 用 ./plugins/<PluginName> 子目录而非仓库根 —— forge 是 Go 工具仓
// （internal/cmd/...），须把插件配置隔离到子目录，避免整个源码树被当插件拉取。
//
// 省略 version 字段：claude marketplace 用 git commit SHA 驱动每次 commit 自动更新
// （claude plugin 文档确认省略 version → SHA），forge v1.0 迭代期合适，且简化 generator
// （无 version 常量 drift）、golden test 更稳。
//
// owner 字段：claude marketplace schema 把 owner 标为 REQUIRED（marketplaces 文档
// "Marketplace schema → Required fields"）。故 GeneratePluginPack 在 OwnerName 空时
// 报错，DefaultPluginPack 预填 forge 的 owner（MjxUpUp）。
//
// 覆盖范围：marketplace 模型的工具（claude/cursor；codex/copilot 复用 claude marketplace）。
// opencode/pi 走各自项目级/包级生成器（opencode.go 的 forge.ts、pi 的 pi install），
// 不在 marketplace 模型内。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// DefaultPluginDescription 是 plugin/marketplace 描述的单一真相，被 DefaultPluginPack
// 与 CLI flag 默认值共用（避免 DefaultPluginPack("").Description 这种为取字段造空 spec
// 的反模式）。
const DefaultPluginDescription = "Forge loop-engineering quality gates: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion for AI coding agents."

// PluginPackSpec 配置生成的 plugin pack。OwnerName 是 required（claude marketplace schema），
// RepoSlug/OwnerEmail 用于品牌化 marketplace manifest 与 README 安装命令。
type PluginPackSpec struct {
	RepoDir         string // 仓库根：marketplaces + plugins/ 写入此目录
	RepoSlug        string // github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	MarketplaceName string // marketplace 标识，如 "forge"
	PluginName      string // plugin 标识，如 "forge"
	Description     string
	OwnerName       string // required（schema）；marketplace owner + plugin author 的 name
	OwnerEmail      string // optional；marketplace owner + plugin author 的 email
}

// DefaultPluginPack 返回填好 forge 默认值的 spec（含 owner=MjxUpUp 满足 schema required）。
// 调用方可覆盖 OwnerName/OwnerEmail/RepoSlug 来品牌化。
func DefaultPluginPack(repoDir string) PluginPackSpec {
	return PluginPackSpec{
		RepoDir:         repoDir,
		RepoSlug:        "MjxUpUp/Forge",
		MarketplaceName: "forge",
		PluginName:      "forge",
		Description:     DefaultPluginDescription,
		OwnerName:       "MjxUpUp",
	}
}

// GeneratePluginPack 在 spec.RepoDir 下写多 host plugin pack（文件布局见文件头注释）。
// OwnerName 空时报错（claude marketplace schema required）；幂等：重跑就地覆盖。
func GeneratePluginPack(spec PluginPackSpec) error {
	if spec.OwnerName == "" {
		return fmt.Errorf("plugin pack: OwnerName is required (claude marketplace schema marks owner as required); pass --owner-name or use DefaultPluginPack")
	}
	if spec.MarketplaceName == "" || spec.PluginName == "" {
		return fmt.Errorf("plugin pack: MarketplaceName and PluginName are required")
	}

	// 2 份 marketplace。claude+copilot 官方文档确认扫 .claude-plugin/；cursor 扫
	// .cursor-plugin/。codex 路径 OpenAI 未明确，按兼容性假设（见文件头注释）。
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

// ownerMap 构建 owner/author 对象。name 总在（GeneratePluginPack 已校验非空），email 可选。
func ownerMap(spec PluginPackSpec) map[string]string {
	m := map[string]string{"name": spec.OwnerName}
	if spec.OwnerEmail != "" {
		m["email"] = spec.OwnerEmail
	}
	return m
}

// writeMarketplace 写一份 marketplace.json（claude 与 cursor 各一份，格式相同，仅目录不同）。
// 结构遵循 claude marketplace schema：{name, description, owner, plugins:[{name, description, source, author}]}。
// source 跟随 PluginName（非硬编码），省略 version（git SHA 驱动自动更新）。
func writeMarketplace(spec PluginPackSpec, dir string) error {
	owner := ownerMap(spec) // name 必有，email 可选——复用一次填 owner 与 author
	entry := map[string]any{
		"name":        spec.PluginName,
		"description": spec.Description,
		"source":      "./plugins/" + spec.PluginName,
		"author":      owner,
	}
	mp := map[string]any{
		"name":        spec.MarketplaceName,
		"description": "Forge plugin marketplace",
		"owner":       owner,
		"plugins":     []map[string]any{entry},
	}
	return writeJSONIndent(filepath.Join(dir, "marketplace.json"), mp)
}

// writeClaudePluginManifest 写 plugins/<name>/.claude-plugin/plugin.json。hooks 字段是
// hooks.ForgeHookSpec() 返回的同一个对象（也是 GenerateSettings 写到 settings.local.json
// "hooks" key 下的那个），故 `claude plugin install <name>` 得到的 gate 接线与 `forge init`
// 字节一致——单一真相源。TestPluginPack_HooksMirrorSettings 守卫此相等性。
func writeClaudePluginManifest(spec PluginPackSpec, pluginDir string) error {
	manifest := map[string]any{
		"name":        spec.PluginName,
		"description": spec.Description,
		"hooks":       hooks.ForgeHookSpec(),
	}
	return writeJSONIndent(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), manifest)
}

// writePluginMCP 写 plugins/<name>/.mcp.json。Claude Code 与 Codex 自动发现已装 plugin
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

// pluginReadme 返回按 host 分列安装命令的 README。代码块用 4-space 缩进（markdown 标准代码块，
// 不依赖 ``` fence）；行内命令用 \x60 转义反引号包裹（Go 双引号 string 里 \x60 = 反引号字符，
// 源码里是 ASCII 转义序列而非裸反引号，规避 Windows 输入引号腐蚀 + 不与 raw-string 界定冲突，
// 见 memory windows-input-quote-corruption，与 mcpconfig.go 的 rune(34) 同理）。
func pluginReadme(repoSlug string) string {
	return "# Forge plugin\n\n" +
		"Forge brings loop-engineering quality gates to your AI coding agent: " +
		"task-tracked source changes, assertion guards, file-sentinel quarantine, " +
		"and review-gated completion.\n\n" +
		"## Install by host\n\n" +
		"### Claude Code\n\n" +
		"Register the marketplace, then install. This wires the full gate set " +
		"(hooks + MCP) for Claude Code:\n\n" +
		"    /plugin marketplace add " + repoSlug + "\n" +
		"    /plugin install forge@forge\n\n" +
		"### Codex (CLI / App)\n\n" +
		"Codex CLI's plugin marketplace path is not officially confirmed to scan " +
		".claude-plugin/ (OpenAI docs do not specify the path). The commands below " +
		"assume schema compatibility; if they fail, skip this section and run " +
		"\x60forge init --agents codex\x60 for full .codex gate wiring.\n\n" +
		"    codex plugin marketplace add " + repoSlug + "\n" +
		"    codex plugin install forge@forge\n\n" +
		"### Cursor\n\n" +
		"    /plugin marketplace add " + repoSlug + "\n" +
		"    /plugin install forge@forge\n\n" +
		"Cursor's plugin model carries skills/MCP, not Claude-shape hooks. Run " +
		"\x60forge init --agents cursor\x60 in your project for .cursor MCP wiring.\n\n" +
		"### GitHub Copilot CLI\n\n" +
		"Copilot officially scans .claude-plugin/marketplace.json:\n\n" +
		"    copilot plugin marketplace add " + repoSlug + "\n" +
		"    copilot plugin install forge@forge\n\n" +
		"For .github/instructions + MCP, run \x60forge init --agents copilot\x60.\n\n" +
		"## Requirements\n\n" +
		"\x60forge\x60 must be on PATH (hooks and the MCP server spawn \x60forge ...\x60). Install:\n\n" +
		"    npm install -g @mjxupup/forge\n\n" +
		"## What the plugin provides (Claude Code)\n\n" +
		"- hooks (\x60.claude-plugin/plugin.json\x60): PreToolUse/PostToolUse/Stop/SessionStart " +
		"gates - identical to forge init's \x60.claude/settings.local.json\x60.\n" +
		"- MCP (\x60.mcp.json\x60): 15 forge tools (resume/decide/attach + task/board/experience).\n\n" +
		"Other hosts: the plugin is the distribution entry point; per-project gate " +
		"wiring comes from \x60forge init --agents <host>\x60.\n"
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
