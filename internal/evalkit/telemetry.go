package evalkit

// telemetry.go — Track B 的 C4/C7 遥测聚合（docs/design/forge-evaluation-system.md
// §三）：从既有 checklog/toollog/registry 聚合摩擦与健康指标。零新增采集——数据已在
// 盘上。所有比率带 Wilson 区间；样本量低于字典 min_samples 只出 insufficient，不出
// 结论（0 与无数据是不同事实）。
//
// telemetry.go — Track B C4/C7 telemetry aggregation: friction & health metrics
// aggregated from existing checklog/toollog/registry. Zero new collection — the
// data is already on disk. All rates carry Wilson intervals; below the
// dictionary's min_samples a metric reports insufficient, never a conclusion
// (0 and no-data are different facts).

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// RateValue is one computed rate metric with its Wilson interval and
// sufficiency verdict.
//
// RateValue 是一个带 Wilson 区间与充分性判定的比率指标值。
type RateValue struct {
	MetricID     string  `json:"metric_id"`
	Numerator    int     `json:"numerator"`
	Denominator  int     `json:"denominator"`
	Value        float64 `json:"value"`
	Lo           float64 `json:"lo"`
	Hi           float64 `json:"hi"`
	Insufficient bool    `json:"insufficient"`
	Note         string  `json:"note"`
	MisuseNote   string  `json:"misuse_note"`
}

// SignatureAuditSummary counts the per-verdict audit-row signature scan.
//
// SignatureAuditSummary 按裁决统计审计行验签扫描。
type SignatureAuditSummary struct {
	Valid          int `json:"valid"`
	Forged         int `json:"forged"`
	ForeignNode    int `json:"foreign_node"`
	UnsignedLegacy int `json:"unsigned_legacy"`
}

// TelemetryReport is one aggregation snapshot (immutable once produced).
//
// TelemetryReport 是一份聚合快照（产出后不可变）。
type TelemetryReport struct {
	GeneratedAt time.Time      `json:"generated_at"`
	RepoRoot    string         `json:"repo_root"`
	Entries     int            `json:"entries"`      // 聚合的 checklog 条目数
	Sessions    int            `json:"sessions"`     // 去重 session 数（空 session 不计）
	Weeks       []WeekRate     `json:"weeks"`        // 门禁触发的周趋势
	Rates       []RateValue    `json:"rates"`        // 汇总比率
	DictionaVersion int        `json:"dictionary_version"`
	SignatureAudit  SignatureAuditSummary `json:"signature_audit"`
}

// WeekRate is one weekly bucket of gate-fire counts (blocked/advisory).
//
// WeekRate 是一周的门禁触发计数桶（blocked/advisory）。
type WeekRate struct {
	WeekStart string `json:"week_start"` // 周一日期 YYYY-MM-DD
	Blocked   int    `json:"blocked"`
	Advisory  int    `json:"advisory"`
}

// Aggregate computes the C4/C7 telemetry snapshot for one project root.
// Unavailable data sources yield insufficient metrics — never zeros, never
// guesses (wait_turns has no producer in v1 and reports as such).
//
// Aggregate 计算一个项目根的 C4/C7 遥测快照。不可用的数据源产出 insufficient
// 指标——绝不出 0、绝不猜（wait_turns 在 v1 无生产者，如实报告）。
func Aggregate(root string, dict *Dictionary, now time.Time) (*TelemetryReport, error) {
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return nil, fmt.Errorf("evalkit: 读取 checklog 失败: %w", err)
	}
	rep := &TelemetryReport{GeneratedAt: now, RepoRoot: root, Entries: len(entries), DictionaVersion: dict.Version}

	// 审计行验签扫描（checklog.AuditEntry）：可归属本机的行上验签失败 = 伪造。
	// 他机行（foreign）与历史行（legacy）单列计数，绝不混入伪造。
	forged, foreign, legacy := 0, 0, 0
	for i := range entries {
		switch checklog.AuditEntry(&entries[i]) {
		case checklog.VerdictForged:
			forged++
		case checklog.VerdictForeignNode:
			foreign++
		case checklog.VerdictUnsignedLegacy:
			legacy++
		}
	}
	rep.SignatureAudit = SignatureAuditSummary{Forged: forged, ForeignNode: foreign, UnsignedLegacy: legacy}

	sessions := map[string]bool{}
	gateBlocked, gateAdvisory := 0, 0
	escapeCount := 0
	offCount := 0
	verifyPass, verifyTotal := 0, 0
	weekIdx := map[string]*WeekRate{}

	for i := range entries {
		e := &entries[i]
		if e.SessionID != "" {
			sessions[e.SessionID] = true
		}
		switch {
		case e.Check == checklog.CheckEscapeHatch:
			escapeCount++
		case e.Check == checklog.CheckTakeoverPolicy && strings.Contains(e.Detail, "takeover off"):
			offCount++
		case e.Check == checklog.CheckTaskVerify || e.Check == checklog.CheckTaskComplete:
			if e.Checked {
				verifyTotal++
				if e.Passed {
					verifyPass++
				}
			}
		}
		lvl := e.EffectiveLevel()
		if lvl == checklog.LevelBlocked || lvl == checklog.LevelAdvisory {
			ws := weekStart(e.RecordedAt)
			bucket, ok := weekIdx[ws]
			if !ok {
				bucket = &WeekRate{WeekStart: ws}
				weekIdx[ws] = bucket
			}
			if lvl == checklog.LevelBlocked {
				bucket.Blocked++
				gateBlocked++
			} else {
				bucket.Advisory++
				gateAdvisory++
			}
		}
	}
	rep.Sessions = len(sessions)
	for _, b := range weekIdx {
		rep.Weeks = append(rep.Weeks, *b)
	}
	sort.Slice(rep.Weeks, func(i, j int) bool { return rep.Weeks[i].WeekStart < rep.Weeks[j].WeekStart })

	// 逐指标计算：字典里存在的才计算并携带其误用注记；字典里没有的指标不产出
	// （字典是指标面的单一真相源）。
	rate := func(id string, num, den int) {
		if m, ok := dict.Find(id); ok {
			rep.Rates = append(rep.Rates, newRateValue(m, num, den))
		}
	}
	count := func(id string, n int) {
		if m, ok := dict.Find(id); ok {
			rep.Rates = append(rep.Rates, newCountValue(m, n))
		}
	}
	rate("gate_escape_rate", escapeCount, rep.Sessions)
	count("off_churn", offCount)
	rate("self_gate_pass_rate", verifyPass, verifyTotal)
	// wait_turns：v1 无生产数据源——如实 insufficient（0 与无数据是不同事实）。
	if m, ok := dict.Find("wait_turns"); ok {
		rep.Rates = append(rep.Rates, RateValue{
			MetricID: m.ID, Insufficient: true,
			Note:       "v1 无确认等待数据源（toollog 未记录确认等待轮次）——待宿主 headless 确认事件接入",
			MisuseNote: m.MisuseNote,
		})
	}
	// gate_fire_rate：两周期计数（blocked/advisory 总量）随周趋势呈现。
	if m, ok := dict.Find("gate_fire_rate"); ok {
		rep.Rates = append(rep.Rates, RateValue{
			MetricID: m.ID, Numerator: gateBlocked + gateAdvisory, Denominator: 1,
			Value: float64(gateBlocked + gateAdvisory),
			Note:       fmt.Sprintf("blocked %d / advisory %d（周趋势见 weeks）", gateBlocked, gateAdvisory),
			MisuseNote: m.MisuseNote,
		})
	}
	return rep, nil
}

