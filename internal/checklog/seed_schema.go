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
	// 内嵌 Stamp 的全部字段填满（对抗审查 should-fix：node_id/seq/ts_hlc/sig
	// 都是承诺面序列化键，零值 omitempty 会漏出键集合）。
	e.Stamp.NodeID = "fnode_x"
	e.Stamp.Seq = 1
	e.Stamp.TsHLC = "t"
	e.Stamp.Sig = "s"
	// 复审发现的遗漏键（channel/forge_version 均为承诺面序列化键）。
	e.Channel = "ch"
	e.ForgeVersion = "v"
	delivered := true
	e.Delivered = &delivered
	return e
}
