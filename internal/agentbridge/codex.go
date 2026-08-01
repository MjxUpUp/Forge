package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// CodexTranslator wires forge hooks into codex's USER-LEVEL hooks.json
// (CodexHome()/hooks.json, i.e. $CODEX_HOME/hooks.json or ~/.codex/hooks.json).
// Codex's lifecycle hooks (PreToolUse/PostToolUse/Stop) are schema-compatible with Claude Code's —
// the matcher/hooks/type/command structure is identical, and so is the stdin/stdout JSON protocol — so the same set of
// `forge hook <name>` commands run unchanged. Alongside claude-code and cursor, codex is one of the agents where hooks truly
// enforce Forge gates; copilot/windsurf still emit guidance text only.
// See CursorTranslator for cursor's flat schema variant.
//
// The user-level location mirrors the kimi/claude-code model: one machine-wide registration
// instead of a per-project .codex/hooks.json copy, so forge init/sync no longer writes into
// the project directory (user-level assets migration; project-level residue cleanup is
// handled by the uninstall/cleanup layer, not here). Merge semantics: entries whose command
// is not forge-sourced (see isForgeBridgeCommand) are preserved verbatim; forge entries are
// replaced wholesale with the current generated set, making Translate idempotent.
//
// Matcher note: Codex compiles the matcher into a regex against tool_name, whereas Claude Code treats it
// as a tool-name match. Plain names (Bash) and alternations (Write|Edit) are both valid regexes and match
// identically in both, so the Claude wiring migrates over directly. Forge never emits the glob-style `Bash(...)` form —
// it is not a legal matcher in Codex.
//
// CodexTranslator 把 forge hook 接线进 codex 的 user-level hooks.json
// （CodexHome()/hooks.json，即 $CODEX_HOME/hooks.json 或 ~/.codex/hooks.json）。Codex 的
// lifecycle hooks（PreToolUse/PostToolUse/Stop）与 Claude Code 的 schema 兼容——
// matcher/hooks/type/command 结构相同，stdin/stdout JSON 协议也相同——故同一批
// `forge hook <name>` 命令原样跑。与 claude-code、cursor 并列，codex 是 hook 真正
// enforce Forge gate 的 agent 之一；copilot/windsurf 仍只发 guidance 文本。
// cursor 的扁平 schema 变体见 CursorTranslator。
//
// 用户级路径对齐 kimi/claude-code 模型：一份全机器注册替代逐项目的 .codex/hooks.json
// 副本，forge init/sync 不再写项目目录（用户级资产迁移；项目级残留由卸载/清理层
// 处理，不在此处）。merge 语义：command 非 forge 来源的条目（见 isForgeBridgeCommand）
// 原样保留；forge 条目整体替换为当前生成集，Translate 幂等。
//
// Matcher 注意：Codex 把 matcher 编译为针对 tool_name 的 regex，而 Claude Code 把它
// 当 tool-name 匹配。纯名（Bash）与 alternation（Write|Edit）都是合法 regex，在两者
// 中匹配结果一致，故 Claude 接线可直接迁移。Forge 从不发 glob 风格的 `Bash(...)` 形式——
// 它在 Codex 里不是合法 matcher。
type CodexTranslator struct{}

func (t *CodexTranslator) Translate(projectDir string, input *TranslationInput) error {
	// User-level translator: projectDir is intentionally ignored — the registration is
	// machine-wide (same contract as KimiTranslator).
	//
	// 用户级 translator：刻意忽略 projectDir——注册是全机器生效（与 KimiTranslator 同契约）。
	path, err := CodexHooksPath()
	if err != nil {
		return fmt.Errorf("codex: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("codex: failed to create config dir: %w", err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("codex: failed to read hooks.json: %w", err)
	}
	merged, err := mergeCodexHooks(existing)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, merged, 0644); err != nil {
		return fmt.Errorf("codex: failed to write hooks.json: %w", err)
	}
	return nil
}

func (t *CodexTranslator) AgentType() AgentType {
	return AgentCodex
}

// CodexHome resolves codex's config home: $CODEX_HOME when set (codex CLI's documented
// convention), otherwise ~/.codex.
//
// CodexHome 解析 codex 的 config home：设了 $CODEX_HOME 用它（codex CLI 的官方约定），
// 否则 ~/.codex。
func CodexHome() (string, error) {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

// CodexHooksPath resolves the user-level hooks.json path inside CodexHome.
//
// CodexHooksPath 解析 CodexHome 下的 user-level hooks.json 路径。
func CodexHooksPath() (string, error) {
	home, err := CodexHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "hooks.json"), nil
}

