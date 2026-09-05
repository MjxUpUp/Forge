package checklog

// seed_schema.go — 序列化 schema 的种子形态（compat 面 5 消费）：返回全填充
// Entry（每字段非零值），供 compat.schemaKeys 提取键集合。Marshal 后的键路径
// 即对下面的序列化契约面（强承诺：只增不删不改名）。

import "time"

// SeedEntryForSchema 返回全填充 Entry（schema 键提取用，值无意义）。
//
// SeedEntryForSchema returns a fully-populated Entry for schema key extraction.
func SeedEntryForSchema() any {
	e := Entry{
		Check: "x", Passed: true, Checked: true, ToolName: "t", TaskRef: "r",
		SessionID: "s", Detail: "d", Level: LevelPass, Source: EvidenceDeterministic,
		RecordedAt: time.Now(), Meta: map[string]string{"k": "v"},
	}
	delivered := true
	e.Delivered = &delivered
	return e
}
