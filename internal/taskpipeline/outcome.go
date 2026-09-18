package taskpipeline

// outcome.go — 纪律优先度量轴（discipline-first-gates 2026-09）：失败门禁条目按
// 「谁先披露**该事实**」分类。discovery=门禁是第一披露点（第一防线缺位）；
// confirmation=第一防线信号已送达**同一批文件**而未行动（纪律债兑现）。
// 刻意事实级而非 task 级：早 nudge 兑现后尾段新欠的文件，agent 从未被告知，
// 记 confirmation 会系统性虚高 D3（守护审计 P2-1，2026-09-17）。

import (
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// firstLineHoldsFact 报告该批 missing 文件是否曾被第一防线**点名过**：
// (a) 已送达的 test-nudge（Delivered 章 true）且其 files 集合与 missing 有交集；
// (b) selfcheck-pairing 条目的 missing_list 与 missing 有交集（任意 Passed——
//
//	Passed=false 即自检亲眼见过这批文件；Passed=true 则 missing_list 为空、
//	天然无交集，自检后漂移的新文件正确落 discovery）。
//
// Meta 截断盲区如实接受：nudge/selfcheck 清单 ≤8 截断，>8 时交集可能漏报——
// 方向偏 discovery（保守：不虚增 confirmation）。读失败同向。
func firstLineHoldsFact(root, taskRef string, missing []string) bool {
	if len(missing) == 0 {
		return false
	}
	missingSet := make(map[string]bool, len(missing))
	for _, f := range missing {
		missingSet[f] = true
	}
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return false
	}
	for _, e := range entries {
		switch e.Check {
		case checklog.CheckTestNudge:
			if e.Delivered == nil || !*e.Delivered {
				continue // 未送达（nil/false）不算——死通道不能洗白成「已告知」
			}
			if metaIntersects(e.Meta["files"], missingSet) {
				return true
			}
		case checklog.CheckSelfcheckPairing:
			if metaIntersects(e.Meta["missing_list"], missingSet) {
				return true
			}
		}
	}
	return false
}

// metaIntersects 报告逗号分隔的 Meta 清单与 missing 集合是否有交集。
func metaIntersects(list string, missingSet map[string]bool) bool {
	for _, f := range strings.Split(list, ",") {
		f = strings.TrimSpace(f)
		if f != "" && missingSet[f] {
			return true
		}
	}
	return false
}
