package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// kimi_plugin.go — kimi-code plugin manifest (.kimi-plugin/plugin.json) derivation.
//
// kimi-code's plugin system (kimi.plugin.json or .kimi-plugin/plugin.json at the plugin
// root) natively supports a `hooks` array whose entries are field-identical to the
// [[hooks]] rules in config.toml (event/matcher/command/timeout). Installing from GitHub
// (`/plugins install https://github.com/MjxUpUp/Forge`) reads the manifest from the repo
// root — so the manifest must be COMMITTED, unlike the claude/cursor marketplace packs
// which are generated on demand by `forge plugin pack`. Drift between the committed
// manifest and ForgeHookSpec is guarded by TestKimiPluginManifestMirrorsSpec.
//
// Plugin vs config.toml wiring (KimiTranslator): both register the same hooks user-level
// and machine-wide — running both means every hook fires twice. Translate therefore
// strips the config.toml marker section when the plugin is installed (same dedupe
// philosophy as claude-code's plugin vs settings.local.json, see
// internal/hooks/plugin_detect.go).
//
// kimi_plugin.go — kimi-code plugin manifest（.kimi-plugin/plugin.json）派生。
//
// kimi-code 的 plugin 系统（plugin 根的 kimi.plugin.json 或 .kimi-plugin/plugin.json）
// 原生支持 `hooks` 数组，条目与 config.toml 的 [[hooks]] 规则字段一致
// （event/matcher/command/timeout）。从 GitHub 安装
// （`/plugins install https://github.com/MjxUpUp/Forge`）读仓库根的 manifest——故
// manifest 必须提交进库，与 `forge plugin pack` 按需生成的 claude/cursor marketplace
// pack 不同。已提交 manifest 与 ForgeHookSpec 的 drift 由
// TestKimiPluginManifestMirrorsSpec 守卫。
//
// Plugin 与 config.toml 接线（KimiTranslator）：两者都在 user-level 全机器注册同一批
// hooks——同时存在则每个 hook 双跑。故 plugin 已装时 Translate 会剥除 config.toml
// 标记段（与 claude-code 的 plugin vs settings.local.json 同款 dedupe 哲学，见
// internal/hooks/plugin_detect.go）。

// KimiPluginHook is one entry of the plugin manifest's hooks array — field-identical to
// a [[hooks]] rule in kimi's config.toml.
//
// KimiPluginHook 是 plugin manifest hooks 数组的一条——与 kimi config.toml 的
// [[hooks]] 规则字段一致。
type KimiPluginHook struct {
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// KimiPluginManifest is the .kimi-plugin/plugin.json schema subset forge ships. Only the
// fields forge uses are modeled; unknown fields are neither needed nor emitted.
//
// KimiPluginManifest 是 forge 发布的 .kimi-plugin/plugin.json schema 子集。只建模
// forge 用到的字段；不需要的字段不建模也不输出。
type KimiPluginManifest struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Hooks       []KimiPluginHook `json:"hooks"`
}

// kimiPluginName is the plugin id. It must stay "forge": the dedupe detection keys on it,
// and it becomes the slash-command namespace.
//
// kimiPluginName 是 plugin id。必须保持 "forge"：dedupe 检测以它为 key，它也是
// 斜杠命令的命名空间。
const kimiPluginName = "forge"

// BuildKimiPluginHooks derives the manifest's hooks array from hooks.ForgeHookSpec —
// the same single source of truth as BuildKimiHooksTOML (config.toml path). Entries are
// sorted by event for deterministic output; commands carry `--agent kimi` for the same
// reason as the config.toml path (stdin dialect + exit-2 output protocol).
//
// BuildKimiPluginHooks 从 hooks.ForgeHookSpec 派生 manifest 的 hooks 数组——与
// BuildKimiHooksTOML（config.toml 路径）共享同一单一真相源。条目按 event 排序保证
// 输出确定；command 带 `--agent kimi`，理由与 config.toml 路径相同（stdin 方言 +
// exit-2 输出协议）。
func BuildKimiPluginHooks() []KimiPluginHook {
	spec := hooks.ForgeHookSpec()
	events := make([]string, 0, len(spec))
	for ev := range spec {
		events = append(events, ev)
	}
	sort.Strings(events)

	var out []KimiPluginHook
	for _, ev := range events {
		for _, m := range spec[ev] {
			for _, entry := range m.Hooks {
				// kimi 0.35.0 drops allow-path stdout from the model context for every event
				// except UserPromptSubmit (verified via wire.jsonl). skill-trigger is advisory —
				// running it on PreToolUse/PostToolUse/Stop/SessionStart would (a) never reach the
				// model and (b) record a checklog entry (recordSkillTriggerHits) that misleads
				// `forge skills usage` into reporting a delivered trigger the model never saw. So
				// the kimi manifest binds skill-trigger ONLY to UserPromptSubmit. The config.toml
				// path (BuildKimiHooksTOML) is deliberately NOT filtered — it serves the
				// no-plugin-installed case and keeps full spec parity (guarded by
				// TestKimiWiringMirrorsClaudeSettings); runSkillTriggerHook's defensive guard
				// neutralizes stale installed manifests either way.
				if ev != "UserPromptSubmit" && isSkillTriggerCommand(entry.Command) {
					continue
				}
				out = append(out, KimiPluginHook{
					Event:   ev,
					Matcher: m.Matcher,
					Command: kimiCommand(entry.Command),
					Timeout: kimiTimeout(entry.Command),
				})
			}
		}
	}
	return out
}

