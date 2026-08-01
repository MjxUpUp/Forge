package agentbridge

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/skillgen"
)

// ClaudeCodeTranslator wires claude-code at USER level: hooks go into
// ~/.claude/settings.json (skipped when the forge plugin is user-level installed),
// the quality skill into ~/.claude/skills/forge-quality/, and the protocol section
// into ~/.claude/CLAUDE.md (backup+append). Project-level claude assets are only
// written by `forge init --project` (team mode), which bypasses this translator.
//
// ClaudeCodeTranslator 在用户级接线 claude-code：hooks 进 ~/.claude/settings.json
// （forge plugin 已 user-level 安装时跳过），quality skill 进
// ~/.claude/skills/forge-quality/，协议段进 ~/.claude/CLAUDE.md（备份+追加）。
// 项目级 claude 资产只由 `forge init --project`（团队模式）写，该路径绕过本 translator。
type ClaudeCodeTranslator struct{}

func (t *ClaudeCodeTranslator) Translate(projectDir string, input *TranslationInput) error {
	// User-level settings.json — only when the plugin is NOT user-level installed.
	// When the plugin is installed, user-level plugin.json already registers ForgeHookSpec
	// machine-wide; writing user-level settings hooks again is redundant.
	//
	// 用户级 settings.json——仅在 plugin 未 user-level 安装时。plugin 已安装时，
	// user-level plugin.json 已全机器注册 ForgeHookSpec，再写是冗余。
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateUserSettings(); err != nil {
			return fmt.Errorf("claude-code: failed to generate user-level settings: %w", err)
		}
	}

	// User-level quality SKILL.md.
	//
	// 用户级 quality SKILL.md
	if input.Protocol != nil {
		if err := skillgen.GenerateUserQualitySkill(input.Protocol); err != nil {
			return fmt.Errorf("claude-code: failed to generate user-level quality skill: %w", err)
		}
	}

	// User-level CLAUDE.md (backup+append, conditional activation preamble).
	//
	// 用户级 CLAUDE.md（备份+追加，条件激活前置）。
	if err := skillgen.GenerateUserClaudeMD(); err != nil {
		return fmt.Errorf("claude-code: failed to update user-level CLAUDE.md: %w", err)
	}

	return nil
}

func (t *ClaudeCodeTranslator) AgentType() AgentType {
	return AgentClaudeCode
}
