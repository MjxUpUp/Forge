package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CopilotTranslator generates .github/instructions/forge-quality.instructions.md.
type CopilotTranslator struct{}

func (t *CopilotTranslator) Detect(projectDir string) bool {
	return dirExists(filepath.Join(projectDir, ".github", "instructions"))
}

func (t *CopilotTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("copilot: protocol is required")
	}

	instructionsDir := filepath.Join(projectDir, ".github", "instructions")
	if err := os.MkdirAll(instructionsDir, 0755); err != nil {
		return fmt.Errorf("copilot: failed to create instructions dir: %w", err)
	}

	content := buildCopilotInstructions(input)
	path := filepath.Join(instructionsDir, "forge-quality.instructions.md")
	return os.WriteFile(path, []byte(content), 0644)
}

func (t *CopilotTranslator) AgentType() AgentType {
	return AgentCopilot
}

func buildCopilotInstructions(input *TranslationInput) string {
	var sb strings.Builder

	// Copilot instructions frontmatter
	sb.WriteString("---\n")
	sb.WriteString("applyTo: \"**/*.go,**/*.rs,**/*.ts,**/*.tsx,**/*.js,**/*.jsx,**/*.py,**/*.java,**/*.rb,**/*.zig,**/*.nim\"\n")
	sb.WriteString("---\n\n")

	sb.WriteString("# Forge Quality Protocol\n\n")

	// Quality standards
	sb.WriteString("## Quality Standards\n\n")
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
			hookInfo = fmt.Sprintf(" (auto-enforced via %s)", s.EnforceHook)
		}
		sb.WriteString(fmt.Sprintf("- [%s] **%s**: %s%s\n", severity, s.Name, s.Description, hookInfo))
	}
	sb.WriteString("\n")

	// Session rules as behavioral instructions
	sb.WriteString("## Behavioral Rules\n\n")
	for _, r := range input.Protocol.SessionRules {
		prefix := "ALWAYS"
		if !r.Mandatory {
			prefix = "PREFER"
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", prefix, r.Instruction))
	}
	sb.WriteString("\n")

	return sb.String()
}
