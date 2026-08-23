package health

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
)

func at(hour int) time.Time { return time.Date(2026, 7, 1, hour, 0, 0, 0, time.UTC) }

// conc is shorthand for building a test conclusion: ref/score/grade/strength/low-score dimensions/completion time.
//
// conc 是构造测试结论的简写：ref/score/grade/strength/低分维度/完成时刻。
func conc(ref, grade, strength string, score float64, lowDims []string, t time.Time) act.Conclusion {
	return act.Conclusion{
		TaskRef:       ref,
		Grade:         grade,
		Strength:      strength,
		Score:         score,
		LowDimensions: lowDims,
		CompletedAt:   t,
	}
}

func TestSummarize_Empty(t *testing.T) {
	s := Summarize(nil)
	if s.TotalTasks != 0 {
		t.Errorf(`空切片 TotalTasks=%d want 0`, s.TotalTasks)
	}
	if s.BlindSpotRate != 0 || s.AvgScore != 0 {
		t.Errorf(`空切片应全零值，got rate=%v avg=%v`, s.BlindSpotRate, s.AvgScore)
	}
	if len(s.LowDims) != 0 {
		t.Errorf(`空切片 LowDims=%v want 空`, s.LowDims)
	}
}

func TestSummarize_BlindSpotRateAndDists(t *testing.T) {
	// 4 tasks: 2 Strong, 1 Unverified, 1 Weak → blind-spot rate 50% (2/4).
	//
	// 4 个任务：2 Strong、1 Unverified、1 Weak → 盲区率 50%（2/4）。
	cs := []act.Conclusion{
		conc(`a`, `A`, `Strong`, 95, nil, at(1)),
		conc(`b`, `B`, `Unverified`, 90, []string{`tests`}, at(2)),
		conc(`c`, `A`, `Strong`, 92, []string{`tests`, `scope`}, at(3)),
		conc(`d`, `D`, `Weak`, 60, []string{`scope`, `docs`}, at(4)),
	}
	s := Summarize(cs)
	if s.TotalTasks != 4 {
		t.Fatalf(`TotalTasks=%d want 4`, s.TotalTasks)
	}
	// Average score (95+90+92+60)/4 = 84.25
	//
	// 均分 (95+90+92+60)/4 = 84.25
	if s.AvgScore != 84.25 {
		t.Errorf(`AvgScore=%v want 84.25`, s.AvgScore)
	}
	// Median (90+92)/2 = 91
	//
	// 中位 (90+92)/2 = 91
	if s.MedianScore != 91 {
		t.Errorf(`MedianScore=%v want 91`, s.MedianScore)
	}
	if s.BlindSpotCount != 2 || s.BlindSpotRate != 0.5 {
		t.Errorf(`BlindSpot=%d/%v want 2/0.5（Unverified+Weak）`, s.BlindSpotCount, s.BlindSpotRate)
	}
	if s.GradeDist[`A`] != 2 || s.GradeDist[`B`] != 1 || s.GradeDist[`D`] != 1 {
		t.Errorf(`GradeDist=%+v want A=2 B=1 D=1`, s.GradeDist)
	}
	if s.StrengthDist[`Strong`] != 2 || s.StrengthDist[`Unverified`] != 1 || s.StrengthDist[`Weak`] != 1 {
		t.Errorf(`StrengthDist=%+v want Strong=2 Unverified=1 Weak=1`, s.StrengthDist)
	}
}

func TestSummarize_LowDimsRanked(t *testing.T) {
	// tests appears 3 times (b, c, e), scope 2 times, docs 1 time → descending order tests/scope/docs.
	//
	// tests 出现 3 次（b、c、e），scope 2 次，docs 1 次 → 降序 tests/scope/docs。
	cs := []act.Conclusion{
		conc(`a`, `A`, `Strong`, 95, []string{`tests`}, at(1)),
		conc(`b`, `A`, `Strong`, 95, []string{`tests`, `scope`}, at(2)),
		conc(`c`, `A`, `Strong`, 95, []string{`tests`, `scope`, `docs`}, at(3)),
	}
	s := Summarize(cs)
	if len(s.LowDims) != 3 {
		t.Fatalf(`LowDims=%v want 3 项`, s.LowDims)
	}
	want := []struct {
		dim   string
		count int
	}{{`tests`, 3}, {`scope`, 2}, {`docs`, 1}}
	for i, w := range want {
		if s.LowDims[i].Dimension != w.dim || s.LowDims[i].Count != w.count {
			t.Errorf(`LowDims[%d]=%+v want %s×%d`, i, s.LowDims[i], w.dim, w.count)
		}
	}
}

