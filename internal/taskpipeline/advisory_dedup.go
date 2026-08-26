package taskpipeline

import (
	"strconv"
)

// advisory_dedup.go — verify 重试下 advisory finding 的同文重发抑制（2026-08 噪音审计）。
//
// 背景：cheat-scan / unused-scan 在每次 task-verify 重扫任务 diff；修复-重试循环里同一
// finding 会逐字重发给 agent（证据：Translate(method) 8 次、comment-only-fix=2 同任务 5
// 次）。本文件提供 finding 指纹（ruleID|file:line，unused-scan 附 symbol）与 per-task 已
// 报告集合（TaskState.ReportedFindings）的过滤助手：指纹已在集合中的 finding 不再出现在
// stderr advisory 里；新 finding（含旧 finding 消失后出现的不同指纹）照常报告。
//
// 边界：checklog 审计条目不受影响——每次 verify 仍记录完整扫描结果（Passed/Detail 反映
// 当次真实扫描），抑制的只是 agent 面向的重复提醒。行号指纹的已知取舍：代码编辑使同一
// 问题行号漂移时会当作新 finding 重报一次（可接受的保守方向——漏报比重报危险）。
//
// advisory_dedup.go — suppression of verbatim advisory re-emission across verify retries
// (2026-08 noise audit).
//
// Background: cheat-scan / unused-scan re-scan the task diff on every task-verify; in a
// fix-retry loop the same finding is re-emitted to the agent verbatim (evidence:
// Translate(method) 8 times, comment-only-fix=2 five times on one task). This file
// provides finding fingerprints (ruleID|file:line, plus symbol for unused-scan) and a
// filter over the per-task already-reported set (TaskState.ReportedFindings): findings
// whose fingerprint is in the set drop out of the stderr advisory; new findings
// (including different fingerprints appearing after an old one vanished) report normally.
//
// Boundary: the checklog audit entry is unaffected — every verify still records the full
// scan result (Passed/Detail reflect the actual run); only the agent-facing repeat nudge
// is suppressed. Known trade-off of line-number fingerprints: an edit shifting the same
// problem's line re-reports it once as "new" (the acceptable conservative direction —
// under-reporting is more dangerous than re-reporting).

// cheatFindingKey 是 cheat-scan finding 的指纹：规则 ID + 文件：行。
//
// cheatFindingKey is a cheat-scan finding's fingerprint: rule ID + file:line.
func cheatFindingKey(c CheatFinding) string {
	return string(c.Pattern) + "|" + c.File + ":" + strconv.Itoa(c.Line)
}

// unusedFindingKey 是 unused-scan finding 的指纹：规则 ID + 文件：行 + 符号名（符号是
// 该 finding 的身份本体——同一行重命名/换定义不应被旧指纹误抑制）。
//
// unusedFindingKey is an unused-scan finding's fingerprint: rule ID + file:line + symbol
// (the symbol IS the finding's identity — a rename or a different definition landing on
// the same line must not be mis-suppressed by the old fingerprint).
func unusedFindingKey(u UnusedFinding) string {
	return string(u.Pattern) + "|" + u.File + ":" + strconv.Itoa(u.Line) + "|" + u.Symbol
}

// filterUnreported 把 keys 分成「本任务尚未报告」的新指纹，并把新指纹追加进
// state.ReportedFindings（返回 changed=true 表示集合有新增，调用方负责持久化 state）。
// 顺序保持输入顺序。state 为 nil 时全部视为新（调用方无从持久化，退化为不去重）。
//
// filterUnreported splits keys into not-yet-reported fingerprints for this task and
// appends the new ones to state.ReportedFindings (changed=true signals the caller to
// persist state). Input order is preserved. A nil state treats everything as new (the
// caller cannot persist anyway — degrade to no dedup).
func filterUnreported(state *TaskState, keys []string) (fresh []string, changed bool) {
	reported := map[string]bool{}
	if state != nil {
		for _, k := range state.ReportedFindings {
			reported[k] = true
		}
	}
	for _, k := range keys {
		if reported[k] {
			continue
		}
		reported[k] = true // 同批内重复指纹只报一次
		fresh = append(fresh, k)
	}
	if state != nil && len(fresh) > 0 {
		state.ReportedFindings = append(state.ReportedFindings, fresh...)
		changed = true
	}
	return fresh, changed
}

// keySet 把指纹列表转成查找集合（advisory 渲染时按指纹过滤 finding）。
//
// keySet turns a fingerprint list into a lookup set (the advisory render filters
// findings by fingerprint).
func keySet(keys []string) map[string]bool {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return set
}

// suppressNote 渲染「另有 N 处已报告过」的尾部说明（0 时返 ""）。
//
// suppressNote renders the "N more already reported" tail note ("" when 0).
func suppressNote(n int) string {
	if n <= 0 {
		return ""
	}
	return "（另 " + strconv.Itoa(n) + " 处已在本任务此前 verify 报告，不再重复）"
}

// allReportedNote 渲染「全部已报告过」的单行说明。
//
// allReportedNote renders the single-line "all already reported" note.
func allReportedNote() string {
	return "（均已在本任务此前 verify 报告过，不再逐条重复）"
}

// dedupSuffix renders the checklog Detail suffix breaking a scan total down into
// fresh vs suppressed findings ("" on a clean scan or when everything is
// fresh — first scans need no annotation). "suppressed" covers both findings
// reported by an earlier verify AND same-batch duplicate fingerprints
// (filterUnreported collapses both). Without it, repeated FAIL entries from
// re-scanning an unchanged diff are indistinguishable from genuinely new hits in the
// audit trail (2026-08 evidence: 30 cheat-scan FAILs in one week were re-scans of ~9
// distinct findings). The agent-facing advisory was already deduped; this annotates
// the audit side with the same breakdown — the entry still records the full truth.
//
// dedupSuffix 渲染 checklog Detail 后缀，把扫描总数拆成新发现 vs 被抑制
//（干净扫描或全部为新时返回 ""——首次扫描无需标注）。「suppressed」兼指
// 此前 verify 已报告的 finding 与同批次内的重复指纹（filterUnreported 对两者
// 都折叠）。没有它，审计留痕里重扫同一 diff 产生的重复 FAIL 条目与真正的新
// 命中无从区分（2026-08 证据：一周 30 条 cheat-scan FAIL 实为 ~9 个不同
// finding 的重扫）。agent 面向的 advisory 已去重，此后缀把同样的拆分标注到
// 审计侧——条目仍记录完整真相。
func dedupSuffix(total, fresh int) string {
	if total == 0 || fresh == total {
		return ""
	}
	return "; new=" + strconv.Itoa(fresh) + ", suppressed=" + strconv.Itoa(total-fresh)
}
