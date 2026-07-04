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
	"strings"

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

// pluginReadme 返回三步首体验 README：binary（机器级）→ plugin（agent 级）→ init（项目级）。
// 用 strings.Builder 而非长 + 链（内容已扩到三步 + caveat，拼接链易错）。
//
// 能力边界必须诚实（用户要求"forge 分发纯净闭环可用"，且不能虚假宣传"一次安装处处完美"）：
// plugin 只接用户级 hooks+MCP，不含项目级 .forge/CLAUDE.md/AGENTS.md/skills——后者每项目
// 仍需 forge init。故 README 分三步明示，并单列 caveat（用户级 hooks 在非 forge 项目会 WARN）。
//
// 代码块用 4-space 缩进（markdown 标准代码块，不依赖 ``` fence）；行内命令用 \x60 转义反引号
// 包裹（Go 双引号 string 里 \x60 = 反引号字符，源码里是 ASCII 转义序列而非裸反引号，规避
// Windows 输入引号腐蚀 + 不与 raw-string 界定冲突，见 memory windows-input-quote-corruption，
// 与 mcpconfig.go 的 rune(34) 同理）。内容不出现裸双引号，同理防腐蚀。
//
// npm 包名必须是 @agent_forge/forge（与 npm/package.json 一致），不是 GitHub owner slug。
// 早期版本这里写过 @mjxupup/forge，指向不存在的包——TestPluginPack_Readme 守卫此回退。
func pluginReadme(repoSlug string) string {
	var sb strings.Builder
	sb.WriteString("# Forge plugin\n\n")
	sb.WriteString("Forge brings loop-engineering quality gates to your AI coding agent: " +
		"task-tracked source changes, assertion guards, file-sentinel quarantine, " +
		"and review-gated completion.\n\n")

	// 三步首体验。
	sb.WriteString("## Three-step setup\n\n")
	sb.WriteString("Forge has two parts: a Go binary (the engine that hooks and MCP " +
		"spawn) and this plugin (the wiring that tells your agent where to call it). " +
		"Install the binary first, then the plugin, then init each project.\n\n")

	// Step 1 — 二进制是硬前置：hooks 和 MCP 都 spawn forge，没二进制 plugin 装了也跑不动。
	sb.WriteString("### 1. Install the forge binary (required, once per machine)\n\n")
	sb.WriteString("Hooks and the MCP server spawn \x60forge ...\x60, so the binary must " +
		"be on PATH before the plugin can do anything.\n\n")
	sb.WriteString("    npm install -g @agent_forge/forge\n\n")

	// Step 2 — plugin（每 agent 一次）。用户级：hooks+MCP 对所有项目生效。
	sb.WriteString("### 2. Install the plugin (once per agent)\n\n")
	sb.WriteString("Register the marketplace, then install. This wires the gate set " +
		"(hooks + MCP) at the user level — every project on this machine gets the " +
		"plugin wiring, with no per-project plugin install. Project assets (.forge/, " +
		"protocol, skills) still need forge init (see step 3).\n\n")
	sb.WriteString("#### Claude Code\n\n")
	sb.WriteString("    /plugin marketplace add " + repoSlug + "\n")
	sb.WriteString("    /plugin install forge@forge\n\n")
	sb.WriteString("#### Codex (CLI / App)\n\n")
	sb.WriteString("Codex CLI's plugin marketplace path is not officially confirmed to scan " +
		".claude-plugin/ (OpenAI docs do not specify the path). The commands below " +
		"assume schema compatibility; if they fail, skip this section and run " +
		"\x60forge init --agents codex\x60 for full .codex gate wiring.\n\n")
	sb.WriteString("    codex plugin marketplace add " + repoSlug + "\n")
	sb.WriteString("    codex plugin install forge@forge\n\n")
	sb.WriteString("#### Cursor\n\n")
	sb.WriteString("    /plugin marketplace add " + repoSlug + "\n")
	sb.WriteString("    /plugin install forge@forge\n\n")
	sb.WriteString("Cursor's plugin model carries skills/MCP, not Claude-shape hooks. Run " +
		"\x60forge init --agents cursor\x60 in your project for .cursor MCP wiring.\n\n")
	sb.WriteString("#### GitHub Copilot CLI\n\n")
	sb.WriteString("Copilot officially scans .claude-plugin/marketplace.json:\n\n")
	sb.WriteString("    copilot plugin marketplace add " + repoSlug + "\n")
	sb.WriteString("    copilot plugin install forge@forge\n\n")
	sb.WriteString("For .github/instructions + MCP, run \x60forge init --agents copilot\x60.\n\n")

	// Step 3 — 项目 init（每项目一次）。诚实能力边界：plugin 是用户级 hooks+MCP，
	// 项目级资产（.forge/CLAUDE.md/AGENTS.md/skills）不在 plugin 内，必须每项目生成。
	// 没有这一步，plugin install 单独不构成完整体验——这是"一次安装处处完美"的真实缺口。
	sb.WriteString("### 3. Initialize each project (once per project)\n\n")
	sb.WriteString("The plugin wires user-level hooks + MCP. It does NOT create the " +
		"project-level assets forge needs to run: the \x60.forge/\x60 pipeline/task state, " +
		"the \x60CLAUDE.md\x60/\x60AGENTS.md\x60 protocol, and the canonical skills " +
		"(\x60/forge-pipeline\x60, \x60/forge-quality\x60, ...). Generate them per project:\n\n")
	sb.WriteString("    cd your-project\n")
	sb.WriteString("    forge init\n\n")
	sb.WriteString("Complete setup: binary (machine) -> plugin (agent) -> init (project).\n\n")

	// 提供什么——Claude Code 完整；其他 host 仅入口。
	sb.WriteString("## What the plugin provides\n\n")
	sb.WriteString("Claude Code (full): hooks (\x60.claude-plugin/plugin.json\x60) = " +
		"PreToolUse/PostToolUse/Stop/SessionStart gates, identical to forge init's " +
		"\x60.claude/settings.local.json\x60 but user-level (all projects); MCP " +
		"(\x60.mcp.json\x60) = 15 forge tools (resume/decide/attach + task/board/experience).\n\n")
	sb.WriteString("Other hosts: the plugin is the distribution entry point (MCP + " +
		"marketplace listing); per-project gate wiring (hooks, .forge/, protocol) comes " +
		"from \x60forge init --agents <host>\x60.\n\n")

	// Caveat — 用户级 hooks 在每个 Claude Code 项目触发，包括用户不想用 forge 的项目。
	// 无 .forge/ 时 task-guard 每次源码编辑 WARN。这是"install once, everywhere"的真实代价，
	// 必须前置说明——否则用户会在无关项目被噪声困扰。
	sb.WriteString("## Caveat: projects you do not want forge in\n\n")
	sb.WriteString("User-level hooks fire in every Claude Code project. In projects " +
		"without \x60.forge/\x60, task-guard emits a WARN (\x60allowed but not tracked\x60) " +
		"on each source edit — noisy but non-blocking. To silence, either \x60forge init\x60 " +
		"the project or uninstall the plugin.\n")
	return sb.String()
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
