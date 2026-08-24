package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// ZcodeTranslator wires forge hooks into ZCode's USER-LEVEL config
// (~/.zcode/cli/config.json, hooks.events). ZCode (z.ai's agentic IDE, GLM
// family) is deliberately Claude-Code-compatible at the hook layer
// (zcode.z.ai/en/docs/hooks, verified 2026-08):
//   - stdin carries Claude snake_case aliases (session_id / tool_name /
//     tool_input / hook_event_name) next to its camelCase fields — the default
//     HookInput unmarshal suffices, no StdinDialect.
//   - stdout reads Claude's shapes: hookSpecificOutput.additionalContext
//     (context injection), permissionDecision allow/deny (PreToolUse),
//     continue:false + reason (UserPromptSubmit), decision:block + reason
//     (Stop). Unknown fields are ignored.
//   - exit 2 is the blocking shortcut on every blockable event.
//
// So the generated commands need no protocol override: `--agent zcode` is
// appended for session attribution (EnsureHookSession/StampSessionAgent) while
// stdin parsing and the emitClaudeOutput default both already speak ZCode's
// dialect — the codebuddy/opencode "Claude-shape, no flag needed" model, plus
// the flag purely for identity. Docs-derived; wire-level verification (the
// kimi-hook-routing.md discipline) is pending a ZCode install.
//
// Event coverage: ZCode's roster is SessionStart / UserPromptSubmit /
// PreToolUse / PermissionRequest / PostToolUse / PostToolUseFailure / Stop —
// PascalCase names identical to Claude's, so no rename map, only a whitelist:
//   - PostCompact has no ZCode event; compaction fires SessionStart with
//     source=compact, which the match-all SessionStart group already covers
//     (task-resume tl;dr tier — the same fallback cursor takes).
//   - SubagentStop is not on the roster; subagent-track is dropped.
//   - PermissionRequest is ZCode-only with no ForgeHookSpec counterpart.
//
// One enforcement caveat wired into every Stop hook's semantics: ZCode
// force-ends the run after 3 consecutive Stop blocks, so task-verify /
// review-stop continuation loops must converge within 3 rounds (the hooks'
// own stop_hook_active guards already bound them).
//
// Project-level hooks are NOT executed by the current ZCode version
// (config_project_hooks_ignored) — this translator is user-level only, and
// team distribution goes through the plugin channel (ZCode falls back to
// reading .claude-plugin/plugin.json when .zcode-plugin/plugin.json is
// absent, so the existing plugins/forge pack loads as-is).
//
// Merge semantics: config.json also carries the user's own settings
// (hooks.timeoutMs/maxOutputBytes, model prefs), so whole-file overwrite is
// out. Top-level and hooks-level unknown fields are preserved via
// json.RawMessage; within hooks.events (Claude's nested {matcher, hooks:[]}
// shape) user entries keep their original bytes and forge entries are
// replaced wholesale (stripForgeMatchersRaw + append generated) — idempotent.
// hooks.enabled is forced true: without it ZCode executes NOTHING, and
// `forge init --agents zcode` is the user's explicit request to wire hooks.
//
// ZcodeTranslator 把 forge hook 接线进 ZCode 的**用户级**配置
// （~/.zcode/cli/config.json 的 hooks.events）。ZCode（z.ai 的 agentic IDE，
// GLM 系列）在 hook 层刻意与 Claude Code 兼容（zcode.z.ai/en/docs/hooks，
// 2026-08 核实）：
//   - stdin 在 camelCase 字段旁携带 Claude 蛇形别名（session_id /
//     tool_name / tool_input / hook_event_name）——默认 HookInput unmarshal
//     即可，无需 StdinDialect。
//   - stdout 读 Claude 形态：hookSpecificOutput.additionalContext（上下文
//     注入）、permissionDecision allow/deny（PreToolUse）、continue:false +
//     reason（UserPromptSubmit）、decision:block + reason（Stop）。未知字段
//     被忽略。
//   - exit 2 是所有可阻断事件的阻断捷径。
//
// 故生成的命令无需协议覆盖：追加 `--agent zcode` 仅为会话归因
// （EnsureHookSession/StampSessionAgent），stdin 解析与 emitClaudeOutput
// 默认实现本已说 ZCode 的方言——codebuddy/opencode 的「Claude 形、无需
// flag」模型，此处 flag 纯粹为身份。以上为文档结论；wire 级验证（
// kimi-hook-routing.md 的标准）待有 ZCode 安装环境后补。
//
// 事件覆盖：ZCode 名册为 SessionStart / UserPromptSubmit / PreToolUse /
// PermissionRequest / PostToolUse / PostToolUseFailure / Stop——PascalCase
// 名与 Claude 逐字相同，故无需改名映射、只需白名单：
//   - PostCompact 无 ZCode 事件；压缩以 source=compact 触发 SessionStart，
//     match-all 的 SessionStart 组已覆盖（task-resume tl;dr 层——与 cursor
//     同一回落）。
//   - SubagentStop 不在名册；subagent-track 丢弃。
//   - PermissionRequest 为 ZCode 独有，无 ForgeHookSpec 对应物。
//
// 一条写进每个 Stop hook 语义的执法注意点：ZCode 连续 3 次 Stop 阻断后强制
// 结束会话，故 task-verify / review-stop 的续跑循环必须在 3 轮内收敛
// （hook 自身的 stop_hook_active 守卫本已设界）。
//
// 项目级 hooks 在当前 ZCode 版本不执行（config_project_hooks_ignored）——
// 本 translator 仅用户级，团队分发走 plugin 渠道（.zcode-plugin/plugin.json
// 缺席时 ZCode 回落读 .claude-plugin/plugin.json，现有 plugins/forge 包可
// 原样加载）。
//
// merge 语义：config.json 还承载用户自己的设置（hooks.timeoutMs/
// maxOutputBytes、模型偏好），整文件覆盖不可行。顶层与 hooks 级未知字段经
// json.RawMessage 保留；hooks.events 内（Claude 嵌套 {matcher, hooks:[]}
// 形态）用户条目保留原始字节、forge 条目整体替换（stripForgeMatchersRaw +
// 追加生成集）——幂等。hooks.enabled 强制为 true：没它 ZCode 什么都不执行，
// 而 `forge init --agents zcode` 本身就是用户接线 hook 的显式请求。
type ZcodeTranslator struct{}

