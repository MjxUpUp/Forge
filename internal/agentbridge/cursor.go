package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/util"
)

// CursorTranslator wires forge hooks into cursor's USER-LEVEL hooks.json (~/.cursor/hooks.json — Cursor natively supports user-level hooks alongside the project-level .cursor/hooks.json).
//
// CursorTranslator 把 forge hook 接线进 cursor 的 user-level hooks.json
// （~/.cursor/hooks.json——Cursor 官方支持 user-level hooks，与项目级
// .cursor/hooks.json 并存）。Cursor 内置与 Claude Code 兼容的 lifecycle hooks
// （exit 2 = deny），故与 claude-code/codex 并列，是 Forge gate 真正 enforce 而非
// 仅 suggest 的 agent。
//
// 用户级路径对齐 kimi/claude-code 模型：一份全机器注册替代逐项目副本，forge
// init/sync 不再写项目目录（用户级资产迁移）。项目级 .cursor/rules/forge-quality.mdc
// guidance 文件也不再由此生成——指令文本由 skillgen 层统一处理。既有项目级文件不动
// （清理由卸载/清理层负责，translator 不管）。
//
// merge 语义：command 非 forge 来源的条目（见 hooks.IsForgeHookCommand）原样保留；
// forge 条目整体替换为当前生成集，Translate 幂等。
type CursorTranslator struct{}

func (t *CursorTranslator) Translate(projectDir string, input *TranslationInput) error {
	// 用户级 translator：刻意忽略 projectDir——注册是全机器生效（与 KimiTranslator 同契约）。
	path, err := CursorHooksPath()
	if err != nil {
		return fmt.Errorf("cursor: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cursor: failed to create config dir: %w", err)
	}

	// 真实 lifecycle hooks——实际 enforcement 接口。Cursor 原生 hooks.json 是扁平结构
	// （hooks.<event>[].{command,matcher}），event 名为 camelCase，stdin 协议与
	// Claude Code 同形，故同一批 `forge hook <name>` 命令可跑——每条带
	// `--agent cursor`，因为**输出协议**不同（上下文走顶层 snake_case
	// additional_context，阻断 = stderr + exit 2、无 stdout JSON decision；见
	// internal/cli/hook.go 的 emitCursorOutput）。
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cursor: failed to read hooks.json: %w", err)
	}
	merged, err := mergeCursorHooks(existing)
	if err != nil {
		return err
	}
	if err := util.AtomicWrite(path, merged, 0644); err != nil {
		return fmt.Errorf("cursor: failed to write hooks.json: %w", err)
	}
	return nil
}

func (t *CursorTranslator) AgentType() AgentType {
	return AgentCursor
}

// CursorHooksPath 解析 user-level hooks.json 路径（~/.cursor/hooks.json）。Cursor
// 没有官方文档化的 config home env 覆盖，故路径直接由用户 home 派生。
func CursorHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".cursor", "hooks.json"), nil
}

// mergeCursorHooks 把生成的 forge 接线合并进已有的 cursor hooks.json。未知顶层字段
// （version、用户自定义 key）经 json.RawMessage 保留；扁平 hooks 段内，command 非
// forge 来源的条目逐字节保留（未知条目字段不丢——见 merge_raw.go），forge 条目
// 整体替换为当前生成集。输出确定，故 Translate 幂等。existing 为 nil/空时生成
// 新文件（带 version:1）。
func mergeCursorHooks(existing []byte) ([]byte, error) {
	cfg := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return nil, fmt.Errorf("cursor: parse existing hooks.json: %w", err)
		}
	}
	if _, ok := cfg["version"]; !ok {
		versionJSON, err := json.Marshal(1)
		if err != nil {
			return nil, fmt.Errorf("cursor: marshal version: %w", err)
		}
		cfg["version"] = versionJSON
	}
	kept := map[string][]json.RawMessage{}
	if raw, ok := cfg["hooks"]; ok {
		var flat map[string][]json.RawMessage
		if err := json.Unmarshal(raw, &flat); err != nil {
			return nil, fmt.Errorf("cursor: parse existing hooks section: %w", err)
		}
		kept, _ = stripForgeFlatEntriesRaw(flat)
	}
	generated, err := rawHooksSection(buildCursorHooks()["hooks"])
	if err != nil {
		return nil, fmt.Errorf("cursor: marshal generated hooks: %w", err)
	}
	for event, entries := range generated {
		kept[event] = append(kept[event], entries...)
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("cursor: marshal merged hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cursor: marshal hooks.json: %w", err)
	}
	return append(data, '\n'), nil
}

