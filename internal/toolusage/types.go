// Package toolusage records AI agent tool calls, used for quality scoring.
//
// Package toolusage 记录 AI agent 的 tool 调用，用于质量评分。
package toolusage

import (
	"time"

	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

// ToolCall records a single AI agent tool call.
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
	// Stamp carries machine attribution (node_id/seq/ts_hlc/sig), filled by Record via nodestamp.Next.
	//
	// Stamp 携带机器归因（node_id/seq/ts_hlc/sig），由 Record 经 nodestamp.Next 落章——
	// 存量行与 fail-open 时为零值。拍平进本对象。注意：Record 里在 ID 计算之后落章，
	// 稳定 sha1 ID 永不漂移。
	nodestamp.Stamp
}

// maxToolInputLen 是 tool_input 存储的截断上限。
const maxToolInputLen = 500
