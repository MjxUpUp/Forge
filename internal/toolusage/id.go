package toolusage

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// computeID derives a stable 12-char hex identifier from a tool call's identity fields (task + session + timestamp + tool name).
// Two calls collide only when all four fields are identical, i.e. they are the same logical call;
// different timestamps never collide. This ID lets an anti-pattern violation point back to the exact ToolCall that triggered it, so
// forge trace can annotate the specific call behind a violation rather than only the tool name.
//
// computeID 从一个 tool call 的身份字段（task + session + timestamp + tool name）
// 派生稳定的 12 字符 hex 标识符。两次调用四处全相同仅当它们是同一个逻辑调用才会碰撞；
// 时间戳不同则永不碰撞。该 ID 让 anti-pattern 违规能指回触发它的确切 ToolCall，使
// forge trace 能标注违规的具体调用，而非只指出 tool 名。
func computeID(c ToolCall) string {
	h := sha1.New()
	h.Write([]byte(c.TaskRef + "|" + c.SessionID + "|" + c.Timestamp.Format(time.RFC3339Nano) + "|" + c.ToolName))
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// ensureID returns the call's existing ID; if missing, it computes one. Toollog entries written before the ID mechanism existed
// deserialize with an empty ID — this function backfills it at load time, so trace can reference violations even for historical tasks.
//
// ensureID 返回 call 现有 ID；缺失则计算一个。在 ID 机制存在之前写入的 Toollog 条目
// 反序列化后 ID 为空——本函数在加载时回填，让 trace 即便对历史 task 也能引用违规。
func ensureID(c ToolCall) string {
	if c.ID != "" {
		return c.ID
	}
	return computeID(c)
}
