package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CursorTranslator generates .cursor/hooks.json (real, block-capable lifecycle
// hooks) plus .cursor/rules/forge-quality.mdc (guidance fallback). Cursor ships
// Claude Code-compatible lifecycle hooks (exit 2 = deny), so alongside
// claude-code/codex it is an agent where Forge gates actually enforce rather
// than merely suggest.
type CursorTranslator struct{}

func (t *CursorTranslator) Detect(projectDir string) bool {
	return dirExists(filepath.Join(projectDir, ".cursor"))
}

func (t *CursorTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("cursor: protocol is required")
	}

	cursorDir := filepath.Join(projectDir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return fmt.Errorf("cursor: failed to create .cursor dir: %w", err)
	}

	// Real lifecycle hooks — the actual enforcement surface. Cursor's native
	// hooks.json is flat (hooks.<event>[].{command,matcher}) with camelCase
	// event names and a Claude Code-compatible stdin/exit-code protocol, so
	// the identical `forge hook <name>` commands run unchanged and exit 2
	// blocks the tool call (deny).
	hooksData, err := json.MarshalIndent(buildCursorHooks(), "", "  ")
	if err != nil {
		return fmt.Errorf("cursor: marshal hooks.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "hooks.json"), append(hooksData, '\n'), 0644); err != nil {
		return fmt.Errorf("cursor: write hooks.json: %w", err)
	}

	// Guidance rules as fallback: for Cursor versions without hook support,
	// or when a tool-name matcher doesn't fire (Cursor's tool names may
	// differ from Claude Code's). The .mdc still tells the agent the rules.
	rulesDir := filepath.Join(cursorDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("cursor: failed to create rules dir: %w", err)
	}
	content := buildCursorMDC(input)
	return os.WriteFile(filepath.Join(rulesDir, "forge-quality.mdc"), []byte(content), 0644)
}

func (t *CursorTranslator) AgentType() AgentType {
	return AgentCursor
}

func buildCursorMDC(input *TranslationInput) string {
	var sb strings.Builder

	// MDC frontmatter
	sb.WriteString("---\n")
	sb.WriteString("description: \"Forge quality protocol\"\n")
	sb.WriteString("alwaysApply: true\n")
	sb.WriteString("---\n\n")

	sb.WriteString("# Forge 质量标准\n\n")

	// Quality standards
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

	// Session rules
	sb.WriteString("## 会话行为规则\n\n")
	for _, r := range input.Protocol.SessionRules {
		prefix := "[MUST]"
		if !r.Mandatory {
			prefix = "[SHOULD]"
		}
		sb.WriteString(fmt.Sprintf("- %s %s\n", prefix, r.Instruction))
	}
	sb.WriteString("\n")

	// Hook info
	if len(input.HookNames) > 0 {
		sb.WriteString("## 自动检查\n\n")
		sb.WriteString("以下检查通过 git hooks 自动执行：\n\n")
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

// buildCursorHooks mirrors hooks/settings.go GenerateSettings for Cursor's
// native hook format. Cursor's hooks.json is FLAT — hooks.<event>[] each
// carrying its own {command,matcher} — with camelCase event names
// (preToolUse/postToolUse/stop) versus Claude Code's PascalCase nested
// {matcher,hooks:[{type,command}]} shape. The hook COMMAND surface is
// identical (`forge hook <name>` / `forge gate`), so gates enforce the same
// way. Kept in sync manually with settings.go — TestCursorWiringMirrorsClaudeSettings
// guards against drift.
func buildCursorHooks() map[string]any {
	hook := func(cmd, matcher string) cursorHookEntry {
		return cursorHookEntry{Command: cmd, Matcher: matcher, Timeout: 60}
	}
	return map[string]any{
		"version": 1,
		"hooks": map[string][]cursorHookEntry{
			"preToolUse": {
				hook("forge hook task-guard", "Write|Edit"),
				hook("forge hook assertion-check", "Write|Edit"),
				hook("forge hook bash-guard", "Bash"),
				hook("forge hook hazard-guard", "Bash"),
			},
			"postToolUse": {
				hook("forge hook auto-compile", "Write|Edit"),
				hook("forge hook workflow-test-guard", "Write|Edit"),
				hook("forge hook file-sentinel", "Bash"),
				hook("forge hook tool-track", "Read"),
			},
			"stop": {
				hook("forge gate --current --silent", ""),
				hook("forge hook task-verify", ""),
				hook("forge hook review-stop", ""),
			},
		},
	}
}