func TestTrend(t *testing.T) {
	cases := []struct {
		name   string
		scores []int // 按时间序的分数
		want   string
	}{
		{`不足4样本→insufficient`, []int{90, 80}, `insufficient`},
		{`改善`, []int{60, 65, 90, 95}, `improving`},   // 前半 62.5 后半 92.5 → +30
		{`回退`, []int{90, 95, 60, 65}, `regressing`},  // 前半 92.5 后半 62.5 → -30
		{`稳定(差<3)`, []int{90, 91, 90, 91}, `stable`}, // 前半 90.5 后半 90.5
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := make([]act.Conclusion, len(tc.scores))
			for i, sc := range tc.scores {
				cs[i] = conc(`x`, `A`, `Strong`, float64(sc), nil, at(i+1))
			}
			s := Summarize(cs)
			if s.Trend != tc.want {
				t.Errorf(`Trend=%q want %q（earlier=%.1f recent=%.1f）`, s.Trend, tc.want, s.EarlierAvg, s.RecentAvg)
			}
		})
	}
}

func TestSummarize_NudgeCount(t *testing.T) {
	cs := []act.Conclusion{
		conc(`a`, `A`, `Strong`, 95, nil, at(1)),
		{TaskRef: `b`, Grade: `A`, Strength: `Unverified`, Score: 95, RetrospectiveNudge: true, CompletedAt: at(2)},
		{TaskRef: `c`, Grade: `D`, Strength: `Strong`, Score: 60, RetrospectiveNudge: true, CompletedAt: at(3)},
	}
	s := Summarize(cs)
	if s.NudgeCount != 2 {
		t.Errorf(`NudgeCount=%d want 2（b/c 被 nudge）`, s.NudgeCount)
	}
}

// TestSummarizeAt_NudgeRecentWindow pins the dashboard-facing windowed nudge count:
// NudgeRecent counts only nudged conclusions completed within the 14-day window ending
// at `now`; NudgeCount keeps the all-history total (single truth, no information loss).
// This is what stops the "alerts only grow" alarm fatigue — a stale nudge from weeks
// ago no longer lights the panel red, while history stays queryable.
//
// TestSummarizeAt_NudgeRecentWindow 钉住面向 dashboard 的窗口化 nudge 计数：
// NudgeRecent 只数 `now` 前 14 天窗口内完成的 nudge 结论；NudgeCount 保留全量真相
//（不丢信息）。这就是对"告警只增不减"疲劳的止血——数周前的陈旧 nudge 不再点亮面板，
// 历史仍可查。
func TestSummarizeAt_NudgeRecentWindow(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	cs := []act.Conclusion{
		// 窗口内 3 天前：计 recent
		{TaskRef: `recent-nudge`, Strength: `Weak`, Score: 60, RetrospectiveNudge: true, CompletedAt: now.Add(-3 * 24 * time.Hour)},
		// 窗口内 13 天前：计 recent（边界内侧）
		{TaskRef: `edge-nudge`, Strength: `Unverified`, Score: 95, RetrospectiveNudge: true, CompletedAt: now.Add(-13 * 24 * time.Hour)},
		// 窗口外 15 天前：只计全量，不计 recent
		{TaskRef: `stale-nudge`, Strength: `Weak`, Score: 60, RetrospectiveNudge: true, CompletedAt: now.Add(-15 * 24 * time.Hour)},
		// 窗口外 60 天前且未 nudge：两边都不计
		{TaskRef: `stale-clean`, Strength: `Strong`, Score: 95, RetrospectiveNudge: false, CompletedAt: now.Add(-60 * 24 * time.Hour)},
		// 窗口内未 nudge：两边都不计
		{TaskRef: `recent-clean`, Strength: `Strong`, Score: 95, RetrospectiveNudge: false, CompletedAt: now.Add(-1 * 24 * time.Hour)},
	}
	s := SummarizeAt(cs, now)
	if s.NudgeCount != 3 {
		t.Errorf(`NudgeCount=%d want 3（全量：recent-nudge/edge-nudge/stale-nudge）`, s.NudgeCount)
	}
	if s.NudgeRecent != 2 {
		t.Errorf(`NudgeRecent=%d want 2（仅窗口内：recent-nudge/edge-nudge；stale 的不计）`, s.NudgeRecent)
	}
	// Window-edge contract (review 2026-08): the window is (now-Window, now] — a nudge
	// completed EXACTLY at now-14d falls outside (After is strict). Pinned explicitly so
	// the half-open semantics are a tested contract, not an accident.
	//
	// 窗口沿契约（review 2026-08）：窗口是 (now-Window, now]——恰好完成于 now-14d 的
	// nudge 落在窗外（After 为严格比较）。显式钉住半开语义，使其成为被测契约而非偶然。
	exact := SummarizeAt([]act.Conclusion{
		{TaskRef: `exact-edge`, Strength: `Weak`, Score: 60, RetrospectiveNudge: true, CompletedAt: now.Add(-14 * 24 * time.Hour)},
	}, now)
	if exact.NudgeCount != 1 || exact.NudgeRecent != 0 {
		t.Errorf(`窗口沿：恰 14 天前的 nudge 应计全量不计窗口（半开区间 (now-14d, now]），got count=%d recent=%d`, exact.NudgeCount, exact.NudgeRecent)
	}
	// Window-interior boundary: 1ns inside the left edge MUST count — guards against
	// an accidental off-by-one in the other direction (whole-day truncation etc.).
	//
	// 窗口内侧边界：左沿内侧 1ns 必须计入——防反方向的差一错误（整天截断等）。
	inside := SummarizeAt([]act.Conclusion{
		{TaskRef: `inside-edge`, Strength: `Weak`, Score: 60, RetrospectiveNudge: true, CompletedAt: now.Add(-14*24*time.Hour + time.Second)},
	}, now)
	if inside.NudgeRecent != 1 {
		t.Errorf(`窗口内侧 1 秒的 nudge 必须计入 recent，got %d`, inside.NudgeRecent)
	}
}

