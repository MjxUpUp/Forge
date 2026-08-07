package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/MjxUpUp/Forge/internal/userassets"
)

// ReasonixTranslator wires forge at USER level into reasonix (DeepSeek-Reasonix) along
// TWO surfaces, because reasonix is Claude-Code-compatible and ships a full lifecycle-hook
// system — not just a skill loader:
//
//  1. Advisory skill — <reasonix home>/skills/forge-quality/SKILL.md, reasonix's native
//     skill mechanism (SKILL.md files are loaded into the session skill index).
//  2. Enforcement hooks — <reasonix home>/settings.json, a FLAT hooks schema
//     { "hooks": { "<Event>": [ { "match": "<matcher>", "command": "forge hook <name>" } ] } }
//     derived from hooks.ForgeHookSpec. This is the surface that actually enforces the
//     quality protocol (task-guard / bash-guard / file-sentinel / read-before-edit /
//     assertion-check): without it the protocol reaches reasonix only as advisory text
//     (AGENTS.md) and compliance is inconsistent.
//
// reasonix's settings.json schema is flatter than Claude Code's nested
// {matcher, hooks:[{type,command}]}: the field is `match` (not `matcher`) and the command
// sits directly on the entry (no `type` wrapper). The stdin/exit-code protocol is
// Claude-Code-compatible, so the bare `forge hook <name>` commands run as-is and forge's
// existing CC-shape block output ({"decision":"block",...} + exit 1) is honored unchanged —
// no --agent reasonix flag is needed (it would be a no-op that correctly collapses to the
// CC-shape path).
//
// No-op when the reasonix home does not exist — Forge never creates an agent's config home
// (the detection self-poison guard: materializing the home on a machine without reasonix
// would be wrong, and reasonix's home is not an auto-detect signal anyway — only the
// project-level .reasonix/ dir is). Both writes are skipped when the home is missing.
//
// Merge semantics for settings.json: unknown top-level fields are preserved via
// json.RawMessage; within the flat hooks section, entries whose command is not forge-sourced
// are kept byte-for-byte (unknown entry fields intact — see merge_raw.go), and forge entries
// are replaced wholesale with the current generated set, making Translate idempotent. The
// first write is backed up (userassets.BackupOriginal) as a rollback anchor for
// `forge uninstall --restore`, same contract as claude-code's user-level settings.json.
//
// Project-level .reasonix/ assets are never written: the default zero-project-write model
// keeps the project dir clean, and reasonix's global settings/skills are visible in every
// project anyway.
//
// ReasonixTranslator 在用户级接线 reasonix（DeepSeek-Reasonix），覆盖两个面——因为
// reasonix 与 Claude Code 兼容、内置完整 lifecycle-hook 系统，而非仅 skill loader：
//
//  1. advisory skill——<reasonix home>/skills/forge-quality/SKILL.md，reasonix 的原生
//     skill 机制（SKILL.md 文件会被加载进会话 skill 索引）。
//  2. enforcement hooks——<reasonix home>/settings.json，扁平 hooks schema
//     { "hooks": { "<Event>": [ { "match": "<matcher>", "command": "forge hook <name>" } ] } }，
//     由 hooks.ForgeHookSpec 派生。这才是真正 enforce 质量协议的面
//     （task-guard / bash-guard / file-sentinel / read-before-edit / assertion-check）：
//     缺了它，协议只以 advisory 文本（AGENTS.md）到达 reasonix，合规不一致。
//
// reasonix 的 settings.json schema 比 Claude Code 的嵌套
// {matcher, hooks:[{type,command}]} 更扁平：字段是 `match`（非 `matcher`），command 直接
// 挂在条目上（无 `type` 包装）。stdin/exit-code 协议与 Claude Code 兼容，故裸
// `forge hook <name>` 命令原样跑，forge 既有 CC-shape block 输出
// （{"decision":"block",...} + exit 1）原样被尊重——无需 --agent reasonix 标志
// （它是 no-op，会正确归并到 CC-shape 路径）。
//
// reasonix home 不存在时 no-op——Forge 绝不创建 agent 的配置 home（检测自毒防线：在
// 没装 reasonix 的机器上具象化其 home 是错的，况且 reasonix 的 home 本就不是 auto-detect
// 信号——只有项目级 .reasonix/ 目录才是）。home 缺失时两处写入都跳过。
//
// settings.json 的 merge 语义：未知顶层字段经 json.RawMessage 保留；扁平 hooks 段内，
// command 非 forge 来源的条目逐字节保留（未知条目字段不丢——见 merge_raw.go），forge 条目
// 整体替换为当前生成集，故 Translate 幂等。首次写入经 userassets.BackupOriginal 备份，
// 作为 `forge uninstall --restore` 的回滚锚点，与 claude-code 用户级 settings.json 同契约。
//
// 从不写项目级 .reasonix/ 资产：默认零项目写入模型保持项目目录干净，且 reasonix 的全局
// settings/skills 本就对所有项目可见。
type ReasonixTranslator struct{}

