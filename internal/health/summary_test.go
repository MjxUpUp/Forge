package health

import (
	"fmt"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
)

func at(hour int) time.Time { return time.Date(2026, 7, 1, hour, 0, 0, 0, time.UTC) }

// mk 是构造测试结论的简写：ref/strength/deterministic/完成时刻。2.3 起结论只带
// 事实（判据/红先绿/评审/逃生）+ 证据强度，没有分数/档/维度。
func mk(ref, strength string, det int, t time.Time) act.Conclusion {
	return act.Conclusion{TaskRef: ref, Strength: strength, Deterministic: det, CompletedAt: t}
}

// facts 给结论挂上事实清单（判据/红先绿/评审/逃生），验证上卷求和。
func facts(ref, strength string, criteria, passed, redTrials, redClosed, rounds, escapes int, t time.Time) act.Conclusion {
	c := mk(ref, strength, passed, t)
	c.AcceptanceTotal = criteria
	c.AcceptancePass = passed
	c.RedFirstTrials = redTrials
	c.RedFirstClosed = redClosed
	c.ReviewRounds = rounds
	c.Escapes = escapes
	return c
}

func TestSummarize_Empty(t *testing.T) {
	s := SummarizeAt(nil, time.Now())
	if s.TotalTasks != 0 {
		t.Errorf(`空切片 TotalTasks=%d want 0`, s.TotalTasks)
	}
	if s.BlindSpotRate != 0 || s.BlindSpotCount != 0 || s.CappedWeakCount != 0 {
		t.Errorf(`空切片应全零值，got rate=%v blind=%d capped=%d`, s.BlindSpotRate, s.BlindSpotCount, s.CappedWeakCount)
	}
	if s.Criteria != 0 || s.CriteriaPassed != 0 || s.RedFirstTrials != 0 || s.RedFirstClosed != 0 ||
		s.ReviewRounds != 0 || s.Escapes != 0 {
		t.Errorf(`空切片事实计数应全零，got %+v`, s)
	}
	if len(s.StrengthDist) != 0 {
		t.Errorf(`空切片 StrengthDist=%v want 空`, s.StrengthDist)
	}
}

func TestSummarize_BlindSpotRateAndFacts(t *testing.T) {
	// 4 个任务：2 Strong、1 Unverified、1 Weak（零 deterministic）→ 盲区率 50%（2/4）。
	cs := []act.Conclusion{
		facts(`a`, `Strong`, 2, 2, 1, 1, 1, 0, at(1)),
		mk(`b`, `Unverified`, 0, at(2)),
		facts(`c`, `Strong`, 3, 3, 1, 1, 2, 0, at(3)),
		mk(`d`, `Weak`, 0, at(4)),
	}
	s := SummarizeAt(cs, time.Now())
	if s.TotalTasks != 4 {
		t.Fatalf(`TotalTasks=%d want 4`, s.TotalTasks)
	}
	if s.BlindSpotCount != 2 || s.BlindSpotRate != 0.5 {
		t.Errorf(`BlindSpot=%d/%v want 2/0.5（Unverified+无证据 Weak）`, s.BlindSpotCount, s.BlindSpotRate)
	}
	if s.CappedWeakCount != 0 {
		t.Errorf(`CappedWeakCount=%d want 0（零 deterministic 的 Weak 是盲区不是封顶）`, s.CappedWeakCount)
	}
	if s.StrengthDist[`Strong`] != 2 || s.StrengthDist[`Unverified`] != 1 || s.StrengthDist[`Weak`] != 1 {
		t.Errorf(`StrengthDist=%+v want Strong=2 Unverified=1 Weak=1`, s.StrengthDist)
	}
	// 事实单向求和：判据 5/5、红先绿 2/2、评审 3 轮、逃生 0。
	if s.Criteria != 5 || s.CriteriaPassed != 5 {
		t.Errorf(`Criteria=%d/%d want 5/5`, s.CriteriaPassed, s.Criteria)
	}
	if s.RedFirstTrials != 2 || s.RedFirstClosed != 2 {
		t.Errorf(`RedFirst=%d/%d want 2/2`, s.RedFirstClosed, s.RedFirstTrials)
	}
	if s.ReviewRounds != 3 || s.Escapes != 0 {
		t.Errorf(`ReviewRounds=%d Escapes=%d want 3/0`, s.ReviewRounds, s.Escapes)
	}
}