// StripCursorHooksUserLevel removes forge hooks from the user-level ~/.cursor/hooks.json (uninstall path).
//
// StripCursorHooksUserLevel 移除 user-level ~/.cursor/hooks.json 中的 forge hooks
// （卸载路径）。用户自定义条目（未知字段不丢，见 merge_raw.go）与未知顶层字段保留；
// 文件本身绝不删除。返回是否实际改动了文件；文件不存在或无 forge hooks 均为干净
// no-op。
func StripCursorHooksUserLevel() (bool, error) {
	path, err := CursorHooksPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("cursor: failed to read hooks.json: %w", err)
	}
	cfg := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &cfg); err != nil {
		return false, fmt.Errorf("cursor: parse existing hooks.json: %w", err)
	}
	raw, ok := cfg["hooks"]
	if !ok {
		return false, nil
	}
	var flat map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &flat); err != nil {
		return false, fmt.Errorf("cursor: parse existing hooks section: %w", err)
	}
	kept, removedAny := stripForgeFlatEntriesRaw(flat)
	if !removedAny {
		return false, nil
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return false, fmt.Errorf("cursor: marshal stripped hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("cursor: marshal hooks.json: %w", err)
	}
	if err := util.AtomicWrite(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("cursor: failed to write hooks.json: %w", err)
	}
	return true, nil
}

type cursorHookEntry struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// buildCursorHooks 从 hooks.ForgeHookSpec（单一真相源）派生 Cursor 的扁平 hooks.json。
// Cursor 的 hooks.json 是扁平结构：hooks.<event>[]，每个 entry 自带
// {command,matcher,timeout}，event 名为 camelCase，与 Claude Code 的 PascalCase 嵌套
// {matcher,hooks:[{type,command}]} 结构相对。官方 Cursor Agent event 名册
// （https://cursor.com/docs/agent/hooks）为：sessionStart、sessionEnd、preToolUse、
// postToolUse、postToolUseFailure、subagentStart、subagentStop、beforeShellExecution、
// afterShellExecution、beforeMCPExecution、afterMCPExecution、beforeReadFile、
// afterFileEdit、beforeSubmitPrompt、preCompact、stop、afterAgentResponse、
// afterAgentThought（外加 Tab 专用的 beforeTabFileRead/afterTabFileEdit）。
// cursorEventName 把 ForgeHookSpec 的 event 映射到该名册；PostCompact 无 Cursor
// 对应物（Cursor 只有 observe-only 的 preCompact），保持 Claude/codex 专属。
// 两个 cursor 专属 delta 应用在副本上：每条 forge 命令追加 ` --agent cursor`
// （输出协议选择——Wave 1b），matcher token 经 cursorMatcherTokens 翻译到 Cursor
// 工具名册（Bash→Shell、Edit→Write、Agent→Task、Skill 丢弃——Claude 工具名在
// Cursor 上永远匹配不到，等于静默解除所有工具门禁）。转换时把每个 matcher 的
// hook 列表扁平化为每 hook 一个 entry（携带 matcher + 60s timeout）。
// 无手工副本 → 无 drift。TestCursorWiringMirrorsClaudeSettings 守卫命令集对等；
// TestCursorHooks_OnlyLegalCursorEvents 钉死 event 名白名单。
func buildCursorHooks() map[string]any {
	spec := hooks.ForgeHookSpec()
	hooksMap := map[string][]cursorHookEntry{}
	for event, matchers := range spec {
		ce, ok := cursorEventName(event)
		if !ok {
			continue
		}
		for _, m := range matchers {
			match, keep := cursorMatcherTokens(m.Matcher)
			if !keep {
				// Every token was dropped (a Skill-only matcher): Cursor has no tool to
				// fire it on — wiring it as match-all would widen the gate to every tool
				// call. Skip the entries (documented limitation).
				continue
			}
			for _, h := range m.Hooks {
				cmd := h.Command
				if hooks.IsForgeHookCommand(cmd) {
					cmd += " --agent cursor"
				}
				hooksMap[ce] = append(hooksMap[ce], cursorHookEntry{
					Command: cmd,
					Matcher: match,
					Timeout: 60,
				})
			}
		}
	}
	return map[string]any{
		`version`: 1,
		`hooks`:   hooksMap,
	}
}