// isSkillTriggerCommand reports whether a hook command is the skill-trigger hook. It is
// dual-purpose: BuildKimiPluginHooks calls it on the RAW spec command ("forge hook
// skill-trigger", bare) and the manifest guard test calls it on the agent-TRANSLATED manifest
// command ("forge hook skill-trigger --agent kimi"). A word boundary after the token (end of
// string or a space) accepts both forms while rejecting a hypothetical future
// "forge hook skill-trigger-v2" that a plain Contains would false-match.
//
// isSkillTriggerCommand 报告某 hook command 是否为 skill-trigger hook。它是双用途：
// BuildKimiPluginHooks 对 RAW spec 命令（裸 "forge hook skill-trigger"）调用，manifest 守卫
// 测试对 agent 翻译后的 manifest 命令（"forge hook skill-trigger --agent kimi"）调用。token
// 后的字边界（字符串尾或空格）两种形式都接受，同时拒绝纯 Contains 会误匹配的假设性未来
// "forge hook skill-trigger-v2"。
func isSkillTriggerCommand(cmd string) bool {
	const token = "forge hook skill-trigger"
	i := strings.Index(cmd, token)
	if i < 0 {
		return false
	}
	rest := cmd[i+len(token):]
	return rest == "" || strings.HasPrefix(rest, " ")
}

// BuildKimiPluginManifest renders the full manifest. version is the plugin's display
// version, now tracked to the forge release (scripts/release.js syncs it; pinned by
// TestKimiPluginManifestVersionTracksRelease). The caller supplies it — tests read it
// from npm/package.json, the single source of truth.
//
// Maintenance path (2026-08-16 audit note): this builder trio has no CLI regen outlet —
// production never calls it; the committed .kimi-plugin/plugin.json is the artifact kimi
// installs. When ForgeHookSpec changes, hand-sync the committed manifest against
// BuildKimiPluginHooks output (TestKimiPluginManifestMirrorsSpec goes red on drift, so a
// spec change cannot land silently); version bumps are text-replaced by
// scripts/release.js. If hand-syncing gets tedious, a `forge plugin kimi-manifest --write`
// subcommand wrapping MarshalKimiPluginManifest is the natural follow-up.
//
// BuildKimiPluginManifest 渲染完整 manifest。version 是 plugin 的展示版本，现跟随
// forge release（scripts/release.js 同步；由 TestKimiPluginManifestVersionTracksRelease
// 钉住）。由调用方传入——测试从单一真相源 npm/package.json 读。
//
// 维护路径（2026-08-16 审计注记）：本 Build 三件套没有 CLI 再生成出口——生产不调用
// 它；committed .kimi-plugin/plugin.json 才是 kimi 安装的产物。ForgeHookSpec 变更时，
// 手工把 committed manifest 与 BuildKimiPluginHooks 输出对齐（漂移会让
// TestKimiPluginManifestMirrorsSpec 变红，故 spec 变更不可能静默落地）；version 由
// scripts/release.js 发版时文本替换。若手工对齐变繁琐，包一层
// `forge plugin kimi-manifest --write` 子命令是自然的后续。
func BuildKimiPluginManifest(version, description string) KimiPluginManifest {
	return KimiPluginManifest{
		Name:        kimiPluginName,
		Version:     version,
		Description: description,
		Hooks:       BuildKimiPluginHooks(),
	}
}