// TestSummarizeAt_MatchesSummarizeOnNonWindowedFields pins the equivalence contract:
// apart from NudgeRecent (window-scoped), SummarizeAt(cs, anyTime) must produce the
// same aggregates as the legacy Summarize — the window ONLY adds a field, never
// silently redefines existing ones (health CLI keeps consuming the same values).
//
// TestSummarizeAt_MatchesSummarizeOnNonWindowedFields 钉住等价契约：除 NudgeRecent
//（窗口域）外，SummarizeAt 与旧 Summarize 的聚合值必须一致——窗口只增字段，绝不
// 静默重定义既有字段（health CLI 继续消费同样的值）。
func TestSummarizeAt_MatchesSummarizeOnNonWindowedFields(t *testing.T) {
	cs := []act.Conclusion{
		conc(`a`, `A`, `Strong`, 95, nil, at(1)),
		{TaskRef: `b`, Grade: `A`, Strength: `Unverified`, Score: 95, RetrospectiveNudge: true, CompletedAt: at(2)},
		conc(`c`, `D`, `Weak`, 60, []string{`scope`}, at(3)),
	}
	legacy := Summarize(cs)
	windowed := SummarizeAt(cs, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if legacy.TotalTasks != windowed.TotalTasks || legacy.NudgeCount != windowed.NudgeCount ||
		legacy.BlindSpotCount != windowed.BlindSpotCount || legacy.AvgScore != windowed.AvgScore ||
		legacy.MedianScore != windowed.MedianScore {
		t.Errorf(`SummarizeAt 与 Summarize 非窗口字段不一致: legacy=%+v windowed=%+v`, legacy, windowed)
	}
	// 窗口在结论之后 7 天（全部结论落进窗口）→ NudgeRecent == NudgeCount
	future := at(1).Add(7 * 24 * time.Hour)
	windowedAll := SummarizeAt(cs, future)
	if windowedAll.NudgeRecent != windowedAll.NudgeCount {
		t.Errorf(`全窗口时 NudgeRecent(%d) 应 == NudgeCount(%d)`, windowedAll.NudgeRecent, windowedAll.NudgeCount)
	}
}

func TestSummarize_SpanFromEarliestToLatest(t *testing.T) {
	cs := []act.Conclusion{
		conc(`c`, `A`, `Strong`, 95, nil, at(9)), // 乱序传入
		conc(`a`, `A`, `Strong`, 95, nil, at(1)),
		conc(`b`, `A`, `Strong`, 95, nil, at(5)),
	}
	s := Summarize(cs)
	if !s.Span.Earliest.Equal(at(1)) || !s.Span.Latest.Equal(at(9)) {
		t.Errorf(`Span=%s~%s want %s~%s（应按时间取范围，非传入顺序）`,
			s.Span.Earliest, s.Span.Latest, at(1), at(9))
	}
}

func TestSummarize_PhasePassRate(t *testing.T) {
	// t1: api+backend grade A (both pass); t2: api grade C (fail); t3: backend grade B (pass);
	// t4: empty grade does not enter phaseGrades (no grade gatekeeping).
	//
	// t1: api+backend grade A（都通过）；t2: api grade C（不过）；t3: backend grade B（通过）；
	// t4: grade=""不进 phaseGrades（无 grade 守门）。
	cs := []act.Conclusion{
		{TaskRef: `t1`, Grade: `A`, Strength: `Strong`, Score: 95, DesignPhases: []string{`api`, `backend`}, CompletedAt: at(1)},
		{TaskRef: `t2`, Grade: `C`, Strength: `Strong`, Score: 75, DesignPhases: []string{`api`}, CompletedAt: at(2)},
		{TaskRef: `t3`, Grade: `B`, Strength: `Strong`, Score: 85, DesignPhases: []string{`backend`}, CompletedAt: at(3)},
		{TaskRef: `t4`, Grade: ``, Strength: `Strong`, Score: 0, DesignPhases: []string{`api`}, CompletedAt: at(4)},
	}
	s := Summarize(cs)
	if s.PhasePassRate == nil {
		t.Fatal(`PhasePassRate=nil want 非空（有 phase+grade 数据）`)
	}
	// api: t1(A,pass) + t2(C,fail) + t4(no grade,excluded) → 1 pass / 2 total = 0.5
	//
	// api: t1(A,通过) + t2(C,不过) + t4(无grade,不进) → 1 通过 / 2 总数 = 0.5
	if got := s.PhasePassRate[`api`]; got != 0.5 {
		t.Errorf(`api pass_rate=%v want 0.5（A通过/C不过/无grade不进 → 1/2）`, got)
	}
	// backend: t1(A) + t3(B) → 2/2 = 1.0 (both A+B pass)
	//
	// backend: t1(A) + t3(B) → 2/2 = 1.0（A+B 都通过）
	if got := s.PhasePassRate[`backend`]; got != 1.0 {
		t.Errorf(`backend pass_rate=%v want 1.0（A+B 都通过）`, got)
	}
}

func TestSummarize_PhasePassRate_EmptyIsNil(t *testing.T) {
	// Empty slice → PhasePassRate nil (JSON omitempty takes effect, no empty map emitted).
	//
	// 空切片 → PhasePassRate nil（JSON omitempty 生效，不出空 map）。
	if s := Summarize(nil); s.PhasePassRate != nil {
		t.Errorf(`空切片 PhasePassRate=%v want nil`, s.PhasePassRate)
	}
	// No grade at all → phaseGrades never populated → PhasePassRate nil.
	//
	// 全无 grade → phaseGrades 永不填充 → PhasePassRate nil。
	s2 := Summarize([]act.Conclusion{
		{TaskRef: `x`, Grade: ``, Strength: `Strong`, Score: 0, DesignPhases: []string{`api`}, CompletedAt: at(1)},
	})
	if s2.PhasePassRate != nil {
		t.Errorf(`全无 grade PhasePassRate=%v want nil（无 grade 守门）`, s2.PhasePassRate)
	}
}

// TestSummarize_EmptyStrengthGuard: an empty Strength must not be counted into a nameless
// bucket of StrengthDist, nor into BlindSpotCount — same non-empty guard as Grade. The write
// side (act.BuildConclusion) never produces an empty Strength, so this only defends against
// hand-crafted/legacy conclusions.
//
// TestSummarize_EmptyStrengthGuard：空 Strength 不得落入 StrengthDist 的无名桶，也不得
// 计入 BlindSpotCount——与 Grade 同款非空守卫。写入侧（act.BuildConclusion）不会产出
// 空 Strength，此守卫只防手工构造/历史脏数据。
func TestSummarize_EmptyStrengthGuard(t *testing.T) {
	cs := []act.Conclusion{
		conc(`a`, `A`, `Strong`, 95, nil, at(1)),
		conc(`b`, `B`, ``, 90, nil, at(2)), // 空 Strength：不进桶、不计盲区
	}
	s := Summarize(cs)
	if _, ok := s.StrengthDist[``]; ok {
		t.Errorf(`空 Strength 不得落入无名桶，StrengthDist=%v`, s.StrengthDist)
	}
	if s.StrengthDist[`Strong`] != 1 {
		t.Errorf(`StrengthDist[Strong]=%d want 1`, s.StrengthDist[`Strong`])
	}
	if s.BlindSpotCount != 0 {
		t.Errorf(`BlindSpotCount=%d want 0（空 Strength 不算盲区）`, s.BlindSpotCount)
	}
	if s.TotalTasks != 2 {
		t.Errorf(`TotalTasks=%d want 2（空 Strength 任务仍计入总数）`, s.TotalTasks)
	}
}