func TestSummarize_CappedWeakNotBlind(t *testing.T) {
	// 带 deterministic 证据的 Weak = 逃生舱封顶（已付代价），单独计数，不算盲区。
	s := SummarizeAt([]act.Conclusion{mk(`capped`, `Weak`, 4, at(1))}, time.Now())
	if s.CappedWeakCount != 1 || s.BlindSpotCount != 0 || s.BlindSpotRate != 0 {
		t.Errorf(`封顶 Weak 应 capped=1 blind=0，got capped=%d blind=%d rate=%v`, s.CappedWeakCount, s.BlindSpotCount, s.BlindSpotRate)
	}
}

func TestSummarize_NudgeCount(t *testing.T) {
	cs := []act.Conclusion{
		mk(`a`, `Strong`, 3, at(1)),
		{TaskRef: `b`, Strength: `Unverified`, RetrospectiveNudge: true, CompletedAt: at(2)},
		{TaskRef: `c`, Strength: `Strong`, RetrospectiveNudge: true, CompletedAt: at(3)},
	}
	s := SummarizeAt(cs, time.Now())
	if s.NudgeCount != 2 {
		t.Errorf(`NudgeCount=%d want 2（b/c 被 nudge）`, s.NudgeCount)
	}
}

// TestSummarizeAt_NudgeRecentWindow pins the dashboard-facing windowed nudge
// count: NudgeRecent counts only nudged conclusions completed within the 14-day
// window ending at `now`.
//
// TestSummarizeAt_NudgeRecentWindow 钉住面向 dashboard 的窗口化 nudge 计数：
// NudgeRecent 只数 `now` 前 14 天窗口内完成的 nudge 结论；NudgeCount 保留全量真相
// （不丢信息）。这就是对"告警只增不减"疲劳的止血——数周前的陈旧 nudge 不再点亮面板，
// 历史仍可查。
func TestSummarizeAt_NudgeRecentWindow(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	cs := []act.Conclusion{
		// 窗口内 3 天前：计 recent
		{TaskRef: `recent-nudge`, Strength: `Weak`, RetrospectiveNudge: true, CompletedAt: now.Add(-3 * 24 * time.Hour)},
		// 窗口内 13 天前：计 recent（边界内侧）
		{TaskRef: `edge-nudge`, Strength: `Unverified`, RetrospectiveNudge: true, CompletedAt: now.Add(-13 * 24 * time.Hour)},
		// 窗口外 15 天前：只计全量，不计 recent
		{TaskRef: `stale-nudge`, Strength: `Weak`, RetrospectiveNudge: true, CompletedAt: now.Add(-15 * 24 * time.Hour)},
		// 窗口外 60 天前且未 nudge：两边都不计
		{TaskRef: `stale-clean`, Strength: `Strong`, RetrospectiveNudge: false, CompletedAt: now.Add(-60 * 24 * time.Hour)},
		// 窗口内未 nudge：两边都不计
		{TaskRef: `recent-clean`, Strength: `Strong`, RetrospectiveNudge: false, CompletedAt: now.Add(-1 * 24 * time.Hour)},
	}
	s := SummarizeAt(cs, now)
	if s.NudgeCount != 3 {
		t.Errorf(`NudgeCount=%d want 3（全量：recent-nudge/edge-nudge/stale-nudge）`, s.NudgeCount)
	}
	if s.NudgeRecent != 2 {
		t.Errorf(`NudgeRecent=%d want 2（仅窗口内：recent-nudge/edge-nudge；stale 的不计）`, s.NudgeRecent)
	}
	// 窗口沿契约（review 2026-08）：窗口是 (now-Window, now]——恰好完成于 now-14d 的
	// nudge 落在窗外（After 为严格比较）。显式钉住半开语义，使其成为被测契约而非偶然。
	exact := SummarizeAt([]act.Conclusion{
		{TaskRef: `exact-edge`, Strength: `Weak`, RetrospectiveNudge: true, CompletedAt: now.Add(-14 * 24 * time.Hour)},
	}, now)
	if exact.NudgeCount != 1 || exact.NudgeRecent != 0 {
		t.Errorf(`窗口沿：恰 14 天前的 nudge 应计全量不计窗口（半开区间 (now-14d, now]），got count=%d recent=%d`, exact.NudgeCount, exact.NudgeRecent)
	}
	// 窗口内侧边界：左沿内侧 1 秒必须计入——防反方向的差一错误（整天截断等）。
	inside := SummarizeAt([]act.Conclusion{
		{TaskRef: `inside-edge`, Strength: `Weak`, RetrospectiveNudge: true, CompletedAt: now.Add(-14*24*time.Hour + time.Second)},
	}, now)
	if inside.NudgeRecent != 1 {
		t.Errorf(`窗口内侧 1 秒的 nudge 必须计入 recent，got %d`, inside.NudgeRecent)
	}
}

