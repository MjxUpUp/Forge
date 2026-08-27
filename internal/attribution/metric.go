package attribution

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// metricInterval throttles Stop-time coverage recording: every Stop of every session
// would flood checklog with near-identical snapshots; 10 minutes keeps the T2 bash-infer
// coverage signal representative without bloat (same throttling family as task-verify's
// .task-verify-throttle.last).
//
// metricInterval 节流 Stop 时覆盖率落章：每会话每个 Stop 都写会把 checklog 灌满近乎
// 相同的快照；10 分钟足以让 T2 bash-infer 覆盖率信号有代表性而不膨胀（与
// task-verify 的 .task-verify-throttle.last 同族节流）。
const metricInterval = 10 * time.Minute

// MetricSnapshot is the structured coverage payload (rides checklog Meta).
//
// MetricSnapshot 是结构化覆盖率载荷（走 checklog Meta）。
type MetricSnapshot struct {
	Attributed int
	Orphans    int
	Rate       float64
}

// RecordStopMetric reconciles the workspace and records one observation-class checklog
// entry carrying attribution coverage (multi-task-concurrency §6: the T2 spike decides
// bash-infer's fate by measured coverage — this is the measurement). Throttled per
// workspace to metricInterval via a last-run marker. Silent on every failure: metrics
// must never break the Stop path.
//
// RecordStopMetric 对账 workspace 并落一条 observation 类 checklog 条目，携带归属
// 覆盖率（multi-task-concurrency §6：T2 spike 靠实测覆盖率决定 bash-infer 去留——
// 这就是那把尺子）。经 last-run 标记按 workspace 节流到 metricInterval。一切失败
// 静默：度量绝不能打断 Stop 路径。
func RecordStopMetric(root, taskRef string) {
	if root == "" {
		return
	}
	marker := filepath.Join(forgedata.DataDirFor(root), ".attribution-metric-"+worktree.ID(root))
	if info, err := os.Stat(marker); err == nil && time.Since(info.ModTime()) < metricInterval {
		return
	}
	v := Reconcile(root)
	attributed := 0
	for _, fs := range v.BySession {
		attributed += len(fs)
	}
	if attributed == 0 && len(v.Orphans) == 0 {
		return // 无变更不落章：覆盖率只在有故事时存在
	}
	_ = os.MkdirAll(filepath.Dir(marker), 0o755)
	if f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_ = f.Close()
		_ = os.Chtimes(marker, time.Now(), time.Now())
	}
	detail := "归属对账: " + strconv.Itoa(attributed) + " attributed / " +
		strconv.Itoa(len(v.Orphans)) + " orphan（覆盖率 " +
		strconv.FormatFloat(v.AttributionRate()*100, 'f', 0, 64) + "%）"
	entry := &checklog.Entry{
		Check:   checklog.CheckAttribution,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelAdvisory,
		TaskRef: taskRef,
		Detail:  detail,
		Meta: map[string]string{
			checklog.MetaKeyAttributionAttributed: strconv.Itoa(attributed),
			checklog.MetaKeyAttributionOrphans:    strconv.Itoa(len(v.Orphans)),
			checklog.MetaKeyAttributionRate:       strconv.FormatFloat(v.AttributionRate(), 'f', 4, 64),
		},
	}
	_ = checklog.Record(root, entry)
}
