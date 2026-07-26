// Package agentbridge 把 Forge 质量配置翻译成多种 AI coding agent（Claude Code、
// Cursor、Copilot、Windsurf）的原生格式。
package agentbridge

import (
	"github.com/MjxUpUp/Forge/internal/protocol"
)

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
)

// Translator 把 Forge config 转成特定 agent 的原生格式。
type Translator interface {
	// Detect 在该 agent 的 config 目录/文件存在时返回 true。
	Detect(projectDir string) bool
	// Translate 生成或更新该 agent 的 config 文件。
	Translate(projectDir string, input *TranslationInput) error
	// AgentType 返回 agent 标识符。
	AgentType() AgentType
}

// TranslationInput 持有待翻译的 Forge 配置。
type TranslationInput struct {
	Protocol  *protocol.Protocol
	HookNames []string
}
