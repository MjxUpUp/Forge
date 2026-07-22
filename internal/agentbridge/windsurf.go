package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WindsurfTranslator generates .windsurf/hooks.json (real, block-capable Cascade
// hooks) plus updates .windsurfrules (guidance fallback). Cascade ships
// lifecycle hooks with exit-code-2 deny, so alongside claude-code/codex/cursor
// it is an agent where Forge gates actually enforce. Its stdin schema differs
// from Claude Code, so hook commands carry `--agent windsurf` and forge
// normalizes (see internal/cli/hook_normalize.go).
type WindsurfTranslator struct{}

func (t *WindsurfTranslator) Detect(projectDir string) bool {
	return fileExists(filepath.Join(projectDir, ".windsurfrules"))
}

func (t *WindsurfTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("windsurf: protocol is required")
	}

	// Real Cascade lifecycle hooks — the enforcement surface. Windsurf's
	// .windsurf/hooks.json is flat: hooks.<event>[].{command,show_output} with
	// snake_case event names and a stdin schema (tool_info/agent_action_name)
	// distinct from Claude Code, so commands carry `--agent windsurf` and forge
	// normalizes (internal/cli/hook_normalize.go). Pre-event exit 2 = deny.
	hooksDir := filepath.Join(projectDir, ".windsurf")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("windsurf: create .windsurf dir: %w", err)
	}
	hooksData, err := json.MarshalIndent(buildWindsurfHooks(), "", "  ")
	if err != nil {
		return fmt.Errorf("windsurf: marshal hooks.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), append(hooksData, '\n'), 0644); err != nil {
		return fmt.Errorf("windsurf: write hooks.json: %w", err)
	}

	// Guidance rules as fallback for Windsurf versions without hook support.
	content := buildWindsurfSection(input)
	path := filepath.Join(projectDir, ".windsurfrules")

	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		updated := replaceForgeRules(string(existing), content)
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// Create new file
	return os.WriteFile(path, []byte(content), 0644)
}

func (t *WindsurfTranslator) AgentType() AgentType {
	return AgentWindsurf
}

type windsurfHookEntry struct {
	Command    string `json:"command"`
	ShowOutput bool   `json:"show_output"`
}

// buildWindsurfHooks mirrors hooks/settings.go GenerateSettings for Windsurf's
// native Cascade hook format. Windsurf's hooks.json is flat —
// hooks.<event>[].{command,show_output} — with snake_case event names
// (pre_write_code/post_write_code/pre_run_command/post_run_command/
// post_read_code/session_start/session_end) versus Claude Code's PascalCase.
// Same-event multi-hook (task-guard + assertion-check on pre_write_code) runs
// both sequentially; pre-event exit 2 = deny. Kept in sync manually with
// settings.go — TestWindsurfWiringMirrorsClaudeSettings guards drift.
func buildWindsurfHooks() map[string]any {
	// task-guard and assertion-check both gate writes; Windsurf runs all
	// same-event entries in order, so a single pre_write_code list with both is
	// correct (Windsurf uses command-not-event matching, unlike Claude Code's
	// per-event separate matchers).
	return map[string]any{
		"hooks": map[string][]windsurfHookEntry{
			"pre_write_code": {
				{Command: "forge hook task-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook assertion-check --agent windsurf", ShowOutput: false},
				{Command: "forge hook read-before-edit --agent windsurf", ShowOutput: false},
			},
			"pre_run_command": {
				{Command: "forge hook bash-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook hazard-guard --agent windsurf", ShowOutput: false},
			},
			"post_write_code": {
				{Command: "forge hook auto-compile --agent windsurf", ShowOutput: false},
				{Command: "forge hook workflow-test-guard --agent windsurf", ShowOutput: false},
			},
			"post_run_command": {
				{Command: "forge hook file-sentinel --agent windsurf", ShowOutput: false},
			},
			"post_read_code": {
				{Command: "forge hook tool-track --agent windsurf", ShowOutput: false},
			},
			"session_start": {
				{Command: "forge hook skill-scan", ShowOutput: false},
				{Command: "forge hook mcp-scan", ShowOutput: false},
				{Command: "forge hook init-suggest", ShowOutput: false},
				{Command: "forge hook task-resume", ShowOutput: false},
			},
			"session_end": {
				{Command: "forge hook task-verify", ShowOutput: false},
				{Command: "forge hook review-stop", ShowOutput: false},
			},
		},
	}
}

const (
	forgeRulesStart = "<!-- FORGE:START -->"
	forgeRulesEnd   = "<!-- FORGE:END -->"
)

func buildWindsurfSection(input *TranslationInput) string {
	var sb strings.Builder

	sb.WriteString(forgeRulesStart + "\n\n")
	sb.WriteString("# Forge Quality Standards\n\n")

	// Quality standards
	for _, s := range input.Protocol.Standards {
		if !s.Enabled {
			continue
		}
		severity := "ERROR"
		switch s.Severity {
		case "warning":
			severity = "WARNING"
		case "info":
			severity = "INFO"
		}
		hookInfo := ""
		if s.EnforceHook != "" {
			hookInfo = fmt.Sprintf(" (enforced: %s)", s.EnforceHook)
		}
		sb.WriteString(fmt.Sprintf("- [%s] **%s**: %s%s\n", severity, s.Name, s.Description, hookInfo))
	}
	sb.WriteString("\n")

	// Session rules
	for _, r := range input.Protocol.SessionRules {
		prefix := "ALWAYS"
		if !r.Mandatory {
			prefix = "PREFER"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", prefix, r.Instruction))
	}
	sb.WriteString("\n")

	sb.WriteString(forgeRulesEnd + "\n")
	return sb.String()
}

// replaceForgeRules replaces content between FORGE:START and FORGE:END markers,
// preserving everything outside. Same pattern as skillgen/claudemd.go.
func replaceForgeRules(content, newSection string) string {
	startIdx := strings.Index(content, forgeRulesStart)
	endIdx := strings.Index(content, forgeRulesEnd)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		// No markers — append
		return content + "\n" + newSection
	}

	before := content[:startIdx]
	after := content[endIdx+len(forgeRulesEnd):]

	section := strings.TrimRight(newSection, "\n")
	result := before + section + "\n"

	after = strings.TrimLeft(after, "\n")
	if after != "" {
		result += "\n" + after
	}
	return result
}
