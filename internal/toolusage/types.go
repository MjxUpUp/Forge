// Package toolusage records AI agent tool invocations for quality scoring.
package toolusage

import "time"

// ToolCall records a single tool invocation by the AI agent.
// Stored in DataDir/toollog.jsonl — one JSON object per line. The activity-ratio
// gate (task-verify) and `forge trace` consume this; it no longer feeds scoring.
type ToolCall struct {
	ID        string    `json:"id,omitempty"` // stable sha1 of identity fields; shown by `forge trace` as [#id]
	ToolName  string    `json:"tool_name"`
	ToolInput string    `json:"tool_input,omitempty"` // truncated to 500 chars
	InputLen  int       `json:"input_len,omitempty"`  // 原始 tool_input 字节数（截断前），token 估算依据
	EstTokens int       `json:"est_tokens,omitempty"` // 估算 token（≈rune/3），loop 成本代理——非精确账单
	TaskRef   string    `json:"task_ref,omitempty"`
	SessionID string    `json:"session_id,omitempty"` // Claude Code session — isolates concurrent sessions
	Timestamp time.Time `json:"timestamp"`
}

// maxToolInputLen is the truncation limit for tool_input storage.
const maxToolInputLen = 500
