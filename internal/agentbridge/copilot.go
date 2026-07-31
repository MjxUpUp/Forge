package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/protocol"
)

// CopilotTranslator generates .github/instructions/forge-quality.instructions.md.
//
// CopilotTranslator 生成 .github/instructions/forge-quality.instructions.md。
type CopilotTranslator struct{}

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

	// Copilot instructions frontmatter.
	//
	// Copilot instructions 的 frontmatter
	sb.WriteString("---\n")
	sb.WriteString("applyTo: \"**/*.go,**/*.rs,**/*.ts,**/*.tsx,**/*.js,**/*.jsx,**/*.py,**/*.java,**/*.rb,**/*.zig,**/*.nim\"\n")
	sb.WriteString("---\n\n")

	sb.WriteString("# Forge Quality Protocol\n\n")

	// Quality standards.
	//
	// 质量标准
	sb.WriteString("## Quality Standards\n\n")
	protocol.RenderStandards(&sb, input.Protocol.Standards, protocol.StandardRenderStyle{
		SeverityLabel:  protocol.WordSeverityLabel,
		HookInfoFormat: " (auto-enforced via %s)",
		LineFormat:     "- [%s] **%s**: %s%s\n",
	})
	sb.WriteString("\n")

	// Session rules as behavioral directives.
	//
	// 会话规则作为行为指令
	sb.WriteString("## Behavioral Rules\n\n")
	protocol.RenderSessionRules(&sb, input.Protocol.SessionRules, protocol.SessionRuleRenderStyle{
		MandatoryLabel: protocol.AlwaysPreferLabel,
		LineFormat:     "- %[1]s: %[2]s\n",
	})
	sb.WriteString("\n")

	return sb.String()
}