func (t *ZcodeTranslator) AgentType() AgentType {
	return AgentZcode
}

// zcodeSupportedEvents is the ZCode hook roster (zcode.z.ai/en/docs/hooks)
// intersected with ForgeHookSpec. Names are verbatim Claude PascalCase — the
// whitelist only DROPS events (PostCompact, SubagentStop — see the translator
// header), it never renames.
//
// zcodeSupportedEvents 是 ZCode hook 名册（zcode.z.ai/en/docs/hooks）与
// ForgeHookSpec 的交集。事件名逐字沿用 Claude PascalCase——白名单只**丢**
// 事件（PostCompact、SubagentStop——见 translator 头注），从不改名。
var zcodeSupportedEvents = map[string]bool{
	"SessionStart":       true,
	"UserPromptSubmit":   true,
	"PreToolUse":         true,
	"PostToolUse":        true,
	"PostToolUseFailure": true,
	"Stop":               true,
}

type zcodeHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	// Timeout is ZCode's compatibility field, in SECONDS (timeoutMs wins when
	// both are set). 60 matches cursor's flat per-entry convention; ZCode's
	// root default is 60s anyway, so this only pins the value against a user
	// lowering their root timeoutMs.
	//
	// Timeout 是 ZCode 的兼容字段，单位**秒**（两者同设时 timeoutMs 优先）。
	// 60 对齐 cursor 的逐条目惯例；ZCode 根默认本为 60s，此处只是防用户调低
	// 根 timeoutMs 后 hook 被提前掐死。
	Timeout int `json:"timeout,omitempty"`
}

type zcodeHookGroup struct {
	Matcher string           `json:"matcher,omitempty"`
	Hooks   []zcodeHookEntry `json:"hooks"`
}

func (t *ZcodeTranslator) Translate(projectDir string, input *TranslationInput) error {
	// User-level translator: projectDir is intentionally ignored — the
	// registration is machine-wide (same contract as KimiTranslator).
	//
	// 用户级 translator：刻意忽略 projectDir——注册全机器生效（与
	// KimiTranslator 同契约）。
	home, err := ZcodeConfigHome()
	if err != nil {
		return fmt.Errorf("zcode: %w", err)
	}
	// Detection self-poison guard: DetectAgents treats "~/.zcode exists" as
	// "zcode installed" (hostcap InstallIndicators). Creating the dir on a
	// machine WITHOUT ZCode would make every later detection wire a
	// non-existent tool (the same guard as hooks.GenerateUserSettings'
	// ~/.claude check). Explicit `--agents zcode` on a zcode-less machine is
	// therefore a no-op — consistent with the claude/cursor behavior.
	//
	// 检测自毒防线：DetectAgents 以「~/.zcode 存在」判定「zcode 已安装」
	// （hostcap InstallIndicators）。在未装 ZCode 的机器上创建该目录会让后续
	// 每次检测都误接一个不存在的工具（与 hooks.GenerateUserSettings 的
	// ~/.claude 检查同一防线）。故无 ZCode 机器上的显式 `--agents zcode` 为
	// no-op——与 claude/cursor 行为一致。
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return nil
	}
	path := filepath.Join(home, "cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("zcode: failed to create config dir: %w", err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("zcode: failed to read config.json: %w", err)
	}
	merged, err := mergeZcodeConfig(existing)
	if err != nil {
		return err
	}
	if string(existing) == string(merged) {
		return nil // already up to date — idempotent no-op
	}
	if err := os.WriteFile(path, merged, 0644); err != nil {
		return fmt.Errorf("zcode: failed to write config.json: %w", err)
	}
	return nil
}

