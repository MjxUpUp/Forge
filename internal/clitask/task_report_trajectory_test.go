package clitask

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestBuildTrajectoryLine 钉住轨迹观测的两件事：耗时按 check 落章后能被 report 读出来，
// 且没有耗时条目时明确说「未记录」而不是静默消失（旧条目没有 duration_ms 是常态）。
func TestBuildTrajectoryLine(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 2; i++ {
		if err := checklog.Record(dir, &checklog.Entry{
			Check:      taskpipeline.CheckNameAcceptance,
			Passed:     true,
			Checked:    true,
			TaskRef:    "traj",
			Detail:     "判据 3 条",
			DurationMS: 4200,
			Steps:      3,
			RecordedAt: now,
		}); err != nil {
			t.Fatalf("record acceptance: %v", err)
		}
	}
	if err := checklog.Record(dir, &checklog.Entry{
		Check:      checklog.CheckReviewPass,
		Passed:     true,
		Checked:    true,
		TaskRef:    "traj",
		Detail:     "review round 1 passed",
		DurationMS: 900,
		Attempt:    1,
		RecordedAt: now,
	}); err != nil {
		t.Fatalf("record review: %v", err)
	}

	line := buildTrajectoryLine(dir, "traj")
	if !strings.Contains(line, "验收 2 轮") {
		t.Fatalf("轨迹应统计验收轮数，got %q", line)
	}
	if !strings.Contains(line, "4.2s") {
		t.Fatalf("轨迹应显示末轮耗时，got %q", line)
	}
	if !strings.Contains(line, "评审 1 轮") {
		t.Fatalf("轨迹应统计评审轮数，got %q", line)
	}

	if got := buildTrajectoryLine(dir, "other"); !strings.Contains(got, "未记录") {
		t.Fatalf("无耗时条目应明说未记录，got %q", got)
	}
}
