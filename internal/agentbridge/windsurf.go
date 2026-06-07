package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WindsurfTranslator generates/updates .windsurfrules with a Forge-managed section.
type WindsurfTranslator struct{}

func (t *WindsurfTranslator) Detect(projectDir string) bool {
	return fileExists(filepath.Join(projectDir, ".windsurfrules"))
}

func (t *WindsurfTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("windsurf: protocol is required")
	}

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
