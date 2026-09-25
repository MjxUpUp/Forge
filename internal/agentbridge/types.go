// Package agentbridge translates Forge quality config into the native formats of
// multiple AI coding agents (Claude Code, Cursor, Copilot, Windsurf).
//
// Package agentbridge 把 Forge 质量配置翻译成多种 AI coding agent（Claude Code、
// Cursor、Copilot、Windsurf）的原生格式。
package agentbridge

import (
	"github.com/MjxUpUp/Forge/internal/protocol"
)

// AgentType identifies a supported AI coding agent.
//
// AgentType 标识一个受支持的 AI coding agent。
type AgentType string

const (
	AgentClaudeCode AgentType = "claude-code"
	AgentCursor     AgentType = "cursor"
	AgentCopilot    AgentType = "copilot"
	AgentWindsurf   AgentType = "windsurf"
	AgentCodex      AgentType = "codex"
	AgentOpencode   AgentType = "opencode"
	AgentCline      AgentType = "cline"
	AgentKimi       AgentType = "kimi"
	AgentCodeBuddy  AgentType = "codebuddy"
	AgentReasonix   AgentType = "reasonix"
	AgentDsh        AgentType = "dsh"
	AgentZcode      AgentType = "zcode"
)

// Translator converts Forge config into the native format of a specific agent.
//
// Translator 把 Forge config 转成特定 agent 的原生格式。
type Translator interface {
	// Translate generates or updates the agent's config files.
	//
	// Translate 生成或更新该 agent 的 config 文件。
	Translate(projectDir string, input *TranslationInput) error
	// AgentType returns the agent identifier.
	//
	// AgentType 返回 agent 标识符。
	AgentType() AgentType
}

// TranslationInput holds the Forge config to be translated.
//
// TranslationInput 持有待翻译的 Forge 配置。
type TranslationInput struct {
	Protocol  *protocol.Protocol
	HookNames []string
}
