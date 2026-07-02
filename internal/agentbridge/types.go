// Package agentbridge translates Forge quality configuration into native
// formats for multiple AI coding agents (Claude Code, Cursor, Copilot, Windsurf).
package agentbridge

import (
	"github.com/MjxUpUp/Forge/internal/pipeline"
	"github.com/MjxUpUp/Forge/internal/protocol"
)

// AgentType identifies a supported AI coding agent.
type AgentType string

const (
	AgentClaudeCode AgentType = "claude-code"
	AgentCursor     AgentType = "cursor"
	AgentCopilot    AgentType = "copilot"
	AgentWindsurf   AgentType = "windsurf"
	AgentCodex      AgentType = "codex"
	AgentOpencode   AgentType = "opencode"
	AgentPi         AgentType = "pi"
)

// Translator converts Forge config to a specific agent's native format.
type Translator interface {
	// Detect returns true if this agent's config directory/file exists.
	Detect(projectDir string) bool
	// Translate generates or updates the agent's config files.
	Translate(projectDir string, input *TranslationInput) error
	// AgentType returns the agent identifier.
	AgentType() AgentType
}

// TranslationInput holds the Forge configuration to translate.
type TranslationInput struct {
	Protocol  *protocol.Protocol
	Pipeline  *pipeline.Pipeline
	HookNames []string
}
