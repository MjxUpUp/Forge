package agentbridge

// Plugin pack generation: lets forge distribute via each agent's plugin marketplace in
// one click. Adopts the multi-host plugin marketplace pattern: thin manifest + shared
// content, single repo = marketplace.
//
// Generated structure (written under spec.RepoDir):
//
//	.claude-plugin/marketplace.json   claude+copilot official docs confirm scanning this dir; codex
//	                                  (OpenAI path unconfirmed) assumes compatibility — README directs codex
//	                                  users to additionally run forge init --agents codex, so even if the entry
//	                                  is invalid for codex, the install path is still reachable
//	.cursor-plugin/marketplace.json   cursor independent (only scans its own .cursor-plugin/)
//	plugins/<PluginName>/
//	  .claude-plugin/plugin.json      claude plugin manifest: hooks field = ForgeHookSpec,
//	                                  so `claude plugin install <name>` directly gets the same gate
//	                                  wiring byte-identical to forge init (single source of truth)
//	  README.md                       one install-command block per host
//
// Key design: source uses the ./plugins/<PluginName> subdirectory rather than the repo
// root — forge is a Go tool repo (internal/cmd/...); plugin config must be isolated to a
// subdirectory to avoid the whole source tree being pulled as a plugin.
//
// version field omitted: claude marketplace uses git commit SHA so each commit auto-updates
// (claude plugin docs confirm omitted version → SHA); fits forge v1.0 iteration, and
// simplifies the generator (no version-constant drift), golden tests more stable.
//
// owner field: claude marketplace schema marks owner as REQUIRED (marketplaces doc
// "Marketplace schema → Required fields"). Hence GeneratePluginPack errors when OwnerName
// is empty, and DefaultPluginPack pre-fills forge's owner (MjxUpUp).
//
// Coverage: marketplace-model tools (claude/cursor; codex/copilot reuse claude marketplace).
// opencode/pi go through their own project-level/package-level generators (opencode.go's
// forge.ts, pi's pi install), outside the marketplace model.
//
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

// DefaultPluginDescription is the single source of truth for the plugin/marketplace
// description, shared by DefaultPluginPack and the CLI flag default (avoids the
// DefaultPluginPack("").Description anti-pattern of fabricating an empty spec just to
// read a field).
//
// DefaultPluginDescription 是 plugin/marketplace 描述的单一真相，被 DefaultPluginPack
// 与 CLI flag 默认值共用（避免 DefaultPluginPack("").Description 这种为取字段造空 spec
// 的反模式）。
const DefaultPluginDescription = "Forge loop-engineering quality gates: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion for AI coding agents."

// PluginPackSpec configures the generated plugin pack. OwnerName is required (claude
// marketplace schema); RepoSlug/OwnerEmail brand the marketplace manifest and README install commands.
//
// PluginPackSpec 配置生成的 plugin pack。OwnerName 是 required（claude marketplace schema），
// RepoSlug/OwnerEmail 用于品牌化 marketplace manifest 与 README 安装命令。
type PluginPackSpec struct {
	// Repo root: marketplaces + plugins/ are written into this dir.
	//
	// 仓库根：marketplaces + plugins/ 写入此目录
	RepoDir         string // 仓库根：marketplaces + plugins/ 写入此目录
	// github owner/repo for install commands, e.g. MjxUpUp/Forge.
	//
	// github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	RepoSlug        string // github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	// Marketplace identifier, e.g. forge.
	//
	// marketplace 标识，如 "forge"
	MarketplaceName string // marketplace 标识，如 "forge"
	// Plugin identifier, e.g. forge.
	//
	// plugin 标识，如 "forge"
	PluginName      string // plugin 标识，如 "forge"
	Description     string
	// required (schema); the name of the marketplace owner + plugin author.
	//
	// required（schema）；marketplace owner + plugin author 的 name
	OwnerName       string // required（schema）；marketplace owner + plugin author 的 name
	// optional; the email of the marketplace owner + plugin author.
	//
	// optional；marketplace owner + plugin author 的 email
	OwnerEmail      string // optional；marketplace owner + plugin author 的 email
}

// DefaultPluginPack returns a spec pre-filled with forge defaults (owner=MjxUpUp satisfies
// schema required). Callers can override OwnerName/OwnerEmail/RepoSlug to brand it.
//
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

