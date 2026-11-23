package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestReviewPassRequiresCriteriaReviewed 钉住 B3：review pass 不写 --criteria-reviewed
// 时必须报错——「审过但没看判据」是当前最常见的评审空洞，默认值不能替评审者认领。
func TestReviewPassRequiresCriteriaReviewed(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	const ref = "feat/criteria-required"
	st := &taskpipeline.TaskState{
		TaskRef: ref,
		Branch:  ref,
		Acceptance: []taskpipeline.AcceptanceCriterion{
			{Run: "true", Passed: true},
			{Run: "true", Passed: true},
		},
	}
	if err := taskpipeline.SaveTaskState(dir, st); err != nil {
		t.Fatal(err)
	}

	err := runReviewPassAtOpts(dir, ref, "", "rev-r", reviewOptions{})
	if err == nil || !strings.Contains(err.Error(), "--criteria-reviewed") {
		t.Fatalf("未声明核对判据数必须被拒, got %v", err)
	}

	// 显式 --criteria-reviewed 0 = 一条都没核对：允许，但要留痕。
	if err := runReviewPassAtOpts(dir, ref, "", "rev-r", reviewOptions{CriteriaReviewed: intPtr(0)}); err != nil {
		t.Fatalf("显式 0 应允许: %v", err)
	}
	st2, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.ReviewRounds) == 0 || st2.ReviewRounds[len(st2.ReviewRounds)-1].CriteriaReviewed == nil || *st2.ReviewRounds[len(st2.ReviewRounds)-1].CriteriaReviewed != 0 {
		t.Fatalf("0 条认领也要落库, got %+v", st2.ReviewRounds)
	}
}