// ZcodeConfigHome resolves ZCode's config home (~/.zcode). ZCode documents no
// env override for its config home (unlike CLAUDE_CONFIG_DIR/CODEX_HOME), so
// the path derives from the user home directly — same convention as cursor.
//
// ZcodeConfigHome 解析 ZCode 的 config home（~/.zcode）。ZCode 未文档化
// config home 的 env 覆盖（不像 CLAUDE_CONFIG_DIR/CODEX_HOME），故路径直接由
// 用户 home 派生——与 cursor 同约定。
func ZcodeConfigHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".zcode"), nil
}

// ZcodeConfigPath resolves the cli/config.json path inside ZcodeConfigHome.
//
// ZcodeConfigPath 解析 ZcodeConfigHome 下的 cli/config.json 路径。
func ZcodeConfigPath() (string, error) {
	home, err := ZcodeConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "cli", "config.json"), nil
}

// buildZcodeHooks derives ZCode's hooks.events map from hooks.ForgeHookSpec —
// the single source of truth shared with every other translator. The shape is
// Claude's nested {matcher, hooks:[{type, command}]} with PascalCase event
// names, so the only transformations are: drop events outside ZCode's roster
// (zcodeSupportedEvents), append ` --agent zcode` to each forge command
// (attribution, see the translator header), and pin timeout:60s. Matchers pass
// through verbatim: ZCode matches Claude tool names (Write|Edit / Read|Bash in
// the official examples, Agent/Task aliases supported); a token ZCode does not
// know simply never matches — harmless, unlike cursor where Bash had to become
// Shell. Sorted map marshaling (encoding/json sorts keys) keeps the output
// deterministic. TestZcodeWiringMirrorsClaudeSettings guards command-set
// parity on supported events.
//
// buildZcodeHooks 从 hooks.ForgeHookSpec——与所有 translator 共享的单一真相源
// ——派生 ZCode 的 hooks.events map。形态是 Claude 的嵌套 {matcher,
// hooks:[{type, command}]}、PascalCase 事件名，故仅有的转换为：丢弃 ZCode
// 名册外的事件（zcodeSupportedEvents）、给每条 forge 命令追加
// ` --agent zcode`（归因，见 translator 头注）、钉死 timeout:60s。matcher
// 原样透传：ZCode 匹配 Claude 工具名（官方示例 Write|Edit / Read|Bash，
// 支持 Agent/Task 别名）；ZCode 不认识的 token 只是永不命中——无害，不像
// cursor 那里 Bash 必须译成 Shell。map marshal 的 key 排序
// （encoding/json）保证输出确定。TestZcodeWiringMirrorsClaudeSettings 守卫
// 受支持事件上的命令集对等。
func buildZcodeHooks() map[string][]zcodeHookGroup {
	spec := hooks.ForgeHookSpec()
	events := make([]string, 0, len(spec))
	for ev := range spec {
		if zcodeSupportedEvents[ev] {
			events = append(events, ev)
		}
	}
	sort.Strings(events)

	out := make(map[string][]zcodeHookGroup, len(events))
	for _, ev := range events {
		for _, m := range spec[ev] {
			group := zcodeHookGroup{Matcher: m.Matcher}
			for _, h := range m.Hooks {
				cmd := h.Command
				if isForgeBridgeCommand(cmd) {
					cmd += " --agent zcode"
				}
				group.Hooks = append(group.Hooks, zcodeHookEntry{
					Type:    "command",
					Command: cmd,
					Timeout: 60,
				})
			}
			out[ev] = append(out[ev], group)
		}
	}
	return out
}

