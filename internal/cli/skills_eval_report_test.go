package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// TestFormatJudgeVerdict pins the formatting of the eval-report machine-verdict line — the skill-evolution SKILL
// reads this line (the verdict line containing accept/reject) to make accept/reject decisions; if the format breaks
// the SKILL cannot parse the verdict signal and falls back to agent self-report (dogfood rule: pure self-discipline always misses).
//
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

// TestResolveReportBaseline pins baseline selection honesty: lookup errors
// propagate (no silent nil baseline lying "未锚定"), a cleaned-out marked
// baseline degrades to absolute scoring (nil, nil) instead of erroring, and an
// explicit --baseline that does not exist is an error.
//
// TestResolveReportBaseline 钉住 baseline 选择的诚实性：读取 error 传播（不再
// 静默 nil baseline 谎称「未锚定」）；标记的 baseline run 已被清理时降级为绝对分
// （nil, nil）而非报错；显式 --baseline 不存在是 error。
func TestResolveReportBaseline(t *testing.T) {
	dir := t.TempDir()
	skill := "demo"

	mustRun := func(id string) {
		t.Helper()
		if err := skillseval.AppendRun(dir, skill, &skillseval.EvalRun{RunID: id, Skill: skill}); err != nil {
			t.Fatalf("AppendRun %s: %v", id, err)
		}
	}

	// No mark at all → absolute scoring.
	if got, err := resolveReportBaseline(dir, skill, ""); err != nil || got != nil {
		t.Fatalf("no mark: got (%v, %v), want (nil, nil)", got, err)
	}

	// Explicit run-id that does not exist → error.
	if _, err := resolveReportBaseline(dir, skill, "nope"); err == nil {
		t.Fatal("explicit missing run: want error, got nil")
	}

	// Marked baseline whose run file was cleaned → degrade (nil, nil).
	mustRun("r1")
	if err := skillseval.SetBaseline(dir, skill, "ghost-run", "test"); err != nil {
		t.Fatalf("SetBaseline: %v", err)
	}
	if got, err := resolveReportBaseline(dir, skill, ""); err != nil || got != nil {
		t.Fatalf("stale mark: got (%v, %v), want (nil, nil) degraded", got, err)
	}

	// Marked baseline with an existing run → returns that run.
	if err := skillseval.SetBaseline(dir, skill, "r1", "test"); err != nil {
		t.Fatalf("SetBaseline r1: %v", err)
	}
	got, err := resolveReportBaseline(dir, skill, "")
	if err != nil || got == nil || got.RunID != "r1" {
		t.Fatalf("marked baseline: got (%v, %v), want run r1", got, err)
	}

	// Corrupt baselines.json → GetBaseline error must propagate.
	if err := os.WriteFile(filepath.Join(dir, "baselines.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveReportBaseline(dir, skill, ""); err == nil {
		t.Fatal("corrupt baselines.json: want error, got nil (silent nil baseline lies 未锚定)")
	}
}