// TestSummarizeAt_NudgeActionable pins the alert-facing tiered nudge count:
// NudgeActionable counts only windowed nudges that are NOT escape-hatch-capped
// Weak. Capped Weak (Strength=Weak && Ratio>=0.5 — would be Strong without the
// verification-class escape hatch) already paid its tax at completion time; the
// human-facing alert channel re-displaying it adds no information (2026-09
// finding: 8/11 dashboard alerts were capped-Weak noise, evidenceBlindRate 0).
//
// TestSummarizeAt_NudgeActionable 钉住面向告警的分级 nudge 计数：NudgeActionable
// 只数窗口内且【非】逃生舱降档 Weak 的 nudge。分类是持久化字段的纯函数，不碰磁盘、零迁移。
func TestSummarizeAt_NudgeActionable(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	mkA := func(ref string, strength string, ratio float64) act.Conclusion {
		return act.Conclusion{
			TaskRef: ref, Strength: strength, Ratio: ratio,
			RetrospectiveNudge: true, CompletedAt: now.Add(-24 * time.Hour),
		}
	}
	cases := []struct {
		name       string
		c          act.Conclusion
		actionable bool
	}{
		{`降档 Weak（ratio 0.75）不计`, mkA(`capped`, `Weak`, 0.75), false},
		{`降档沿（ratio 恰 0.5）不计`, mkA(`capped-edge`, `Weak`, 0.50), false},
		{`真弱（ratio 0.49）计`, mkA(`true-weak`, `Weak`, 0.49), true},
		{`真弱（ratio 0.08 自述占绝大多数）计`, mkA(`claim-heavy`, `Weak`, 0.08), true},
		{`Unverified 计`, mkA(`unverified`, `Unverified`, 0), true},
		{`Strong 非 nudge 不计`, act.Conclusion{
			TaskRef: `clean`, Strength: `Strong`, Ratio: 0.9,
			CompletedAt: now.Add(-24 * time.Hour),
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := SummarizeAt([]act.Conclusion{tc.c}, now)
			if tc.c.RetrospectiveNudge {
				if s.NudgeRecent != 1 {
					t.Errorf(`NudgeRecent=%d want 1（nudge 样本仍计窗口总量——透明拆解不丢信息）`, s.NudgeRecent)
				}
			} else if s.NudgeRecent != 0 {
				t.Errorf(`NudgeRecent=%d want 0（非 nudge 样本）`, s.NudgeRecent)
			}
			if tc.actionable && s.NudgeActionable != 1 {
				t.Errorf(`NudgeActionable=%d want 1`, s.NudgeActionable)
			}
			if !tc.actionable && s.NudgeActionable != 0 {
				t.Errorf(`NudgeActionable=%d want 0`, s.NudgeActionable)
			}
		})
	}
	// 窗口外降档/真弱均不计 actionable；stale 真弱也不点亮面板。
	staleC := mkA(`stale`, `Weak`, 0.44)
	staleC.CompletedAt = now.Add(-15 * 24 * time.Hour)
	stale := SummarizeAt([]act.Conclusion{staleC}, now)
	if stale.NudgeActionable != 0 || stale.NudgeRecent != 0 {
		t.Errorf(`窗口外应全不计，got actionable=%d recent=%d`, stale.NudgeActionable, stale.NudgeRecent)
	}
	// 混合快照：3 真弱 + 8 降档（2026-09 实测形态）→ recent=11、actionable=3。
	mix := []act.Conclusion{
		mkA(`w1`, `Weak`, 0.44), mkA(`w2`, `Weak`, 0.08), mkA(`w3`, `Weak`, 0.33),
	}
	for i := 0; i < 8; i++ {
		mix = append(mix, mkA(fmt.Sprintf("c%d", i), `Weak`, 0.75))
	}
	s := SummarizeAt(mix, now)
	if s.NudgeRecent != 11 || s.NudgeActionable != 3 {
		t.Errorf(`实测形态：recent=%d want 11、actionable=%d want 3`, s.NudgeRecent, s.NudgeActionable)
	}
}