func (t *ReasonixTranslator) AgentType() AgentType {
	return AgentReasonix
}

func (t *ReasonixTranslator) Translate(projectDir string, input *TranslationInput) error {
	// User-level translator: projectDir is intentionally ignored — the registration is
	// machine-wide (same contract as CursorTranslator/KimiTranslator).
	//
	// 用户级 translator：刻意忽略 projectDir——注册是全机器生效（与
	// CursorTranslator/KimiTranslator 同契约）。
	if input.Protocol == nil {
		return nil // nothing to render without a protocol (mirrors claude-code)
	}
	home, err := ReasonixConfigHome()
	if err != nil {
		return fmt.Errorf("reasonix: %w", err)
	}
	// Detection self-poison guard: Forge must not create reasonix's config home, so both
	// writes are skipped when the home is missing. Unlike claude-code (which MkdirAlls the
	// home when its env is explicitly set), reasonix's home is never auto-created here — the
	// guard is unconditional. Covers both the skill write and the settings.json write; the
	// skill writer has the same check internally.
	//
	// 检测自毒防线：Forge 不创建 reasonix 的配置 home，故 home 缺失时两处写入都跳过。
	// 与 claude-code（env 显式设置时 MkdirAll home）不同，reasonix 的 home 这里绝不自动
	// 创建——防线是无条件的。同时覆盖 skill 写入与 settings.json 写入；skill writer 内部
	// 有同样检查。
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return nil
	}
	// 1. Advisory skill — reasonix's native skill mechanism.
	//
	// 1. advisory skill——reasonix 的原生 skill 机制。
	if err := skillgen.GenerateUserQualitySkillTo(filepath.Join(home, "skills"), input.Protocol); err != nil {
		return fmt.Errorf("reasonix: %w", err)
	}
	// Plugin wins (kimi-style dedupe): when forge is installed as a reasonix plugin
	// (`reasonix plugin install`), the plugin's reasonix-plugin.json manifest already
	// registers every hook at the user level — merging into settings.json would double-run
	// every hook (same dedupe philosophy as kimi.go's config.toml path and claude-code's
	// plugin vs settings.local.json, internal/hooks/plugin_detect.go). The skill above is
	// still written (the plugin pack ships no skill — writeReasonixPluginManifest emits only
	// the hooks manifest), so this strip happens AFTER the skill write. Then we stop: plugin
	// wins, no double-run.
	//
	// Plugin 优先（kimi 式 dedupe）：forge 已作为 reasonix plugin 安装
	// （`reasonix plugin install`）时，plugin 的 reasonix-plugin.json manifest 已在 user
	// level 注册全部 hook——再合并进 settings.json 会让每个 hook 双跑（与 kimi.go 的
	// config.toml 路径、claude-code 的 plugin vs settings.local.json 同款 dedupe 哲学，见
	// internal/hooks/plugin_detect.go）。上面的 skill 仍写入（plugin pack 不附 skill——
	// writeReasonixPluginManifest 只输出 hooks manifest），故此 strip 发生在 skill 写入之后。
	// 然后返回：plugin 优先，不双跑。
	if IsReasonixPluginInstalled() {
		if _, err := StripReasonixHooksUserLevel(); err != nil {
			return fmt.Errorf("reasonix: %w", err)
		}
		return nil
	}
	// 2. Enforcement hooks — settings.json, flat schema derived from ForgeHookSpec. Backup
	// first so `forge uninstall --restore` can roll back (reasonix's settings.json may hold
	// user content beyond hooks, same reason claude-code backs up its user-level settings).
	//
	// 2. enforcement hooks——settings.json，由 ForgeHookSpec 派生的扁平 schema。先备份，
	// 供 `forge uninstall --restore` 回滚（reasonix 的 settings.json 可能含 hooks 之外的
	// 用户内容，与 claude-code 备份其用户级 settings 同理）。
	settingsPath := filepath.Join(home, "settings.json")
	if err := userassets.BackupOriginal(settingsPath); err != nil {
		return fmt.Errorf("reasonix: backup settings.json: %w", err)
	}
	if err := mergeReasonixHooks(settingsPath); err != nil {
		return fmt.Errorf("reasonix: %w", err)
	}
	return nil
}