// cursorMatcherTokens 把 Claude 的 tool-name matcher alternation 翻译到 Cursor 的
// 工具名册（已对 https://cursor.com/docs/agent/hooks 核实）：Bash→Shell、Edit→Write
// （cursor 的文件创建与编辑都上报为 Write）、Read→Read、Agent→Task；Skill 无 Cursor
// 工具对应物，丢弃（Skill-only matcher 返回 keep=false——为何跳过条目而非接成
// match-all 见调用处）。未知 token 原样透传（新 token 暴露给人看，好过静默丢弃）。
// 翻译后去重（Write|Edit → Write）。空输入保持空（match-all 组——stop/sessionStart
// ——匹配的是事件不是工具名）。结果保持纯 STRING alternation 形式，cursor 接受它
// 作 matcher。
func cursorMatcherTokens(matcher string) (string, bool) {
	if matcher == "" {
		return "", true
	}
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.Split(matcher, "|") {
		switch tok {
		case "Bash":
			tok = "Shell"
		case "Edit":
			tok = "Write"
		case "Agent":
			tok = "Task"
		case "Skill":
			continue
		}
		if !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "|"), true
}

// cursorEventName 把 Claude Code 的 PascalCase event 名映射到 Cursor 的 camelCase
// hooks.json event 名（已对 https://cursor.com/docs/agent/hooks 核实）。Cursor 不
// 接的 event 返回 ok=false，供 buildCursorHooks 跳过：
//   - PostCompact——Cursor 无 post-compaction event；仅有 observe-only 的
//     preCompact，无法承载 compact-resume 的重注入契约，故刻意不映射。
//   - 未来 spec 新增且无 Cursor 对应物的 event（Cursor 侧的 sessionEnd/
//     subagentStart/preCompact 等同样无 ForgeHookSpec 对应物——subagentStop 有，
//     已在下方接线）。
//
// PostToolUseFailure/SubagentStop 于 2026-08-22 加入：两者都在 Cursor 官方名册
// （见 buildCursorHooks 头注），承载 failure-track/subagent-track（#4-A 后续——
// 跨宿主事件矩阵，spec-research4）。payload 方言由 cli 侧 runHook 填空块适配
// （hook.go）：postToolUseFailure 的 error_message 文本与 failure_type 枚举同发
// （文本优先填 Error，枚举兜底）；subagentStop 把 CC 字段拼作 subagent_type/
// status/result——归一到 AgentTypeHook/LastAssistantMessage，status 随
// subagent-track 记进 Meta。
func cursorEventName(event string) (string, bool) {
	switch event {
	case `PreToolUse`:
		return `preToolUse`, true
	case `PostToolUse`:
		return `postToolUse`, true
	case `Stop`:
		return `stop`, true
	case `SessionStart`:
		return `sessionStart`, true
	case `UserPromptSubmit`:
		return `beforeSubmitPrompt`, true
	case `PostToolUseFailure`:
		return `postToolUseFailure`, true
	case `SubagentStop`:
		return `subagentStop`, true
	default:
		return ``, false
	}
}