// GeneratePluginPack writes a multi-host plugin pack under spec.RepoDir (file layout
// shown in the file-header comment). Errors when OwnerName is empty (claude marketplace
// schema required); idempotent: re-runs overwrite in place.
//
// GeneratePluginPack 在 spec.RepoDir 下写多 host plugin pack（文件布局见文件头注释）。
// OwnerName 空时报错（claude marketplace schema required）；幂等：重跑就地覆盖。
func GeneratePluginPack(spec PluginPackSpec) error {
	if spec.OwnerName == "" {
		return fmt.Errorf("plugin pack: OwnerName is required (claude marketplace schema marks owner as required); use DefaultPluginPack for the defaults")
	}
	if spec.MarketplaceName == "" || spec.PluginName == "" {
		return fmt.Errorf("plugin pack: MarketplaceName and PluginName are required")
	}

	// 2 marketplace copies. claude+copilot official docs confirm scanning .claude-plugin/;
	// cursor scans .cursor-plugin/. codex path is unconfirmed by OpenAI — assume compatibility
	// (see file-header comment).
	//
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
	if err := writePluginReadme(spec, pluginDir); err != nil {
		return err
	}
	return nil
}

// ownerMap builds the owner/author object. name is always present (GeneratePluginPack
// already validated non-empty); email is optional.
//
// ownerMap 构建 owner/author 对象。name 总在（GeneratePluginPack 已校验非空），email 可选。
func ownerMap(spec PluginPackSpec) map[string]string {
	m := map[string]string{"name": spec.OwnerName}
	if spec.OwnerEmail != "" {
		m["email"] = spec.OwnerEmail
	}
	return m
}

