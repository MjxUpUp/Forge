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
	// Generate settings.local.json — only when plugin is NOT user-level installed.
	// When plugin IS installed, user-level plugin.json already registers
	// ForgeHookSpec machine-wide; writing project-level hooks is redundant.
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateSettings(projectDir); err != nil {
			return fmt.Errorf("claude-code: failed to generate settings: %w", err)
		}
	}

	// Generate quality SKILL.md
	if input.Protocol != nil {
		if err := skillgen.GenerateQualitySkill(projectDir, input.Protocol); err != nil {
			return fmt.Errorf("claude-code: failed to generate quality skill: %w", err)
		}
	}

	// Generate CLAUDE.md
	if err := skillgen.GenerateClaudeMD(projectDir); err != nil {
		return fmt.Errorf("claude-code: failed to generate CLAUDE.md: %w", err)
	}

	return nil
}

func (t *ClaudeCodeTranslator) AgentType() AgentType {
	return AgentClaudeCode
}
