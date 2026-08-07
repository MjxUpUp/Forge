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
//	  reasonix-plugin.json            reasonix NATIVE plugin manifest (apiVersion reasonix.io/plugin/v1):
//	                                  hooks field = buildReasonixHooks flat {match,command}, so
//	                                  `reasonix plugin install <name>` gets identical gate wiring.
//	                                  reasonix's Claude compat does NOT resolve .claude-plugin/plugin.json
//	                                  hooks, so this native manifest is required; reasonix prefers it
//	                                  when both are present (no cross-contamination).
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
//	  reasonix-plugin.json            reasonix NATIVE plugin manifest（apiVersion reasonix.io/plugin/v1）：
//	                                  hooks 字段 = buildReasonixHooks 扁平 {match,command}，让
//	                                  `reasonix plugin install <name>` 获得相同的 gate 接线。
//	                                  reasonix 的 Claude 兼容不解析 .claude-plugin/plugin.json 的
//	                                  hooks，故此 native manifest 必需；两者并存时 reasonix 优先
//	                                  native（互不污染）。
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
	RepoDir string // 仓库根：marketplaces + plugins/ 写入此目录
	// github owner/repo for install commands, e.g. MjxUpUp/Forge.
	//
	// github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	RepoSlug string // github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	// Marketplace identifier, e.g. forge.
	//
	// marketplace 标识，如 "forge"
	MarketplaceName string // marketplace 标识，如 "forge"
	// Plugin identifier, e.g. forge.
	//
	// plugin 标识，如 "forge"
	PluginName  string // plugin 标识，如 "forge"
	Description string
	// required (schema); the name of the marketplace owner + plugin author.
	//
	// required（schema）；marketplace owner + plugin author 的 name
	OwnerName string // required（schema）；marketplace owner + plugin author 的 name
	// optional; the email of the marketplace owner + plugin author.
	//
	// optional；marketplace owner + plugin author 的 email
	OwnerEmail string // optional；marketplace owner + plugin author 的 email
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
	if err := writeReasonixPluginManifest(spec, pluginDir); err != nil {
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

// writeReasonixPluginManifest writes plugins/<name>/reasonix-plugin.json — reasonix's NATIVE
// plugin manifest (apiVersion reasonix.io/plugin/v1). reasonix's Claude compatibility does NOT
// resolve the hooks field of .claude-plugin/plugin.json (empirically rejected with "no Reasonix-
// compatible capabilities", kinds all 0), so a native manifest alongside the claude one is
// required for reasonix. reasonix PREFERS the native manifest when both are present (confirmed:
// manifestKind "reasonix", compatibility full, mappedCapabilities ["hooks"]), so the two coexist
// in the same plugin dir — claude reads .claude-plugin/plugin.json, reasonix reads
// reasonix-plugin.json, no cross-contamination. The native schema is flat {match, command} per
// event (NOT claude's nested {matcher, hooks:[{type,command}]} — reasonix rejects matcher/type/
// nested hooks fields), which is exactly the shape buildReasonixHooks produces for settings.json.
// Reusing it keeps `reasonix plugin install` gate wiring identical to `forge init --agents
// reasonix` — single source of truth. TestPluginPack_ReasonixManifestHooksMirror guards this.
//
// version is a static display string ("1.0.0"): reasonix requires the field (the native manifest
// struct models it non-omitempty), and the pack generator takes no version input (claude omits
// version for SHA-driven updates, but reasonix's native format wants it present). It is plugin
// display metadata, decoupled from forge's own release version.
//
// writeReasonixPluginManifest 写 plugins/<name>/reasonix-plugin.json——reasonix 的 NATIVE
// plugin manifest（apiVersion reasonix.io/plugin/v1）。reasonix 的 Claude 兼容不解析
// .claude-plugin/plugin.json 的 hooks 字段（实测被判 "no Reasonix-compatible capabilities"、
// kinds 全 0），故 reasonix 需在 claude manifest 旁加一份 native manifest。两者并存时
// reasonix 优先 native（已确认：manifestKind "reasonix"、compatibility full、
// mappedCapabilities ["hooks"]），故两份 manifest 共处同一 plugin 目录——claude 读
// .claude-plugin/plugin.json，reasonix 读 reasonix-plugin.json，互不污染。native schema 是
// 每 event 扁平 {match, command}（非 claude 的嵌套 {matcher, hooks:[{type,command}]}——
// reasonix 拒绝 matcher/type/嵌套 hooks 字段），正是 buildReasonixHooks 为 settings.json
// 产出的形态。复用它使 `reasonix plugin install` 的 gate 接线与 `forge init --agents
// reasonix` 一致——单一真相源。TestPluginPack_ReasonixManifestHooksMirror 守卫此点。
//
// version 是静态展示串（"1.0.0"）：reasonix 要求该字段（native manifest 结构体把它建模为
// 非 omitempty），而 pack 生成器不接收 version 输入（claude 省略 version 走 SHA 自动更新，
// 但 reasonix 的 native 格式要求 version 在场）。它是 plugin 展示元数据，与 forge 自身发布
// 版本解耦。
func writeReasonixPluginManifest(spec PluginPackSpec, pluginDir string) error {
	manifest := map[string]any{
		"apiVersion":  "reasonix.io/plugin/v1",
		"name":        spec.PluginName,
		"version":     "1.0.0",
		"description": spec.Description,
		"hooks":       buildReasonixHooks()["hooks"],
	}
	return writeJSONIndent(filepath.Join(pluginDir, "reasonix-plugin.json"), manifest)
}

func writePluginReadme(spec PluginPackSpec, pluginDir string) error {
	slug := spec.RepoSlug
	if slug == "" {
		slug = "MjxUpUp/Forge"
	}
	return os.WriteFile(filepath.Join(pluginDir, "README.md"), []byte(pluginReadme(slug)), 0644)
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