// writeMarketplace writes one marketplace.json (one each for claude and cursor, same
// format, only the directory differs). Structure follows the claude marketplace schema:
// {name, description, owner, plugins:[{name, description, source, author}]}.
// source follows PluginName (not hardcoded); version is omitted (git SHA auto-updates).
//
// writeMarketplace 写一份 marketplace.json（claude 与 cursor 各一份，格式相同，仅目录不同）。
// 结构遵循 claude marketplace schema：{name, description, owner, plugins:[{name, description, source, author}]}。
// source 跟随 PluginName（非硬编码），省略 version（git SHA 驱动自动更新）。
func writeMarketplace(spec PluginPackSpec, dir string) error {
	// name always present, email optional — reuse once to fill both owner and author.
	//
	// name 必有，email 可选——复用一次填 owner 与 author
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

// writeClaudePluginManifest writes plugins/<name>/.claude-plugin/plugin.json. The hooks
// field is the same object returned by hooks.ForgeHookSpec() (also the one GenerateSettings
// writes under the `hooks` key of settings.local.json), so `claude plugin install <name>`
// yields gate wiring byte-identical to `forge init` — single source of truth.
// TestPluginPack_HooksMirrorSettings guards this equality.
//
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

func writePluginReadme(spec PluginPackSpec, pluginDir string) error {
	slug := spec.RepoSlug
	if slug == "" {
		slug = "MjxUpUp/Forge"
	}
	return os.WriteFile(filepath.Join(pluginDir, "README.md"), []byte(pluginReadme(slug)), 0644)
}

// pluginReadme returns a three-step first-run README: binary (machine-level) → plugin
// (agent-level) → init (project-level). Uses strings.Builder rather than a long +
// chain (content has grown to three steps + caveat, concat chains are error-prone).
//
// Capability boundary must be honest (user asked for "forge distribution pure closed-loop
// usable", and we must not falsely advertise "install once, perfect everywhere"):
// plugin only wires user-level hooks, not project-level .forge/CLAUDE.md/AGENTS.md/skills
// — the latter still needs forge init per project. So the README shows three steps
// explicitly and lists a separate caveat (user-level hooks WARN in non-forge projects).
//
// Code blocks use 4-space indent (markdown standard code blocks, no reliance on ``` fences);
// inline commands use \x60-escaped backticks (in Go double-quoted strings \x60 = backtick
// char; in source it is an ASCII escape sequence, not a raw backtick, avoiding Windows
// input-quote corruption + no clash with raw-string delimiters, see memory
// windows-input-quote-corruption; same reasoning as mcpconfig.go's rune(34)). Content has
// no raw double quotes — same anti-corruption reasoning.
//
// npm package name must be @agent_forge/forge (matching npm/package.json), not the GitHub
// owner slug. Earlier versions wrote @mjxupup/forge here, pointing at a non-existent
// package — TestPluginPack_Readme guards this regression.
//
// pluginReadme 返回三步首体验 README：binary（机器级）→ plugin（agent 级）→ init（项目级）。
// 用 strings.Builder 而非长 + 链（内容已扩到三步 + caveat，拼接链易错）。
//
// Capability boundary must be honest (user asked for "forge distribution pure closed-loop usable", and we must not falsely advertise "install once, perfect everywhere"):
// plugin only wires user-level hooks, not project-level .forge/CLAUDE.md/AGENTS.md/skills — the latter per project
// still needs forge init. So the README shows three steps explicitly and lists a separate caveat (user-level hooks WARN in non-forge projects).
//
// 能力边界必须诚实（用户要求"forge 分发纯净闭环可用"，且不能虚假宣传"一次安装处处完美"）：
// plugin 只接用户级 hooks，不含项目级 .forge/CLAUDE.md/AGENTS.md/skills——后者每项目
// 仍需 forge init。故 README 分三步明示，并单列 caveat（用户级 hooks 在非 forge 项目会 WARN）。
//
// Code blocks use 4-space indent (markdown standard code blocks, no reliance on ``` fences); inline commands use \x60-escaped backticks
// wrapped (Go double-quoted string \x60 = backtick char; in source it is an ASCII escape sequence, not a raw backtick, avoiding
// Windows input-quote corruption + no clash with raw-string delimiters, see memory windows-input-quote-corruption,
// same reasoning as mcpconfig.go's rune(34)). Content has no raw double quotes — same anti-corruption reasoning.
//
// 代码块用 4-space 缩进（markdown 标准代码块，不依赖 ``` fence）；行内命令用 \x60 转义反引号
// 包裹（Go 双引号 string 里 \x60 = 反引号字符，源码里是 ASCII 转义序列而非裸反引号，规避
// Windows 输入引号腐蚀 + 不与 raw-string 界定冲突，见 memory windows-input-quote-corruption，
// 与 mcpconfig.go 的 rune(34) 同理）。内容不出现裸双引号，同理防腐蚀。
//
// npm package name must be @agent_forge/forge (matching npm/package.json), not the GitHub owner slug.
// Earlier versions wrote @mjxupup/forge here, pointing at a non-existent package — TestPluginPack_Readme guards this regression.
//
// npm 包名必须是 @agent_forge/forge（与 npm/package.json 一致），不是 GitHub owner slug。
// 早期版本这里写过 @mjxupup/forge，指向不存在的包——TestPluginPack_Readme 守卫此回退。
func pluginReadme(repoSlug string) string {
	var sb strings.Builder
	sb.WriteString("# Forge plugin\n\n")
	sb.WriteString("Forge brings loop-engineering quality gates to your AI coding agent: " +
		"task-tracked source changes, assertion guards, file-sentinel quarantine, " +
		"and review-gated completion.\n\n")

	// Three-step first-run experience.
	//
	// 三步首体验。
	sb.WriteString("## Three-step setup\n\n")
	sb.WriteString("Forge has two parts: a Go binary (the engine that hooks " +
		"spawn) and this plugin (the wiring that tells your agent where to call it). " +
		"Install the binary first, then the plugin, then init each project.\n\n")

	// Step 1 — the binary is a hard prerequisite: all hooks spawn forge, so without the
	// binary the plugin cannot run anything.
	//
	// Step 1 — 二进制是硬前置：hooks 都 spawn forge，没二进制 plugin 装了也跑不动。
	sb.WriteString("### 1. Install the forge binary (required, once per machine)\n\n")
	sb.WriteString("Hooks spawn \x60forge ...\x60, so the binary must be on PATH before " +
		"the plugin can do anything.\n\n")
	sb.WriteString("    npm install -g @agent_forge/forge\n\n")

	// Step 2 — plugin (once per agent). User-level: hooks apply to all projects.
	//
	// Step 2 — plugin（每 agent 一次）。用户级：hooks 对所有项目生效。
	sb.WriteString("### 2. Install the plugin (once per agent)\n\n")
	sb.WriteString("Register the marketplace, then install. This wires the gate set " +
		"(hooks) at the user level — every project on this machine gets the " +
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
	sb.WriteString("Cursor's plugin model carries skills, not Claude-shape hooks. Run " +
		"\x60forge init --agents cursor\x60 in your project for .cursor gate wiring.\n\n")
	sb.WriteString("#### GitHub Copilot CLI\n\n")
	sb.WriteString("Copilot officially scans .claude-plugin/marketplace.json:\n\n")
	sb.WriteString("    copilot plugin marketplace add " + repoSlug + "\n")
	sb.WriteString("    copilot plugin install forge@forge\n\n")
	sb.WriteString("For .github/instructions gate wiring, run \x60forge init --agents copilot\x60.\n\n")
	sb.WriteString("#### Kimi Code\n\n")
	sb.WriteString("Kimi Code reads the plugin manifest committed at the repo root " +
		"(\x60.kimi-plugin/plugin.json\x60) — no marketplace registration needed:\n\n")
	sb.WriteString("    /plugins install https://github.com/" + repoSlug + "\n\n")
	sb.WriteString("This wires the full hook set (PreToolUse/PostToolUse/Stop/SessionStart/" +
		"PostCompact/UserPromptSubmit) at the user level. Alternative without the " +
		"plugin: \x60forge init --agents kimi\x60 writes the same hooks into " +
		"\x60~/.kimi-code/config.toml\x60 (marker-section merge). When both exist, " +
		"\x60forge init\x60 strips the config.toml section — the plugin wins and " +
		"hooks never double-run.\n\n")

	// Step 3 — project init (once per project). Honest capability boundary: plugin is
	// user-level hooks; project-level assets (.forge/CLAUDE.md/AGENTS.md/skills) are NOT in
	// the plugin and must be generated per project. Without this step, plugin install alone
	// does not make a complete experience — this is the real gap in "install once, perfect everywhere".
	//
	// Step 3 — 项目 init（每项目一次）。诚实能力边界：plugin 是用户级 hooks，
	// 项目级资产（.forge/CLAUDE.md/AGENTS.md/skills）不在 plugin 内，必须每项目生成。
	// 没有这一步，plugin install 单独不构成完整体验——这是"一次安装处处完美"的真实缺口。
	sb.WriteString("### 3. Initialize each project (once per project)\n\n")
	sb.WriteString("The plugin wires user-level hooks. It does NOT create the " +
		"project-level assets forge needs to run: the \x60.forge/\x60 task state, " +
		"the \x60CLAUDE.md\x60/\x60AGENTS.md\x60 protocol, and the canonical skills " +
		"(\x60/forge-quality\x60, ...). Generate them per project:\n\n")
	sb.WriteString("    cd your-project\n")
	sb.WriteString("    forge init\n\n")
	sb.WriteString("Complete setup: binary (machine) -> plugin (agent) -> init (project).\n\n")

	// What is provided — Claude Code is full; other hosts are entry-only.
	//
	// 提供什么——Claude Code 完整；其他 host 仅入口。
	sb.WriteString("## What the plugin provides\n\n")
	sb.WriteString("Claude Code (full): hooks (\x60.claude-plugin/plugin.json\x60) = " +
		"PreToolUse/PostToolUse/Stop/SessionStart gates, identical to forge init's " +
		"\x60.claude/settings.local.json\x60 but user-level (all projects).\n\n")
	// Backflow 4fad92e hand-maintained dedupe behavior description (kept after stripping MCP
	// wording: dedupe is the real behavior after plugin install — init-suggest auto-calls
	// forge plugin dedupe, settings keeps the {} shell without deleting user config).
	//
	// 回流 4fad92e 手维护的 dedupe 行为说明（清 MCP 字样后保留：dedupe 是 plugin install 后
	// 的真实行为——init-suggest 自动调 forge plugin dedupe,settings 保留 {} 壳不删用户配置）。
	sb.WriteString("Because the plugin already wires user-level hooks, \x60forge init\x60 " +
		"auto-dedupes the duplicates when the plugin is installed — Claude Code would " +
		"otherwise double-run hooks. This covers both the project-level " +
		"(\x60.claude/settings.local.json\x60 hooks) and the user-level " +
		"(\x60~/.claude\x60/\x60$CLAUDE_CONFIG_DIR\x60 \x60settings.local.json\x60 forge " +
		"hooks, left over from a historical global \x60forge init\x60 in the home dir or " +
		"an old global install). Existing projects are migrated automatically by the " +
		"init-suggest SessionStart hook via \x60forge plugin dedupe --keep-empty\x60 " +
		"(which also cleans the user-level file). \x60settings.local.json\x60 (both " +
		"levels) is preserved as an empty \x60{}\x60 shell — it is user-placed gitignored " +
		"config, never silently deleted (the user-level file is always preserved " +
		"regardless of \x60--keep-empty\x60, since it is the user's global config).\n\n")
	sb.WriteString("Other hosts: the plugin is the distribution entry point " +
		"(marketplace listing); per-project gate wiring (hooks, .forge/, protocol) comes " +
		"from \x60forge init --agents <host>\x60.\n\n")

	// Caveat — user-level hooks fire in every Claude Code project, including ones where the
	// user does not want forge. Without .forge/, task-guard WARNs on every source edit. This
	// is the real cost of "install once, everywhere" and must be stated up front — otherwise
	// users get noise in unrelated projects.
	// Backflow 4fad92e hand-maintained init-suggest Caveat details (non-MCP: handling path
	// for plugin user-level hooks in projects without .forge/ — init-suggest detection +
	// suggest decline/reset to silence).
	//
	// Caveat — 用户级 hooks 在每个 Claude Code 项目触发，包括用户不想用 forge 的项目。
	// 无 .forge/ 时 task-guard 每次源码编辑 WARN。这是"install once, everywhere"的真实代价，
	// 必须前置说明——否则用户会在无关项目被噪声困扰。
	// 回流 4fad92e 手维护的 init-suggest Caveat 详情（非 MCP：plugin 用户级 hooks 在无 .forge/
	// 项目的处理路径——init-suggest 检测 + suggest decline/reset 静默）。
	sb.WriteString("## Caveat: projects you do not want forge in\n\n")
	sb.WriteString("User-level hooks fire in every Claude Code project. In git projects " +
		"without \x60.forge/\x60, the **init-suggest** SessionStart hook detects this and " +
		"prompts the agent to ask the user whether to run \x60forge init\x60 (one-shot " +
		"\x60suggested\x60 marker so it asks only once). To permanently silence the prompt " +
		"for a specific project, run \x60forge suggest decline\x60 there. To remove forge " +
		"entirely from a project, \x60forge init --reset\x60 (clean) or uninstall the plugin.\n\n")

	// Backflow 4fad92e hand-maintained Supported hosts table (MCP removed: full hooks +
	// MCP → full hooks).
	//
	// 回流 4fad92e 手维护的 Supported hosts 表（清 MCP：full hooks + MCP → full hooks）。
	sb.WriteString("## Supported hosts (out of the box)\n\n")
	sb.WriteString("| Host | Plugin install | Per-project gate wiring | Notes |\n")
	sb.WriteString("|------|----------------|------------------------|-------|\n")
	sb.WriteString("| **Claude Code** | \x60plugin.json\x60 marketplace | automatic (user-level) | full hooks; auto-init via \x60init-suggest\x60 SessionStart hook |\n")
	sb.WriteString("| **Codex (CLI / App)** | marketplace (path not officially confirmed) | \x60forge init --agents codex\x60 | if marketplace path fails, fall back to manual |\n")
	sb.WriteString("| **Cursor** | marketplace | \x60forge init --agents cursor\x60 | Cursor plugin model carries skills, not Claude-shape hooks |\n")
	sb.WriteString("| **GitHub Copilot (CLI / VS Code)** | marketplace | \x60forge init --agents copilot\x60 (CLI) | VS Code side is guidance-only via the generated \x60.github/instructions/forge-quality.instructions.md\x60 |\n")
	sb.WriteString("| **Windsurf** | (mirrored \x60buildWindsurfHooks\x60 in code) | (Cascade hooks) | mirrors Claude SessionStart + write hooks via \x60internal/agentbridge/windsurf.go\x60 |\n")
	sb.WriteString("| **Kimi Code** | repo-root \x60.kimi-plugin/plugin.json\x60 (\x60/plugins install https://github.com/MjxUpUp/Forge\x60) | automatic (user-level) | full event set (PreToolUse/PostToolUse/Stop/SessionStart/PostCompact/UserPromptSubmit), exit-2 block protocol; fallback \x60forge init --agents kimi\x60 (config.toml marker section, stripped when the plugin is installed) |\n")
	sb.WriteString("| **OpenCode / Kiro / Cline / Gemini CLI / Mistral Vibe / Trae / Nanobot / Hermes / Antigravity / OpenClaw** | (manual, see \x60install.sh\x60) | \x60forge init --agents <host>\x60 if supported | install.sh script provides one-step symlink-style per-skill/folder install for 14 hosts |\n\n")
	sb.WriteString("For experimental / bleeding-edge hosts, run \x60./plugins/forge/install.sh --help\x60 for the full supported platform list.\n\n")

	// Backflow 4fad92e hand-maintained Distribution model (hooks/MCP removed → hooks).
	//
	// 回流 4fad92e 手维护的 Distribution model（清 hooks/MCP → hooks）。
	sb.WriteString("## Distribution model\n\n")
	sb.WriteString("Forge ships as an npm binary (\x60@agent_forge/forge\x60) plus a marketplace plugin (this directory). All supported agent hosts use the same single marketplace install command — there is no per-skill vs folder symlink split because plugin marketplaces already give a unified delivery surface. This contrasts with single-skill tools (whose 14-host \x60install.sh\x60 uses per-skill/folder symlinks as the actual installation primitive).\n\n")
	sb.WriteString("When this model stops being sufficient (e.g. agents whose marketplace can not resolve \x60hooks\x60), \x60forge plugin pack --agent <host>\x60 lets us generate host-specific packs; until then, one marketplace path serves all supported agents.\n\n")

	// Backflow 4fad92e hand-maintained local-debug guidance (non-MCP: workaround for Claude
	// plugin cache not following symlinks).
	//
	// 回流 4fad92e 手维护的本地调试指引（非 MCP：Claude plugin cache 不跟 symlink 的 workaround）。
	sb.WriteString("## Developing locally (cache copy, not symlinks)\n\n")
	sb.WriteString("Claude Code plugin cache (\x60~/.claude/plugins/cache/forge/forge/<version>/\x60) does **not** follow symlinks — \x60Search\x60/\x60Glob\x60 tools in the agent skip symlinked dirs. The plugin manifest deliberately omits \x60version\x60 (git SHA drives updates), so do NOT try to read it with \x60jq -r .version\x60 (yields \x60null\x60) — locate the cache dir by listing it (usually a single entry named after the git SHA). To test local plugin changes:\n\n")
	sb.WriteString("1. Rebuild after changes: \x60go build ./...\x60\n")
	sb.WriteString("2. Locate the cache dir by listing: \x60ls ~/.claude/plugins/cache/forge/forge/\x60\n")
	sb.WriteString("3. Replace its contents with the freshly-built assets:\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("CACHE_DIR=$(ls -d \"$HOME\"/.claude/plugins/cache/forge/forge/*/ | head -1)\n")
	sb.WriteString("rm -rf \"$CACHE_DIR\"\n")
	sb.WriteString("mkdir -p \"$CACHE_DIR\"\n")
	sb.WriteString("cp -R plugins/forge/* \"$CACHE_DIR\"\n")
	sb.WriteString("```\n\n")
	sb.WriteString("4. Start a fresh Claude Code session (existing sessions keep old prompts in context).\n")
	sb.WriteString("5. Verify by opening any git project — the \x60init-suggest\x60 SessionStart hook should fire.\n\n")
	sb.WriteString("Rationale: Claude Search/Glob tools can not follow symlinks, so the cache copy above replaces rather than links.\n")
	return sb.String()
}

// writeJSONIndent writes JSON to path with 2-space indent (auto-creates parent dirs).
// All plugin pack files go through this helper to keep format consistent (golden tests
// depend on this indent).
//
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