// TestSummarizeAt_FullWindowRecentEqualsCount pins that when every nudged
// conclusion falls inside the window, NudgeRecent equals the all-history
// NudgeCount, and that `now` never changes the non-windowed aggregates.
//
// TestSummarizeAt_FullWindowRecentEqualsCount 钉住：全部 nudge 结论落进窗口时，
// NudgeRecent 等于全量 NudgeCount；窗口在结论之后太远时 NudgeRecent 归零而
// NudgeCount 不变；now 不同不影响非窗口聚合（总数/盲区/判据事实）。
func TestSummarizeAt_FullWindowRecentEqualsCount(t *testing.T) {
	cs := []act.Conclusion{
		facts(`a`, `Strong`, 2, 2, 1, 1, 1, 0, at(1)),
		{TaskRef: `b`, Strength: `Unverified`, RetrospectiveNudge: true, CompletedAt: at(2)},
		facts(`c`, `Weak`, 3, 2, 1, 0, 1, 1, at(3)),
	}
	// 窗口在结论之后 7 天（全部结论落进窗口）→ NudgeRecent == NudgeCount
	windowedAll := SummarizeAt(cs, at(1).Add(7*24*time.Hour))
	if windowedAll.NudgeRecent != windowedAll.NudgeCount || windowedAll.NudgeCount != 1 {
		t.Errorf(`全窗口时 NudgeRecent(%d) 应 == NudgeCount(%d) == 1`, windowedAll.NudgeRecent, windowedAll.NudgeCount)
	}
	stale := SummarizeAt(cs, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))
	if stale.NudgeRecent != 0 || stale.NudgeCount != 1 {
		t.Errorf(`窗口外：NudgeRecent=%d want 0、NudgeCount=%d want 1`, stale.NudgeRecent, stale.NudgeCount)
	}
	// 窗口只影响 nudge 窗口计数，绝不静默改动非窗口聚合（now 不同，其余字段必须一致）。
	if windowedAll.TotalTasks != stale.TotalTasks || windowedAll.BlindSpotCount != stale.BlindSpotCount ||
		windowedAll.Criteria != stale.Criteria || windowedAll.CriteriaPassed != stale.CriteriaPassed ||
		windowedAll.RedFirstClosed != stale.RedFirstClosed || windowedAll.ReviewRounds != stale.ReviewRounds ||
		windowedAll.Escapes != stale.Escapes {
		t.Errorf(`非窗口字段随 now 变化: windowedAll=%+v stale=%+v`, windowedAll, stale)
	}
}

func TestSummarize_SpanFromEarliestToLatest(t *testing.T) {
	cs := []act.Conclusion{
		mk(`c`, `Strong`, 3, at(9)), // 乱序传入
		mk(`a`, `Strong`, 3, at(1)),
		mk(`b`, `Strong`, 3, at(5)),
	}
	s := SummarizeAt(cs, time.Now())
	if !s.Span.Earliest.Equal(at(1)) || !s.Span.Latest.Equal(at(9)) {
		t.Errorf(`Span=%s~%s want %s~%s（应按时间取范围，非传入顺序）`,
			s.Span.Earliest, s.Span.Latest, at(1), at(9))
	}
}

// TestSummarize_EmptyStrengthGuard: an empty Strength must not be counted into a
// nameless bucket of StrengthDist, nor into BlindSpotCount.
//
// TestSummarize_EmptyStrengthGuard：空 Strength 不得落入 StrengthDist 的无名桶，也不得
// 计入 BlindSpotCount。写入侧（act.BuildConclusion）不会产出空 Strength，此守卫只防
// 手工构造/历史脏数据。
func TestSummarize_EmptyStrengthGuard(t *testing.T) {
	cs := []act.Conclusion{
		mk(`a`, `Strong`, 3, at(1)),
		mk(`b`, ``, 0, at(2)), // 空 Strength：不进桶、不计盲区
	}
	s := SummarizeAt(cs, time.Now())
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