// ReasonixConfigHome resolves reasonix's config home: $REASONIX_HOME when set, otherwise the
// OS user-config dir + "reasonix" — %APPDATA%\reasonix on Windows, ~/.config/reasonix on
// Linux, ~/Library/Application Support/reasonix on macOS (os.UserConfigDir convention).
// reasonix reads settings.json from this location (its binary changelog: "Global hooks in
// settings.json are now migrated to the new config home"), so wiring MUST write here or the
// hooks never load. Env override doubles as test isolation (same pattern as CODEX_HOME /
// KIMI_CODE_HOME).
//
// ReasonixConfigHome 解析 reasonix 的配置 home：设了 $REASONIX_HOME 用它，否则 OS 用户配置
// 目录 + "reasonix"——Windows 上 %APPDATA%\reasonix、Linux 上 ~/.config/reasonix、macOS 上
// ~/Library/Application Support/reasonix（os.UserConfigDir 约定）。reasonix 从此位置读
// settings.json（其二进制 changelog："Global hooks in settings.json are now migrated to the
// new config home"），故接线必须写到这里，否则 hooks 永不加载。env 覆盖同时充当测试隔离
// （与 CODEX_HOME / KIMI_CODE_HOME 同模式）。
func ReasonixConfigHome() (string, error) {
	if h := os.Getenv("REASONIX_HOME"); h != "" {
		return h, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "reasonix"), nil
}

// reasonixHookEntry is reasonix's flat hook-entry shape: {match, command}. Mirrors
// cursorHookEntry but with `match` (reasonix's field name, not cursor's `matcher`) and no
// type/timeout — reasonix's schema is flatter than Claude Code's nested
// {matcher, hooks:[{type,command}]}. match is omitempty so session-level events (SessionStart,
// Stop — no matcher) serialize without the key.
//
// reasonixHookEntry 是 reasonix 的扁平 hook 条目形态：{match, command}。镜像
// cursorHookEntry，但字段是 `match`（reasonix 的字段名，非 cursor 的 `matcher`），且无
// type/timeout——reasonix 的 schema 比 Claude Code 的嵌套 {matcher, hooks:[{type,command}]}
// 更扁平。match 用 omitempty，使会话级 event（SessionStart、Stop——无 matcher）序列化时不
// 带该键。
type reasonixHookEntry struct {
	Match   string `json:"match,omitempty"`
	Command string `json:"command"`
}

