package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
)

// CursorTranslator wires forge hooks into cursor's USER-LEVEL hooks.json
// (~/.cursor/hooks.json — Cursor natively supports user-level hooks alongside the
// project-level .cursor/hooks.json). Cursor ships Claude-Code-compatible lifecycle
// hooks (exit 2 = deny), so alongside claude-code/codex it is an agent where Forge
// gates actually enforce rather than merely suggest.
//
// The user-level location mirrors the kimi/claude-code model: one machine-wide
// registration instead of a per-project copy, so forge init/sync no longer writes into
// the project directory (user-level assets migration). The project-level
// .cursor/rules/forge-quality.mdc guidance file is no longer generated here either —
// instruction text is unified by the skillgen layer. Existing project-level files are
// left untouched (cleanup is the uninstall/cleanup layer's job, not the translator's).
//
// Merge semantics: entries whose command is not forge-sourced (see
// isForgeBridgeCommand) are preserved verbatim; forge entries are replaced wholesale
// with the current generated set, making Translate idempotent.
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
// merge 语义：command 非 forge 来源的条目（见 isForgeBridgeCommand）原样保留；
// forge 条目整体替换为当前生成集，Translate 幂等。
type CursorTranslator struct{}

func (t *CursorTranslator) Translate(projectDir string, input *TranslationInput) error {
	// User-level translator: projectDir is intentionally ignored — the registration is
	// machine-wide (same contract as KimiTranslator).
	//
	// 用户级 translator：刻意忽略 projectDir——注册是全机器生效（与 KimiTranslator 同契约）。
	path, err := CursorHooksPath()
	if err != nil {
		return fmt.Errorf("cursor: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cursor: failed to create config dir: %w", err)
	}

	// Real lifecycle hooks — the actual enforcement interface. Cursor native hooks.json is a
	// flat structure (hooks.<event>[].{command,matcher}), event names are camelCase, and the
	// stdin/exit-code protocol is Claude-Code-compatible, so the same `forge hook <name>`
	// commands run as-is; exit 2 blocks that tool call (deny).
	//
	// 真实 lifecycle hooks——实际 enforcement 接口。Cursor 原生 hooks.json 是扁平结构
	// （hooks.<event>[].{command,matcher}），event 名为 camelCase，stdin/exit-code 协议
	// 与 Claude Code 兼容，故同一批 `forge hook <name>` 命令原样跑，exit 2 即 block
	// 该工具调用（deny）。
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cursor: failed to read hooks.json: %w", err)
	}
	merged, err := mergeCursorHooks(existing)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, merged, 0644); err != nil {
		return fmt.Errorf("cursor: failed to write hooks.json: %w", err)
	}
	return nil
}

func (t *CursorTranslator) AgentType() AgentType {
	return AgentCursor
}

// CursorHooksPath resolves the user-level hooks.json path (~/.cursor/hooks.json).
// Cursor has no documented env override for its config home, so the path derives from
// the user home directly.
//
// CursorHooksPath 解析 user-level hooks.json 路径（~/.cursor/hooks.json）。Cursor
// 没有官方文档化的 config home env 覆盖，故路径直接由用户 home 派生。
func CursorHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".cursor", "hooks.json"), nil
}

// mergeCursorHooks merges the generated forge wiring into an existing cursor
// hooks.json. Unknown top-level fields (version, user keys) are preserved via
// json.RawMessage; within the flat hooks section, entries whose command is not
// forge-sourced are kept byte-for-byte (unknown entry fields intact — see
// merge_raw.go), and forge entries are replaced wholesale with the current
// generated set. The output is deterministic, so Translate is idempotent.
// A nil/empty existing input produces a fresh file (carrying version:1).
//
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

// StripCursorHooksUserLevel removes forge hooks from the user-level ~/.cursor/hooks.json
// (uninstall path). User-defined entries (unknown fields intact, see merge_raw.go) and
// unknown top-level fields are preserved; the file itself is never deleted. Reports
// whether the file was actually modified; a missing file or a file without forge hooks
// is a clean no-op.
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
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("cursor: failed to write hooks.json: %w", err)
	}
	return true, nil
}