// newRateValue computes one rate: sample size is the denominator (Wilson
// interval over num/den); below the dictionary floor → insufficient.
//
// newRateValue 计算一个比率：样本量即分母（num/den 的 Wilson 区间）；低于字典
// 下限 → insufficient。
func newRateValue(m *MetricDef, num, den int) RateValue {
	v := RateValue{MetricID: m.ID, Numerator: num, Denominator: den, MisuseNote: m.MisuseNote}
	if den <= 0 {
		v.Insufficient = true
		v.Note = "分母为 0——无数据，不是 0%"
		return v
	}
	if den < m.MinSamples {
		v.Insufficient = true
		v.Note = fmt.Sprintf("样本 %d 低于字典下限 %d", den, m.MinSamples)
		return v
	}
	v.Value = float64(num) / float64(den)
	v.Lo, v.Hi = WilsonInterval(num, den)
	return v
}

// newCountValue computes one count metric: the sample size is the count itself
// (an off_churn of 3 observed 3 times is a sample of 3, not a rate).
//
// newCountValue 计算一个计数指标：样本量即计数本身（观测到 3 次 off_churn 就是
// 3 的样本，不是比率）。
func newCountValue(m *MetricDef, n int) RateValue {
	v := RateValue{MetricID: m.ID, Numerator: n, Denominator: n, Value: float64(n), MisuseNote: m.MisuseNote}
	if n < m.MinSamples {
		v.Insufficient = true
		v.Note = fmt.Sprintf("计数 %d 低于字典下限 %d", n, m.MinSamples)
	}
	return v
}

// weekStart returns the Monday (UTC) of the week containing t, as YYYY-MM-DD.
//
// weekStart 返回 t 所在周的周一（UTC），格式 YYYY-MM-DD。
func weekStart(t time.Time) string {
	u := t.UTC()
	offset := (int(u.Weekday()) + 6) % 7 // 周一=0 … 周日=6
	monday := u.AddDate(0, 0, -offset)
	return monday.Format("2006-01-02")
}

// VerifyAuditRows scans one project's audit rows and returns the per-verdict
// signature summary (no dictionary dependency — usable on any machine).
//
// VerifyAuditRows 扫描一个项目的审计行，返回按裁决的签名统计（不依赖字典——
// 任意机器可用）。
func VerifyAuditRows(root string) (SignatureAuditSummary, error) {
	entries, err := checklog.LoadAllAll(root)
	if err != nil {
		return SignatureAuditSummary{}, fmt.Errorf("evalkit: 读取 checklog 失败: %w", err)
	}
	var sum SignatureAuditSummary
	for i := range entries {
		switch checklog.AuditEntry(&entries[i]) {
		case checklog.VerdictValid:
			sum.Valid++
		case checklog.VerdictForged:
			sum.Forged++
		case checklog.VerdictForeignNode:
			sum.ForeignNode++
		case checklog.VerdictUnsignedLegacy:
			sum.UnsignedLegacy++
		}
	}
	return sum, nil
}

// LoadToolCalls is a thin indirection over toolusage for future wait-turn
// mining; v1 telemetry does not consume it (keeps the import seam explicit so
// the wait_turns producer plugs in without touching callers).
//
// LoadToolCalls 是 toolusage 的薄间接（为将来 wait-turn 挖矿预留的显式接缝）；
// v1 遥测不消费它——wait_turns 的生产者接入时无需改动调用方。
func LoadToolCalls(root string) ([]toolusage.ToolCall, error) {
	return toolusage.LoadAll(root)
}