// MarshalKimiPluginManifest serializes the manifest in the committed file's canonical
// form (2-space indent, trailing newline) so the guard test can byte-compare.
//
// MarshalKimiPluginManifest 按提交文件的规范形式序列化 manifest（2 空格缩进 + 末尾
// 换行），供守卫测试做字节级比对。
func MarshalKimiPluginManifest(m KimiPluginManifest) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// IsKimiPluginInstalled reports whether the forge plugin is installed and enabled in
// kimi-code (record present in $KIMI_CODE_HOME/plugins/installed.json). kimi's
// /plugins install/remove is TUI-only, so the on-disk record is the only signal a CLI
// can read. The parse is deliberately tolerant: the exact record schema is not
// documented (kimi-code 0.31.0), so any entry whose id (or name) is "forge" counts,
// and enablement defaults to true unless explicitly disabled. The trade-off this
// accepts: an unrelated third-party plugin also named "forge" (id collision, no source
// check — checking the source would punish forks) would make Translate strip the
// config.toml section without that plugin registering forge hooks; judged improbable
// enough to stay a tolerant read rather than a strict one.
//
// The managed copy under plugins/managed/forge/ is NOT a signal: /plugins remove keeps
// it on disk after uninstalling.
//
// IsKimiPluginInstalled 报告 forge plugin 是否已在 kimi-code 安装并启用
// （$KIMI_CODE_HOME/plugins/installed.json 中有记录）。kimi 的
// /plugins install/remove 只能在 TUI 里跑，磁盘记录是 CLI 唯一可读信号。解析刻意
// 宽容：记录 schema 无文档（kimi-code 0.31.0），凡 id（或 name）为 "forge" 的条目
// 即算数，启用状态默认 true 除非显式禁用。此设计接受的权衡：同名 "forge" 的无关
// 第三方插件（id 碰撞，不校验 source——校验会误伤 fork 安装）会让 Translate 剥除
// config.toml 标记段而该插件并不注册 forge hooks；概率足够低，故保持宽容读而非
// 严格校验。
//
// plugins/managed/forge/ 的托管副本不是信号：/plugins remove 卸载后它仍留在磁盘上。
func IsKimiPluginInstalled() bool {
	home, err := KimiConfigHome()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, "plugins", "installed.json"))
	if err != nil {
		return false
	}
	var reg struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return false
	}
	for _, p := range reg.Plugins {
		id, _ := p["id"].(string)
		name, _ := p["name"].(string)
		if id != kimiPluginName && name != kimiPluginName {
			continue
		}
		if enabled, ok := p["enabled"].(bool); ok && !enabled {
			continue
		}
		if disabled, ok := p["disabled"].(bool); ok && disabled {
			continue
		}
		return true
	}
	return false
}

// KimiPluginStaleInfo reads the installed forge plugin's source ref tag and returns the
// bare version (v prefix trimmed). It is the trustworthy "which version is installed"
// signal for staleness detection: the manifest's version field is committed metadata
// (now release-tracked via scripts/release.js, but only refreshed on reinstall) and the managed
// copy under plugins/managed/forge/ survives uninstall (see IsKimiPluginInstalled notes) —
// only installed.json's github.ref records the tag the user actually installed from.
//
// ok is true only when a forge entry exists, is enabled, and carries github.ref.kind=="tag"
// with a non-empty value. Non-tag refs (commit/branch) return ok=false — they cannot be
// semver-compared, and flagging them as stale would be noise.
//
// KimiPluginStaleInfo 读取已装 forge plugin 的来源 ref tag，返回裸版本号（已 trim v 前缀）。
// 它是 staleness 检测的可信"装了哪个版本"信号：manifest 的 version 字段是 committed 元数据
// （现经 scripts/release.js 跟随 release，但只在重装时刷新），而 plugins/managed/forge/ 下的托管
// 副本在卸载后仍留存（见 IsKimiPluginInstalled 注释）——只有 installed.json 的 github.ref
// 记录了用户实际安装来源的 tag。
//
// 仅当存在 forge 条目、已启用、且带 github.ref.kind=="tag" 与非空 value 时 ok=true。
// 非 tag ref（commit/branch）返回 ok=false——它们无法 semver 比对，标为过期只会是噪声。
func KimiPluginStaleInfo() (installed string, ok bool) {
	home, err := KimiConfigHome()
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(home, "plugins", "installed.json"))
	if err != nil {
		return "", false
	}
	var reg struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return "", false
	}
	for _, p := range reg.Plugins {
		id, _ := p["id"].(string)
		name, _ := p["name"].(string)
		if id != kimiPluginName && name != kimiPluginName {
			continue
		}
		if enabled, ok := p["enabled"].(bool); ok && !enabled {
			continue
		}
		if disabled, ok := p["disabled"].(bool); ok && disabled {
			continue
		}
		github, _ := p["github"].(map[string]any)
		ref, _ := github["ref"].(map[string]any)
		kind, _ := ref["kind"].(string)
		if kind != "tag" {
			// continue rather than early return: if a future installed.json schema ever
			// carries multiple forge records, we want the first comparable tag ref, not a
			// dead end on a non-tag sibling. Single-record behavior is unchanged.
			//
			// continue 而非提前 return：若将来 installed.json schema 出现多条 forge 记录，
			// 我们要第一个可比对的 tag ref，而非在非 tag 兄弟条目上死锁。单条目行为不变。
			continue
		}
		value, _ := ref["value"].(string)
		if value == "" {
			continue
		}
		return strings.TrimPrefix(value, "v"), true
	}
	return "", false
}
