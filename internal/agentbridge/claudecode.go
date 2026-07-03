package agentbridge

import (
	"fmt"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/skillgen"
)

// ClaudeCodeTranslator wraps existing Forge generation functions.
// It does not migrate code — only provides a Translator interface wrapper.
type ClaudeCodeTranslator struct{}

func (t *ClaudeCodeTranslator) Detect(projectDir string) bool {
	return dirExists(filepath.Join(projectDir, ".claude"))
}

func (t *ClaudeCodeTranslator) Translate(projectDir string, input *TranslationInput) error {
	// Generate settings.local.json
	if err := hooks.GenerateSettings(projectDir); err != nil {
		return fmt.Errorf("claude-code: failed to generate settings: %w", err)
	}

	// Generate pipeline SKILL.md
	var inferredIDs []string
	if input.Pipeline != nil {
		if err := skillgen.GenerateSkill(projectDir, input.Pipeline, inferredIDs); err != nil {
			return fmt.Errorf("claude-code: failed to generate pipeline skill: %w", err)
		}
	}

	// Generate quality SKILL.md
	if input.Protocol != nil && input.Pipeline != nil {
		if err := skillgen.GenerateQualitySkill(projectDir, input.Protocol, input.Pipeline); err != nil {
			return fmt.Errorf("claude-code: failed to generate quality skill: %w", err)
		}
	}

	// Generate CLAUDE.md
	if err := skillgen.GenerateClaudeMD(projectDir); err != nil {
		return fmt.Errorf("claude-code: failed to generate CLAUDE.md: %w", err)
	}

	// Generate .mcp.json — expose forge's 15 MCP tools (task resume/decide/attach,
	// gates, dashboard, experience) to the agent so it calls forge structurally
	// instead of the user typing CLI. Idempotent merge (see mcpconfig.go).
	if err := writeClaudeMCP(projectDir); err != nil {
		return fmt.Errorf("claude-code: failed to generate .mcp.json: %w", err)
	}

	return nil
}

func (t *ClaudeCodeTranslator) AgentType() AgentType {
	return AgentClaudeCode
}
