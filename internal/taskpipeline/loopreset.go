package taskpipeline

import (
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// LoopResetDetailPrefix is the Detail prefix of the audit row that
// ResetLoopExhaustion lands for a human `--reset-loop` decision (the note
// follows). Single source shared by the writer and every reader
// (IsLoopResetEntry) — a free-text human note forge cannot verify.
//
// LoopResetDetailPrefix 是 ResetLoopExhaustion 为人工 --reset-loop 裁决落审计行时
// 的 Detail 前缀（其后接裁决说明）。写入方与所有识别方（IsLoopResetEntry）共用的
// 单一真相源——这是 forge 无法验证的自供文本。
const LoopResetDetailPrefix = "loop reset by human decision: "

// IsLoopResetEntry reports whether e is the audit row of a human loop reset:
// a passed loop-exhausted row whose Detail carries LoopResetDetailPrefix. The
// blocked exhaustion rows share the check name but are Passed=false.
//
// IsLoopResetEntry 判断 e 是否为人工回环重置的审计行：Passed=true 的
// loop-exhausted 行且 Detail 带 LoopResetDetailPrefix。耗尽拦截行同 check 名但
// Passed=false，不算。
func IsLoopResetEntry(e *checklog.Entry) bool {
	return e != nil && e.Check == checklog.CheckLoopExhausted && e.Passed &&
		strings.HasPrefix(e.Detail, LoopResetDetailPrefix)
}