// mergeCodexHooks merges the generated forge wiring into an existing codex hooks.json.
// Unknown top-level fields are preserved via json.RawMessage; within the hooks section,
// entries whose command is not forge-sourced are kept in place, and forge entries are
// replaced wholesale with the current generated set (appended after the user's entries —
// hook execution order among forge gates is preserved from the spec, and codex runs
// per-event entries in order). The output is deterministic, so Translate is idempotent.
// A nil/empty existing input produces a fresh file.
//
// mergeCodexHooks 把生成的 forge 接线合并进已有的 codex hooks.json。未知顶层字段经
// json.RawMessage 保留；hooks 段内，command 非 forge 来源的条目原地保留，forge 条目
// 整体替换为当前生成集（追加在用户条目之后——forge 门禁间的执行顺序按 spec 保持，
// codex 按序执行同 event 条目）。输出确定，故 Translate 幂等。existing 为 nil/空时
// 生成新文件。
func mergeCodexHooks(existing []byte) ([]byte, error) {
	cfg := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return nil, fmt.Errorf("codex: parse existing hooks.json: %w", err)
		}
	}
	kept := map[string][]hooks.HookMatcher{}
	if raw, ok := cfg["hooks"]; ok {
		var spec map[string][]hooks.HookMatcher
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, fmt.Errorf("codex: parse existing hooks section: %w", err)
		}
		kept = stripForgeMatchers(spec)
	}
	generated, err := codexGeneratedMatchers()
	if err != nil {
		return nil, err
	}
	for event, matchers := range generated {
		kept[event] = append(kept[event], matchers...)
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal merged hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("codex: marshal hooks.json: %w", err)
	}
	return append(data, '\n'), nil
}

// codexGeneratedMatchers returns the generated forge wiring as typed matchers (the
// typed form of buildCodexHooks' hooks section), for mergeCodexHooks.
//
// codexGeneratedMatchers 以类型化 matcher 形式返回生成的 forge 接线（buildCodexHooks
// hooks 段的类型化形态），供 mergeCodexHooks 使用。
func codexGeneratedMatchers() (map[string][]hooks.HookMatcher, error) {
	raw, err := json.Marshal(buildCodexHooks()["hooks"])
	if err != nil {
		return nil, fmt.Errorf("codex: marshal generated hooks: %w", err)
	}
	var spec map[string][]hooks.HookMatcher
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("codex: reparse generated hooks: %w", err)
	}
	return spec, nil
}

// stripForgeMatchers removes every forge-sourced hook entry (and the matchers/events
// left empty by the removal) from a Claude-Code-shaped nested spec. User-defined
// entries are preserved in their original order. Shared by codex's merge and strip
// paths (both operate on the same nested schema).
//
// stripForgeMatchers 从 Claude-Code 形嵌套 spec 中移除所有 forge 来源的 hook 条目
// （以及因此被掏空的 matcher/event）。用户自定义条目按原顺序保留。codex 的 merge
// 与 strip 两条路径共用（两者操作同一嵌套 schema）。
func stripForgeMatchers(spec map[string][]hooks.HookMatcher) map[string][]hooks.HookMatcher {
	kept := make(map[string][]hooks.HookMatcher, len(spec))
	for event, matchers := range spec {
		var keptMatchers []hooks.HookMatcher
		for _, m := range matchers {
			var keptHooks []hooks.HookEntry
			for _, h := range m.Hooks {
				if isForgeBridgeCommand(h.Command) {
					continue
				}
				keptHooks = append(keptHooks, h)
			}
			if len(keptHooks) > 0 {
				m.Hooks = keptHooks
				keptMatchers = append(keptMatchers, m)
			}
		}
		if len(keptMatchers) > 0 {
			kept[event] = keptMatchers
		}
	}
	return kept
}

// isForgeBridgeCommand reports whether a hook command is forge-sourced (commands written
// by the agent translators: forge hook <name> / forge gate ..., optionally carrying an
// --agent <name> suffix). Mirrors hooks.isForgeHookCommand — duplicated here because the
// hooks-package helper is unexported and the agentbridge merge paths must not reach into
// another package's internals.
//
// isForgeBridgeCommand 报告 hook command 是否 forge 来源（agent translator 写入的命令：
// forge hook <name> / forge gate ...，可带 --agent <name> 后缀）。镜像
// hooks.isForgeHookCommand——因 hooks 包的 helper 未导出而在此复制，agentbridge 的
// merge 路径不应跨包探入内部实现。
func isForgeBridgeCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "forge hook ") ||
		strings.HasPrefix(cmd, "forge gate ") ||
		cmd == "forge hook" || cmd == "forge gate"
}

