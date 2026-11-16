package clitask

import (
	"strings"
	"testing"
)

// TestRenderTaskReport_DocReviewDisclosure 钉住 1.87.2 dogfood 的修复：交付单必须
// 在「审查」行下披露 L2 文档回检（分数/轮次/评审者），否则验收方无法据此决定抽查深度。
func TestRenderTaskReport_DocReviewDisclosure(t *testing.T) {
	rep := &taskReportJSON{
		TaskRef:       "feat/demo",
		DocReviewLine: "L2 score 88（round 2，reviewer-x） · 自查（独立性未校验）",
	}
	out := renderTaskReport(rep)
	if !strings.Contains(out, "文档回检：L2 score 88") {
		t.Fatalf("交付单应包含文档回检行，got:\n%s", out)
	}
	if !strings.Contains(out, "自查（独立性未校验）") {
		t.Fatalf("自查/未记录评审者时应显式披露，got:\n%s", out)
	}
}
