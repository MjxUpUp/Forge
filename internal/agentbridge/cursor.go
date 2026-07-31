package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// CursorTranslator generates .cursor/hooks.json (real, block-capable lifecycle hooks)
// and .cursor/rules/forge-quality.mdc (guidance fallback). Cursor ships Claude-Code-compatible
// lifecycle hooks (exit 2 = deny), so alongside claude-code/codex it is an agent where Forge
// gates actually enforce rather than merely suggest.
//
// CursorTranslator 生成 .cursor/hooks.json（真实、可 block 的 lifecycle hooks）与
// .cursor/rules/forge-quality.mdc（guidance 兜底）。Cursor 内置与 Claude Code 兼容的
// lifecycle hooks（exit 2 = deny），故与 claude-code/codex 并列，是 Forge gate 真正
// enforce 而非仅 suggest 的 agent。
type CursorTranslator struct{}

func (t *CursorTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("cursor: protocol is required")
	}

	cursorDir := filepath.Join(projectDir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return fmt.Errorf("cursor: failed to create .cursor dir: %w", err)
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
	hooksData, err := json.MarshalIndent(buildCursorHooks(), "", "  ")
	if err != nil {
		return fmt.Errorf("cursor: marshal hooks.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "hooks.json"), append(hooksData, '\n'), 0644); err != nil {
		return fmt.Errorf("cursor: write hooks.json: %w", err)
	}

	// Guidance rules as fallback: for Cursor versions without hook support, or when the
	// tool-name matcher misses (Cursor tool names may differ from Claude Code). The .mdc
	// still tells the agent the rules.
	//
	// Guidance 规则作为兜底：用于不支持 hook 的 Cursor 版本，或 tool-name matcher
	// 未命中时（Cursor 的 tool 名可能与 Claude Code 不同）。.mdc 仍把规则告知 agent。
	rulesDir := filepath.Join(cursorDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("cursor: failed to create rules dir: %w", err)
	}
	content := buildCursorMDC(input)
	if err := os.WriteFile(filepath.Join(rulesDir, "forge-quality.mdc"), []byte(content), 0644); err != nil {
		return fmt.Errorf("cursor: write forge-quality.mdc: %w", err)
	}
	return nil
}

func (t *CursorTranslator) AgentType() AgentType {
	return AgentCursor
}

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
	for _, s := range input.Protocol.Standards {
		if !s.Enabled {
			continue
		}
		icon := "🔴"
		switch s.Severity {
		case "warning":
			icon = "🟡"
		case "info":
			icon = "🔵"
		}
		hookInfo := ""
		if s.EnforceHook != "" {
			hookInfo = fmt.Sprintf(" (enforced: %s)", s.EnforceHook)
		}
		sb.WriteString(fmt.Sprintf("- %s **%s**: %s%s\n", icon, s.Name, s.Description, hookInfo))
	}
	sb.WriteString("\n")

	// Session rules section.
	//
	// 会话规则段
	sb.WriteString("## 会话行为规则\n\n")
	for _, r := range input.Protocol.SessionRules {
		prefix := "[MUST]"
		if !r.Mandatory {
			prefix = "[SHOULD]"
		}
		sb.WriteString(fmt.Sprintf("- %s %s\n", prefix, r.Instruction))
	}
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