// reasonixEventName whitelists the Claude-Code PascalCase events reasonix's hook system
// accepts. reasonix is CC-compatible and uses the event names verbatim (confirmed: a
// settings.json with "PreToolUse" produces hook status active), so this is a filter, not a
// remap. Events outside the whitelist return ok=false so buildReasonixHooks skips them —
// reasonix may reject the WHOLE file on an unknown event (as it rejected the CC double-nested
// form), so the set is intentionally conservative: the four classic lifecycle events that
// carry all hard enforcement (PreToolUse/PostToolUse/Stop/SessionStart). PostCompact and
// UserPromptSubmit (compact-resume / resume-reinject — re-injection conveniences, not
// enforcement) are omitted until empirically confirmed supported; expand after probing each
// via `reasonix hook status --json` (see plan verification).
//
// reasonixEventName 白名单化 reasonix hook 系统接受的 Claude-Code PascalCase event。
// reasonix 与 CC 兼容、原样使用 event 名（已确认：带 "PreToolUse" 的 settings.json 产出
// hook status active），故这是过滤而非重映射。白名单外的 event 返回 ok=false，供
// buildReasonixHooks 跳过——reasonix 遇未知 event 可能拒绝整个文件（正如它拒绝了 CC 的
// 双层嵌套形态），故集合刻意保守：承载全部硬强制的四个经典 lifecycle event
// （PreToolUse/PostToolUse/Stop/SessionStart）。PostCompact 与 UserPromptSubmit
// （compact-resume / resume-reinject——重注入便利，非强制）在经验确认支持前先略去；逐一
// 探测 `reasonix hook status --json` 后再扩（见 plan 验证）。
func reasonixEventName(event string) (string, bool) {
	switch event {
	case "PreToolUse", "PostToolUse", "Stop", "SessionStart":
		return event, true
	default:
		return "", false
	}
}

// buildReasonixHooks derives reasonix's flat settings.json hooks from hooks.ForgeHookSpec
// (single source of truth). Clones buildCursorHooks' flatten loop: iterate the spec, skip
// events failing the reasonix whitelist, and flatten each matcher's hook list to one entry per
// hook — carrying the matcher onto each entry as `match` and dropping the `type` wrapper
// (always "command"). No manual copy → no drift vs ForgeHookSpec.
//
// buildReasonixHooks 从 hooks.ForgeHookSpec（单一真相源）派生 reasonix 的扁平 settings.json
// hooks。克隆 buildCursorHooks 的扁平化循环：遍历 spec，跳过未过 reasonix 白名单的 event，
// 把每个 matcher 的 hook 列表扁平化为每 hook 一个条目——matcher 作为 `match` 带到每个条目，
// 丢掉 `type` 包装（恒为 "command"）。无手工副本 → 与 ForgeHookSpec 无 drift。
func buildReasonixHooks() map[string]any {
	spec := hooks.ForgeHookSpec()
	hooksMap := map[string][]reasonixHookEntry{}
	for event, matchers := range spec {
		re, ok := reasonixEventName(event)
		if !ok {
			continue
		}
		for _, m := range matchers {
			for _, h := range m.Hooks {
				hooksMap[re] = append(hooksMap[re], reasonixHookEntry{
					Match:   m.Matcher,
					Command: h.Command,
				})
			}
		}
	}
	return map[string]any{
		`hooks`: hooksMap,
	}
}

