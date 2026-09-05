package checklog

// escape.go — 逃生舱行的构造助手（mechanism-hardening P1-2，豁免治理学落地）。
// 依据：FSE 2025 实证 50.8% 的豁免是幽灵豁免（不抑制任何告警）且只增不减；
// 好豁免八要素中 forge 缺 reason（结构化原因）与 owner（谁豁免的）两项。
// 本助手把两字段经既有 Entry.Meta 通道落进行——零 schema 变更（对下强承诺：
// 序列化键只增不删，Meta 是既有 map）。
//
// reason 语义：枚举标签（可聚合分析）+可选自由文本；owner 语义：per-task
// override → 任务 ref；env 形式 → "env"（agent 场景下委托任务即 owner，
// env 无任务上下文时诚实标 env）。

// 逃生舱 reason 枚举（FSE 2025 动机分析的分类 + forge 场景扩展）。
const (
	EscapeReasonOverride    = "per-task override" // 显式 per-task 关闭（forge task override）
	EscapeReasonEnv         = "env"               // 环境变量形式（CI/测试/一次性）
	EscapeReasonFalsePos    = "false-positive"    // 操作者判定为误报
	EscapeReasonOutOfScope  = "out-of-scope"      // 检查不适用本任务场景
	EscapeReasonTimebox     = "timebox"           // 时间盒内暂时跳过
	EscapeReasonUpstream    = "upstream-issue"    // 上游/宿主问题所致
	EscapeReasonUnspecified = "unspecified"       // 未声明（默认——聚合时单独计数，推动补 reason）
)

// EscapeHatchEntry 构造一条带 reason/owner 元数据的逃生舱行。
// gate = 被豁免的门禁名（如 "doc-gate"）；owner = 任务 ref 或 "env"；
// reason 枚举 + detail 自由文本。调用方负责 Record。
//
// EscapeHatchEntry constructs one escape-hatch row carrying reason/owner
// metadata (mechanism-hardening P1-2).
func EscapeHatchEntry(gate, reason, owner, detail string) *Entry {
	return &Entry{
		Check:   CheckEscapeHatch,
		Passed:  true,
		Checked: true,
		Level:   LevelWarn,
		Detail:  detail,
		Meta: map[string]string{
			"escape.gate":   gate,
			"escape.reason": reason,
			"escape.owner":  owner,
		},
	}
}

// EscapeGateOf 从行提取被豁免门禁名（无元数据的旧行返回 ""——聚合侧按
// Detail 散文兜底或跳过，不误判）。
//
// EscapeGateOf extracts the escaped gate name from row metadata.
func EscapeGateOf(e *Entry) string {
	if e == nil || e.Meta == nil {
		return ""
	}
	return e.Meta["escape.gate"]
}

// EscapeOwnerOf 从行提取 owner（旧行返回 ""）。
//
// EscapeOwnerOf extracts the owner from row metadata.
func EscapeOwnerOf(e *Entry) string {
	if e == nil || e.Meta == nil {
		return ""
	}
	return e.Meta["escape.owner"]
}

// EscapeReasonOf 从行提取 reason 枚举（旧行返回 EscapeReasonUnspecified 的
// 缺省——聚合时区分"没记"与"未声明"，两者都归 unspecified 桶）。
//
// EscapeReasonOf extracts the reason enum from row metadata.
func EscapeReasonOf(e *Entry) string {
	if e == nil || e.Meta == nil || e.Meta["escape.reason"] == "" {
		return EscapeReasonUnspecified
	}
	return e.Meta["escape.reason"]
}
