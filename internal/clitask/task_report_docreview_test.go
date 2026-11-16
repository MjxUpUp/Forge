package clitask

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestBuildTaskReport_DocReviewLine 钉住披露行的三种拼接形态——渲染器只测前缀
// 会漏掉 reviewer 为空 / SelfReview 的真实分支（1.87.2 文档评审 M3）。
func TestBuildTaskReport_DocReviewLine(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	cases := []struct {
		name string
		d    *tasktypes.DocReview
		want []string
		deny []string
	}{
		{
			name: "normal reviewer",
			d:    &tasktypes.DocReview{Passed: true, RubricScore: 88, Round: 2, Reviewer: "reviewer-x", ReviewedAt: time.Now()},
			want: []string{"L2 回检 pass（score 88", "round 2", "reviewer-x"},
			deny: []string{"自查"},
		},
		{
			name: "no reviewer",
			d:    &tasktypes.DocReview{Passed: true, RubricScore: 80, Round: 1, ReviewedAt: time.Now()},
			want: []string{"未记录评审者", "自查（独立性未校验）"},
		},
		{
			name: "failed review",
			d:    &tasktypes.DocReview{Passed: false, RubricScore: 60, Round: 1, Reviewer: "reviewer-y", ReviewedAt: time.Now()},
			want: []string{"L2 回检 fail（score 60", "reviewer-y"},
		},
		{
			name: "self review",
			d:    &tasktypes.DocReview{Passed: true, RubricScore: 80, Round: 1, Reviewer: "me", SelfReview: true, ReviewedAt: time.Now()},
			want: []string{"自查（独立性未校验）"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := &taskpipeline.TaskState{TaskRef: "feat/demo", DocReview: c.d}
			rep, err := buildTaskReport(root, state)
			if err != nil {
				t.Fatalf("buildTaskReport: %v", err)
			}
			for _, w := range c.want {
				if !strings.Contains(rep.DocReviewLine, w) {
					t.Fatalf("DocReviewLine 缺 %q，got %q", w, rep.DocReviewLine)
				}
			}
			for _, d := range c.deny {
				if strings.Contains(rep.DocReviewLine, d) {
					t.Fatalf("DocReviewLine 不应含 %q，got %q", d, rep.DocReviewLine)
				}
			}
		})
	}
}

// TestRenderTaskReport_DocReviewDisclosure 保证人读渲染确实把披露行印在「审查」之后。
func TestRenderTaskReport_DocReviewDisclosure(t *testing.T) {
	rep := &taskReportJSON{TaskRef: "feat/demo", DocReviewLine: "L2 回检 pass（score 88，round 2，reviewer-x）"}
	out := renderTaskReport(rep)
	if !strings.Contains(out, "文档回检：L2 回检 pass（score 88") {
		t.Fatalf("交付单应包含文档回检行，got:\n%s", out)
	}
}
