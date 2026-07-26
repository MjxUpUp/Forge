// Package toolusage records AI agent tool calls, used for quality scoring.
//
// Package toolusage 记录 AI agent 的 tool 调用，用于质量评分。
package toolusage

import "time"

// ToolCall records a single AI agent tool call.
// Stored in DataDir/toollog.jsonl — one JSON object per line. The activity-ratio gate (task-verify)
// and forge trace consume this data; it no longer participates in scoring.
//
// ToolCall 记录 AI agent 的一次 tool 调用。
// 存于 DataDir/toollog.jsonl——每行一个 JSON 对象。activity-ratio gate（task-verify）
// 与 forge trace 消费本数据；它不再参与评分。
type ToolCall struct {
	ID        string    `json:"id,omitempty"` // 身份字段的稳定 sha1；forge trace 显示为 [#id]
	ToolName  string    `json:"tool_name"`
	ToolInput string    `json:"tool_input,omitempty"` // 截断到 500 字符
	InputLen  int       `json:"input_len,omitempty"`  // 原始 tool_input 字节数（截断前），token 估算依据
	EstTokens int       `json:"est_tokens,omitempty"` // 估算 token（≈rune/3），loop 成本代理——非精确账单
	TaskRef   string    `json:"task_ref,omitempty"`
	SessionID string    `json:"session_id,omitempty"` // Claude Code session — isolates concurrent sessions
	Timestamp time.Time `json:"timestamp"`
}

// maxToolInputLen is the truncation cap for stored tool_input.
//
// maxToolInputLen 是 tool_input 存储的截断上限。
const maxToolInputLen = 500
