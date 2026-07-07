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

	// Generate .mcp.json — expose forge's 14 MCP tools (task resume/decide/attach,
	// gates, experience) to the agent so it calls forge structurally
	// instead of the user typing CLI. Idempotent merge (see mcpconfig.go).
	// 纯函数：永远 merge 写 forge server；project-level 重复清理由命令层
	// dedupeProjectLevelIfPlugin 统一做。注：GenerateSettings 加了 plugin guard
	// （因其 "写 hooks 然后立即 strip" 模式在进程中断时可能损坏 settings.local.json），
	// 但 writeClaudeMCP 不需要——.mcp.json 是纯 forge 管理文件，无用户数据，中断无危害。
	if err := writeClaudeMCP(projectDir); err != nil {
		return fmt.Errorf("claude-code: failed to generate .mcp.json: %w", err)
	}

	return nil
}

func (t *ClaudeCodeTranslator) AgentType() AgentType {
	return AgentClaudeCode
}