// mergeReasonixHooks merges the generated forge wiring into reasonix's settings.json at path.
// Mirrors mergeForgeHooksIntoSettings (path-based, RawMessage top-level preservation, backup
// handled by the caller) but for the FLAT hooks shape: stripForgeFlatEntriesRaw removes stale
// forge entries (kept user entries byte-verbatim), then the current buildReasonixHooks set is
// appended per event. Output is deterministic → Translate is idempotent. A missing file is
// created; a non-NotExist read/parse error is returned (never silently overwrite unreadable
// user config).
//
// mergeReasonixHooks 把生成的 forge 接线合并进 path 处的 reasonix settings.json。镜像
// mergeForgeHooksIntoSettings（基于 path、RawMessage 顶层保留、备份由调用方处理），但用于
// 扁平 hooks 形态：stripForgeFlatEntriesRaw 移除陈旧 forge 条目（保留的用户条目逐字节不动），
// 再按 event 追加当前 buildReasonixHooks 集。输出确定 → Translate 幂等。文件不存在则新建；
// 非 NotExist 的读/解析错误原样返回（绝不静默覆盖读不出的用户配置）。
func mergeReasonixHooks(path string) error {
	cfg := map[string]json.RawMessage{}
	if existing, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return fmt.Errorf("parse existing settings.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read settings.json: %w", err)
	}

	kept := map[string][]json.RawMessage{}
	if raw, ok := cfg["hooks"]; ok {
		var flat map[string][]json.RawMessage
		if err := json.Unmarshal(raw, &flat); err != nil {
			return fmt.Errorf("parse existing hooks section: %w", err)
		}
		kept, _ = stripForgeFlatEntriesRaw(flat)
	}
	generated, err := rawHooksSection(buildReasonixHooks()["hooks"])
	if err != nil {
		return fmt.Errorf("marshal generated hooks: %w", err)
	}
	for event, entries := range generated {
		kept[event] = append(kept[event], entries...)
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings.json: %w", err)
	}
	// Trailing newline matches StripReasonixHooksUserLevel (and the cursor convention) so a
	// merge→strip→merge cycle leaves the file byte-stable.
	//
	// 尾随换行对齐 StripReasonixHooksUserLevel（与 cursor 约定），使 merge→strip→merge
	// 循环后文件逐字节稳定。
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// StripReasonixHooksUserLevel removes forge hooks from reasonix's user-level settings.json
// (uninstall path). Clones StripCursorHooksUserLevel for the flat shape: user-defined entries
// (unknown fields intact, see merge_raw.go) and unknown top-level fields are preserved; the
// file itself is never deleted. Reports whether the file was actually modified; a missing file
// or a file without forge hooks is a clean no-op.
//
// StripReasonixHooksUserLevel 移除 reasonix 用户级 settings.json 中的 forge hooks（卸载路径）。
// 克隆 StripCursorHooksUserLevel 用于扁平形态：用户自定义条目（未知字段不丢，见
// merge_raw.go）与未知顶层字段保留；文件本身绝不删除。返回是否实际改动；文件不存在或无
// forge hooks 均为干净 no-op。
func StripReasonixHooksUserLevel() (bool, error) {
	home, err := ReasonixConfigHome()
	if err != nil {
		return false, err
	}
	path := filepath.Join(home, "settings.json")
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reasonix: read settings.json: %w", err)
	}
	cfg := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &cfg); err != nil {
		return false, fmt.Errorf("reasonix: parse settings.json: %w", err)
	}
	raw, ok := cfg["hooks"]
	if !ok {
		return false, nil
	}
	var flat map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &flat); err != nil {
		return false, fmt.Errorf("reasonix: parse hooks section: %w", err)
	}
	kept, removedAny := stripForgeFlatEntriesRaw(flat)
	if !removedAny {
		return false, nil
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return false, fmt.Errorf("reasonix: marshal stripped hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("reasonix: marshal settings.json: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("reasonix: write settings.json: %w", err)
	}
	return true, nil
}

// reasonixPluginName is the plugin id reasonix registers under. Must stay "forge": the
// plugin-wins detection keys on it, matching writeReasonixPluginManifest's manifest name.
//
// reasonixPluginName 是 reasonix 注册时用的 plugin id。必须保持 "forge"：plugin-wins 检测
// 以它为 key，与 writeReasonixPluginManifest 的 manifest name 一致。
const reasonixPluginName = "forge"