// buildCursorMDC renders the forge-quality guidance rules in Cursor's .mdc format.
// It is no longer written by Translate (project-level instruction files are unified
// by the skillgen layer); the renderer is retained for that layer and for the
// render-convergence guard (TestProtocolRenderConvergence).
//
// buildCursorMDC 以 Cursor 的 .mdc 格式渲染 forge-quality guidance 规则。Translate
// 不再写它（项目级指令文件由 skillgen 层统一）；渲染器保留给该层与渲染一致性守卫
// （TestProtocolRenderConvergence）使用。
func buildCursorMDC(input *TranslationInput) string {
	var sb strings.Builder

	// MDC frontmatter.
	//
	// MDC 的 frontmatter
	sb.WriteString("---\n")
	sb.WriteString("description: \"Forge quality protocol\"\n")
	sb.WriteString("alwaysApply: true\n")
	sb.WriteString("---\n\n")

	sb.WriteString("# Forge 质量标准\n\n")

	// Quality standards section.
	//
	// 质量标准段
	sb.WriteString("## 质量标准\n\n")
	protocol.RenderStandards(&sb, input.Protocol.Standards, protocol.StandardRenderStyle{
		SeverityLabel:  protocol.EmojiSeverityLabel,
		HookInfoFormat: " (enforced: %s)",
		LineFormat:     "- %s **%s**: %s%s\n",
	})
	sb.WriteString("\n")

	// Session rules section.
	//
	// 会话规则段
	sb.WriteString("## 会话行为规则\n\n")
	protocol.RenderSessionRules(&sb, input.Protocol.SessionRules, protocol.SessionRuleRenderStyle{
		MandatoryLabel: protocol.MustShouldLabel,
		LineFormat:     "- %[1]s %[2]s\n",
	})
	sb.WriteString("\n")

	// Hook info section.
	//
	// Hook 信息段
	if len(input.HookNames) > 0 {
		sb.WriteString("## 自动检查\n\n")
		sb.WriteString("以下检查通过 agent lifecycle hooks（PreToolUse/PostToolUse 等，非 .git/hooks）自动执行：\n\n")
		for _, h := range input.HookNames {
			sb.WriteString(fmt.Sprintf("- `%s`\n", h))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

type cursorHookEntry struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// buildCursorHooks derives Cursor's flat hooks.json from hooks.ForgeHookSpec (single
// source of truth). Cursor's hooks.json is flat: hooks.<event>[], each entry carries
// {command,matcher,timeout}; event names are camelCase (preToolUse/postToolUse/stop),
// in contrast to Claude Code's PascalCase nested {matcher,hooks:[{type,command}]} shape.
// Conversion flattens each matcher's hook list to one entry per hook (carrying matcher
// + 60s timeout). SessionStart is filtered — Cursor native hooks.json historically
// accepts only pre/post/stop. No manual copy → no drift.
// TestCursorWiringMirrorsClaudeSettings guards command-set parity.
//
// buildCursorHooks 从 hooks.ForgeHookSpec（单一真相源）派生 Cursor 的扁平 hooks.json。
// Cursor 的 hooks.json 是扁平结构：hooks.<event>[]，每个 entry 自带
// {command,matcher,timeout}，event 名为 camelCase（preToolUse/postToolUse/stop），
// 与 Claude Code 的 PascalCase 嵌套 {matcher,hooks:[{type,command}]} 结构相对。转换时
// 把每个 matcher 的 hook 列表扁平化为每 hook 一个 entry（携带 matcher + 60s timeout）。
// SessionStart 被过滤——Cursor 原生 hooks.json 历史上只接 pre/post/stop。无手工副本 → 无 drift。
// TestCursorWiringMirrorsClaudeSettings 守卫命令集对等。
func buildCursorHooks() map[string]any {
	spec := hooks.ForgeHookSpec()
	hooksMap := map[string][]cursorHookEntry{}
	for event, matchers := range spec {
		ce, ok := cursorEventName(event)
		if !ok {
			continue
		}
		for _, m := range matchers {
			for _, h := range m.Hooks {
				hooksMap[ce] = append(hooksMap[ce], cursorHookEntry{
					Command: h.Command,
					Matcher: m.Matcher,
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

// cursorEventName maps Claude Code PascalCase event names to Cursor's camelCase
// hooks.json event names. Events Cursor does not accept (SessionStart) return ok=false,
// so buildCursorHooks can skip them.
//
// cursorEventName 把 Claude Code 的 PascalCase event 名映射到 Cursor 的 camelCase
// hooks.json event 名。Cursor 不接的 event（SessionStart）返回 ok=false，供
// buildCursorHooks 跳过。
func cursorEventName(event string) (string, bool) {
	switch event {
	case `PreToolUse`:
		return `preToolUse`, true
	case `PostToolUse`:
		return `postToolUse`, true
	case `Stop`:
		return `stop`, true
	default:
		return ``, false
	}
}
