package health

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
)

func at(hour int) time.Time { return time.Date(2026, 7, 1, hour, 0, 0, 0, time.UTC) }

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
	// 均分 (95+90+92+60)/4 = 84.25
	if s.AvgScore != 84.25 {
		t.Errorf(`AvgScore=%v want 84.25`, s.AvgScore)
	}
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
	// t1: api+backend grade A（都通过）；t2: api grade C（不过）；t3: backend grade B（通过）；
	// t4: grade="" 不进 phaseGrades（无 grade 守门）。
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
	// api: t1(A,通过) + t2(C,不过) + t4(无grade,不进) → 1 通过 / 2 总数 = 0.5
	if got := s.PhasePassRate[`api`]; got != 0.5 {
		t.Errorf(`api pass_rate=%v want 0.5（A通过/C不过/无grade不进 → 1/2）`, got)
	}
	// backend: t1(A) + t3(B) → 2/2 = 1.0（A+B 都通过）
	if got := s.PhasePassRate[`backend`]; got != 1.0 {
		t.Errorf(`backend pass_rate=%v want 1.0（A+B 都通过）`, got)
	}
}

func TestSummarize_PhasePassRate_EmptyIsNil(t *testing.T) {
	// 空切片 → PhasePassRate nil（JSON omitempty 生效，不出空 map）。
	if s := Summarize(nil); s.PhasePassRate != nil {
		t.Errorf(`空切片 PhasePassRate=%v want nil`, s.PhasePassRate)
	}
	// 全无 grade → phaseGrades 永不填充 → PhasePassRate nil。
	s2 := Summarize([]act.Conclusion{
		{TaskRef: `x`, Grade: ``, Strength: `Strong`, Score: 0, DesignPhases: []string{`api`}, CompletedAt: at(1)},
	})
	if s2.PhasePassRate != nil {
		t.Errorf(`全无 grade PhasePassRate=%v want nil（无 grade 守门）`, s2.PhasePassRate)
	}
}