// IsReasonixPluginInstalled reports whether the forge plugin is installed (and active) in
// reasonix, by reading <reasonix home>/plugin-packages.json — the registry reasonix's
// `reasonix plugin install` writes (configPath surfaced by `--dry-run`). reasonix's
// plugin add/remove is also exposed via the `/plugins` in-app slash command, so the
// on-disk registry is the only signal a CLI can read (same situation as IsKimiPluginInstalled).
//
// The parse is deliberately TOLERANT and recursive: the exact record schema is not
// documented (reasonix pre-1.0), and the registry may be a top-level array, an object
// keyed by plugin name, an object with a `packages`/`plugins` array, or a nested tree.
// reasonixFindForge walks any such shape looking for a map whose `name` (the manifest
// field writeReasonixPluginManifest writes) is "forge"; an entry counts only when not
// explicitly disabled (enabled defaults true — mirrors IsKimiPluginInstalled). The
// trade-off this accepts: an unrelated third-party plugin also named "forge" (id
// collision, no source check — checking source would punish forks) would make Translate
// strip settings.json hooks without that plugin registering forge hooks; judged
// improbable enough to stay a tolerant read rather than a strict one (same call kimi
// made). A missing/unreadable/garbled registry is a clean false.
//
// IsReasonixPluginInstalled 报告 forge plugin 是否已在 reasonix 安装（且激活），读
// <reasonix home>/plugin-packages.json——reasonix 的 `reasonix plugin install` 写入的
// 注册表（`--dry-run` 暴露的 configPath）。reasonix 的 plugin add/remove 也经应用内
// `/plugins` 斜杠命令，故磁盘注册表是 CLI 唯一可读信号（与 IsKimiPluginInstalled 同境）。
//
// 解析刻意宽容且递归：记录 schema 无文档（reasonix 1.0 前），注册表可能是顶层数组、按
// plugin name 为 key 的对象、带 `packages`/`plugins` 数组的对象、或嵌套树。reasonixFindForge
// 遍历任意此类形态，找 `name`（writeReasonixPluginManifest 写入的 manifest 字段）为
// "forge" 的 map；条目仅在未显式禁用时算数（启用默认 true——镜像 IsKimiPluginInstalled）。
// 此设计接受的权衡：同名 "forge" 的无关第三方插件（id 碰撞，不校验 source——校验会误伤
// fork）会让 Translate 剥除 settings.json hooks 而该插件并不注册 forge hooks；概率足够低，
// 故保持宽容读而非严格校验（与 kimi 同判断）。缺失/读不出/损坏的注册表是干净 false。
func IsReasonixPluginInstalled() bool {
	home, err := ReasonixConfigHome()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, "plugin-packages.json"))
	if err != nil {
		return false
	}
	var reg any
	if err := json.Unmarshal(data, &reg); err != nil {
		return false
	}
	return reasonixFindForge(reg)
}

// reasonixFindForge walks an arbitrary decoded-JSON value looking for an active forge
// plugin entry (see IsReasonixPluginInstalled). A node that IS a forge entry is a leaf
// for the search (we never recurse into an entry's own children looking for more entries),
// so a disabled forge entry returns false from its own call without poisoning the search
// for an enabled sibling elsewhere in the tree.
//
// reasonixFindForge 在任意解码后的 JSON 值里找激活的 forge plugin 条目（见
// IsReasonixPluginInstalled）。本身是 forge 条目的节点是搜索的叶子（绝不递归进条目自身
// 的子树找更多条目），故被禁用的 forge 条目从其自身调用返回 false，不会毒化树中别处
// 激活兄弟条目的搜索。
func reasonixFindForge(v any) bool {
	switch node := v.(type) {
	case map[string]any:
		if name, _ := node["name"].(string); name == reasonixPluginName {
			// This node is a forge entry. It is a search leaf: count it only when active.
			// Returning false here (disabled) does not abort the enclosing loop — the caller
			// keeps scanning siblings for an active forge entry.
			//
			// 本节点是 forge 条目。它是搜索叶子：仅激活时算数。此处返回 false（被禁用）
			// 不会中止外层循环——调用方会继续扫兄弟节点找激活的 forge 条目。
			if enabled, ok := node["enabled"].(bool); ok && !enabled {
				return false
			}
			if disabled, ok := node["disabled"].(bool); ok && disabled {
				return false
			}
			return true
		}
		for _, child := range node {
			if reasonixFindForge(child) {
				return true
			}
		}
		return false
	case []any:
		for _, child := range node {
			if reasonixFindForge(child) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
