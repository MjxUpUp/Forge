package agentbridge

import (
	"fmt"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/skillgen"
)

// ClaudeCodeTranslator wraps the existing Forge generation functions.
// No code migration — only provides a wrapper for the Translator interface.
//
// ClaudeCodeTranslator 包装既有的 Forge 生成函数。
// 不迁移代码——仅提供 Translator 接口的包装。
type ClaudeCodeTranslator struct{}

func (t *ClaudeCodeTranslator) Detect(projectDir string) bool {
	return dirExists(filepath.Join(projectDir, ".claude"))
}

func (t *ClaudeCodeTranslator) Translate(projectDir string, input *TranslationInput) error {
	// Generate settings.local.json — only when the plugin is NOT user-level installed.
	// When the plugin is installed, user-level plugin.json already registers ForgeHookSpec
	// machine-wide; writing project-level hooks is redundant.
	//
	// 生成 settings.local.json——仅在 plugin 未 user-level 安装时。
	// plugin 已安装时，user-level plugin.json 已全机器注册 ForgeHookSpec，
	// 再写 project-level hooks 是冗余。
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateSettings(projectDir); err != nil {
			return fmt.Errorf("claude-code: failed to generate settings: %w", err)
		}
	}

	// Generate quality SKILL.md.
	//
	// 生成 quality SKILL.md
	if input.Protocol != nil {
		if err := skillgen.GenerateQualitySkill(projectDir, input.Protocol); err != nil {
			return fmt.Errorf("claude-code: failed to generate quality skill: %w", err)
		}
	}

	// Generate CLAUDE.md.
	//
	// 生成 CLAUDE.md
	if err := skillgen.GenerateClaudeMD(projectDir); err != nil {
		return fmt.Errorf("claude-code: failed to generate CLAUDE.md: %w", err)
	}

	return nil
}

func (t *ClaudeCodeTranslator) AgentType() AgentType {
	return AgentClaudeCode
}