// mergeZcodeConfig merges the generated forge wiring into an existing ZCode
// config.json. Unknown top-level fields (user settings) and unknown
// hooks-level fields (timeoutMs/maxOutputBytes) are preserved via
// json.RawMessage; within hooks.events, forge-sourced entries are stripped and
// the current generated set appended (user entries keep their original bytes —
// merge_raw.go contract). hooks.enabled is set true (ZCode executes nothing
// without it). Deterministic output → Translate is idempotent. A nil/empty
// existing input produces a fresh minimal config.
//
// mergeZcodeConfig 把生成的 forge 接线合并进已有 ZCode config.json。未知顶层
// 字段（用户设置）与未知 hooks 级字段（timeoutMs/maxOutputBytes）经
// json.RawMessage 保留；hooks.events 内剥除 forge 来源条目后追加当前生成集
// （用户条目保留原始字节——merge_raw.go 契约）。hooks.enabled 置 true
// （没有它 ZCode 什么都不执行）。输出确定 → Translate 幂等。existing 为
// nil/空时生成一份最小新配置。
func mergeZcodeConfig(existing []byte) ([]byte, error) {
	cfg := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return nil, fmt.Errorf("zcode: parse existing config.json: %w", err)
		}
	}
	// A literal `null` body (or `{"hooks": null}` below) unmarshals into a NIL
	// map — assigning a key into it panics. Re-seat both maps after every
	// unmarshal so a hand-emptied config takes the fresh-file path instead of
	// crashing forge init.
	//
	// 字面 `null` 正文（或下方的 `{"hooks": null}`）会 unmarshal 成 **nil**
	// map——往里写键即 panic。每次 unmarshal 后重坐两个 map，让被手工清空的
	// 配置走全新文件路径，而不是让 forge init 崩掉。
	if cfg == nil {
		cfg = map[string]json.RawMessage{}
	}

	hooksObj := map[string]json.RawMessage{}
	if raw, ok := cfg["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooksObj); err != nil {
			return nil, fmt.Errorf("zcode: parse existing hooks section: %w", err)
		}
	}
	if hooksObj == nil {
		hooksObj = map[string]json.RawMessage{}
	}
	enabledJSON, err := json.Marshal(true)
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal hooks.enabled: %w", err)
	}
	hooksObj["enabled"] = enabledJSON

	kept := map[string][]json.RawMessage{}
	if raw, ok := hooksObj["events"]; ok {
		var spec map[string][]json.RawMessage
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, fmt.Errorf("zcode: parse existing hooks.events section: %w", err)
		}
		kept, _ = stripForgeMatchersRaw(spec)
	}
	generated, err := rawHooksSection(buildZcodeHooks())
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal generated hooks: %w", err)
	}
	for event, groups := range generated {
		kept[event] = append(kept[event], groups...)
	}
	eventsJSON, err := json.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal merged events: %w", err)
	}
	hooksObj["events"] = eventsJSON
	hooksJSON, err := json.Marshal(hooksObj)
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal hooks section: %w", err)
	}
	cfg["hooks"] = hooksJSON

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal config.json: %w", err)
	}
	return append(data, '\n'), nil
}

// StripZcodeHooks removes forge hooks from ZCode's user-level
// ~/.zcode/cli/config.json (uninstall path). User-defined entries (unknown
// fields intact, see merge_raw.go), unknown hooks-level fields (including the
// enabled flag — the user's own hooks may depend on it), and unknown top-level
// fields are preserved; the file itself is never deleted. Reports whether the
// file was actually modified; a missing file or a file without forge hooks is
// a clean no-op.
//
// StripZcodeHooks 移除用户级 ~/.zcode/cli/config.json 中的 forge hooks（卸载
// 路径）。用户自定义条目（未知字段不丢，见 merge_raw.go）、未知 hooks 级字段
// （含 enabled 标志——用户自己的 hook 可能依赖它）与未知顶层字段全部保留；
// 文件本身绝不删除。返回是否实际改动了文件；文件不存在或无 forge hooks 均为
// 干净 no-op。
func StripZcodeHooks() (bool, error) {
	path, err := ZcodeConfigPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("zcode: failed to read config.json: %w", err)
	}
	cfg := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &cfg); err != nil {
		return false, fmt.Errorf("zcode: parse existing config.json: %w", err)
	}
	raw, ok := cfg["hooks"]
	if !ok {
		return false, nil
	}
	hooksObj := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &hooksObj); err != nil {
		return false, fmt.Errorf("zcode: parse existing hooks section: %w", err)
	}
	rawEvents, ok := hooksObj["events"]
	if !ok {
		return false, nil
	}
	var spec map[string][]json.RawMessage
	if err := json.Unmarshal(rawEvents, &spec); err != nil {
		return false, fmt.Errorf("zcode: parse existing hooks.events section: %w", err)
	}
	kept, removedAny := stripForgeMatchersRaw(spec)
	if !removedAny {
		return false, nil
	}
	eventsJSON, err := json.Marshal(kept)
	if err != nil {
		return false, fmt.Errorf("zcode: marshal stripped events: %w", err)
	}
	hooksObj["events"] = eventsJSON
	hooksJSON, err := json.Marshal(hooksObj)
	if err != nil {
		return false, fmt.Errorf("zcode: marshal hooks section: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("zcode: marshal config.json: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("zcode: failed to write config.json: %w", err)
	}
	return true, nil
}
