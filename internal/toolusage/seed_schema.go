package toolusage

// seed_schema.go — ToolCall 的序列化 schema 种子（compat 面 5 消费，经 cli
// 注入到 compat.SeedToolCallForSchema 接缝）。

import (
	"time"

	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

// SeedToolCallForSchema 返回全填充 ToolCall（schema 键提取用，值无意义）。
//
// SeedToolCallForSchema returns a fully-populated ToolCall for schema key extraction.
func SeedToolCallForSchema() any {
	return ToolCall{
		ID: "id", ToolName: "Bash", ToolInput: "in", InputLen: 2, EstTokens: 1,
		TaskRef: "t", SessionID: "s", Timestamp: time.Now(),
		Stamp: nodestamp.Stamp{NodeID: "n", Seq: 1},
	}
}