// StripCodexHooksUserLevel removes forge hooks from the user-level ~/.codex/hooks.json
// (uninstall path). User-defined entries and unknown top-level fields are preserved;
// the file itself is never deleted. Reports whether the file was actually modified;
// a missing file or a file without forge hooks is a clean no-op.
//
// StripCodexHooksUserLevel 移除 user-level ~/.codex/hooks.json 中的 forge hooks
// （卸载路径）。用户自定义条目与未知顶层字段保留；文件本身绝不删除。返回是否实际
// 改动了文件；文件不存在或无 forge hooks 均为干净 no-op。
func StripCodexHooksUserLevel() (bool, error) {
	path, err := CodexHooksPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("codex: failed to read hooks.json: %w", err)
	}
	cfg := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &cfg); err != nil {
		return false, fmt.Errorf("codex: parse existing hooks.json: %w", err)
	}
	raw, ok := cfg["hooks"]
	if !ok {
		return false, nil
	}
	var spec map[string][]hooks.HookMatcher
	if err := json.Unmarshal(raw, &spec); err != nil {
		return false, fmt.Errorf("codex: parse existing hooks section: %w", err)
	}
	if !hasForgeMatcher(spec) {
		return false, nil
	}
	kept := stripForgeMatchers(spec)
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return false, fmt.Errorf("codex: marshal stripped hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("codex: marshal hooks.json: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("codex: failed to write hooks.json: %w", err)
	}
	return true, nil
}

// hasForgeMatcher reports whether a Claude-Code-shaped nested spec contains any
// forge-sourced hook entry.
//
// hasForgeMatcher 报告 Claude-Code 形嵌套 spec 是否含任何 forge 来源的 hook 条目。
func hasForgeMatcher(spec map[string][]hooks.HookMatcher) bool {
	for _, matchers := range spec {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if isForgeBridgeCommand(h.Command) {
					return true
				}
			}
		}
	}
	return false
}

// buildCodexHooks derives codex's hooks.json from hooks.ForgeHookSpec — that spec is the single source of truth shared with
// settings.local.json and the plugin pack. Codex's hook schema is identical to Claude Code's
// nested {matcher, hooks:[{type,command}]} structure (Codex compiles the matcher into a regex against
// tool_name; Forge emits only plain names and alternations, both legal regexes), so the spec can be marshaled
// as-is into a legal codex hooks.json. Codex has no SessionStart lifecycle hook, so that event is
// filtered out (skill-scan is Claude-Code exclusive). No hand-maintained copy → no drift.
// TestCodexWiringMirrorsClaudeSettings guards command-set parity.
//
// buildCodexHooks 从 hooks.ForgeHookSpec 派生 codex 的 hooks.json——该 spec 是与
// settings.local.json、plugin pack 共享的单一真相源。Codex 的 hook schema 与 Claude Code
// 的嵌套 {matcher, hooks:[{type,command}]} 结构相同（Codex 把 matcher 编译为针对
// tool_name 的 regex；Forge 只发纯名与 alternation，均合法 regex），故 spec 可原样
// marshal 为合法 codex hooks.json。Codex 无 SessionStart lifecycle hook，故该 event
// 被过滤（skill-scan 是 Claude-Code 专属）。无手工副本 → 无 drift。
// TestCodexWiringMirrorsClaudeSettings 守卫命令集对等。
func buildCodexHooks() map[string]any {
	spec := hooks.ForgeHookSpec()
	codex := make(map[string][]hooks.HookMatcher, len(spec))
	for event, matchers := range spec {
		// Whitelist: codex supports only PreToolUse/PostToolUse/Stop (no SessionStart/PostCompact/
		// UserPromptSubmit or other session/compress/prompt lifecycle). Other claude-code-specific events — including
		// the gap#2 PostCompact/UserPromptSubmit re-injection chain — are skipped automatically.
		//
		// 白名单：codex 只支持 PreToolUse/PostToolUse/Stop（无 SessionStart/PostCompact/
		// UserPromptSubmit 等会话/压缩/prompt lifecycle）。其余 claude-code 特有 event——含
		// gap#2 的 PostCompact/UserPromptSubmit 重注入链——自动跳过。
		if event != "PreToolUse" && event != "PostToolUse" && event != "Stop" {
			continue
		}
		codex[event] = matchers
	}
	return map[string]any{
		`hooks`: codex,
	}
}
