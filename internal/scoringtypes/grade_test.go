package scoringtypes

import "testing"

// 2026-08-29 审查轮回归钉：部分 thresholds 配置（缺键）不得让任意分数直接拿
// 缺键档（原零值语义 = 全 A，评分门禁整体失效）。
func TestGradeFromScore_MissingThresholdKeys(t *testing.T) {
	partial := map[string]float64{"A": 90} // B/C/D/F 缺失
	cases := []struct {
		score float64
		want  string
	}{
		{95, "A"},
		{85, "B"}, // 缺键回退默认 80 → B，而非误判 A
		{72, "C"},
		{50, "F"},
	}
	for _, c := range cases {
		if got := GradeFromScore(c.score, partial); got != c.want {
			t.Errorf("GradeFromScore(%v, partial) = %q, want %q", c.score, got, c.want)
		}
	}
	// 全缺省（nil map）也不得全 A。
	if got := GradeFromScore(10, nil); got != "F" {
		t.Errorf("GradeFromScore(10, nil) = %q, want F", got)
	}
}
