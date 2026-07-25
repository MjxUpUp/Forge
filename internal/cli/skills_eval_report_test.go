package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// TestFormatJudgeVerdict 锁定 eval-report 机器判据行的格式化——skill-evolution SKILL 据此
// 行（「机器判据：accept/reject」）做 accept/reject 决策，格式断裂则 SKILL 解析不到判据信号，
// 退回 agent 自述（dogfood 铁律：纯靠自觉必漏）。
func TestFormatJudgeVerdict(t *testing.T) {
	cases := []struct {
		name     string
		rep      *skillseval.RegressionReport
		prefix   string // 输出应以该前缀开头
		contains string // 输出应含的子串
	}{
		{
			name:     `无 baseline accept+advisory（不能当无退化读）`,
			rep:      &skillseval.RegressionReport{HasBaseline: false},
			prefix:   `机器判据：accept（advisory：`,
			contains: `首跑无 baseline`,
		},
		{
			name: `不可比 accept+advisory`,
			rep: &skillseval.RegressionReport{
				HasBaseline:        true,
				Comparable:         false,
				IncomparableReason: `forge_version v1→v2`,
			},
			prefix:   `机器判据：accept（advisory：`,
			contains: `不可比`,
		},
		{
			name: `regressions reject`,
			rep: &skillseval.RegressionReport{
				HasBaseline: true,
				Comparable:  true,
				Regressions: []skillseval.CaseResult{{CaseID: "t1"}},
			},
			prefix:   `机器判据：reject（`,
			contains: `t1`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatJudgeVerdict(c.rep)
			if c.prefix != "" && !strings.HasPrefix(got, c.prefix) {
				t.Errorf("输出应以 %q 开头, got %q", c.prefix, got)
			}
			if c.contains != "" && !strings.Contains(got, c.contains) {
				t.Errorf("输出应含 %q, got %q", c.contains, got)
			}
		})
	}
}
