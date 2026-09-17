package taskpipeline

// outcome.go — 纪律优先度量轴（discipline-first-gates 2026-09 P1-A）：失败门禁
// 条目按「谁先披露」分类。discovery=门禁是第一披露点（第一防线缺位）；
// confirmation=第一防线信号已送达但未行动（纪律债兑现）。此处只提供查询助手，
// 盖章点在各 checkVerify* 的失败分支——通过与历史条目永不分类。

import (
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// nudgeDeliveredForTask 报告该 task 内是否存过**已送达**的 test-nudge（Delivered
// 章非 nil 且为 true）。它是 test-coverage-gate 失败时 outcome 分类的唯一输入：
// 送达过的 nudge 在先 → confirmation（agent 被告知过）；否则 → discovery（门禁是
// 第一披露点）。nil/false 都不算送达——把死 advisory 通道计成「已告知」会把
// 第一防线缺位洗白成纪律未执行，度量随之失真（与 Entry.Delivered 的 nil 语义
// 契约一致）。读失败按无信号处理（fail-open 不阻断门禁，只让 outcome 落在
// discovery 侧——保守方向：不虚增 confirmation）。
func nudgeDeliveredForTask(root, taskRef string) bool {
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Check == checklog.CheckTestNudge && e.Delivered != nil && *e.Delivered {
			return true
		}
	}
	return false
}
