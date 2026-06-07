package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CursorTranslator generates .cursor/rules/forge-quality.mdc.
type CursorTranslator struct{}

func (t *CursorTranslator) Detect(projectDir string) bool {
	return dirExists(filepath.Join(projectDir, ".cursor"))
}

func (t *CursorTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("cursor: protocol is required")
	}

	rulesDir := filepath.Join(projectDir, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("cursor: failed to create rules dir: %w", err)
	}

	content := buildCursorMDC(input)
	path := filepath.Join(rulesDir, "forge-quality.mdc")
	return os.WriteFile(path, []byte(content), 0644)
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
