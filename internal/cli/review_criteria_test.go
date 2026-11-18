package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestReviewCriteriaReviewed 钉住「评审者独立核对过多少条判据」这一栏真的落库，
// 交付单与 review 审计都靠它判断评审是否看过判据本身。
func TestReviewCriteriaReviewed(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	const ref = "feat/criteria"
	st0 := &taskpipeline.TaskState{
		TaskRef: ref,
		Branch:  ref,
		Acceptance: []taskpipeline.AcceptanceCriterion{
			{Run: "true", Passed: true},
			{Run: "true", Passed: true},
			{Run: "true", Passed: true},
			{Run: "true", Passed: true},
			{Run: "true", Passed: true},
		},
	}
	if err := taskpipeline.SaveTaskState(dir, st0); err != nil {
		t.Fatal(err)
	}
	if err := runReviewPassAtOpts(dir, ref, "", "rev-c", reviewOptions{CriteriaReviewed: intPtr(4)}); err != nil {
		t.Fatalf("review pass: %v", err)
	}
	st, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.ReviewRounds) == 0 {
		t.Fatal("应记录评审轮次")
	}
	last := st.ReviewRounds[len(st.ReviewRounds)-1]
	if last.CriteriaReviewed == nil || *last.CriteriaReviewed != 4 {
		t.Fatalf("认领判据条数应为 4, got %+v", last.CriteriaReviewed)
	}
}

func intPtr(n int) *int { return &n }

// TestReviewCriteriaReviewed_RejectsWithoutCriteria 钉住零判据任务不能认领判据——
// 否则「独立核对过 N 条」在 0/0 的任务上可以随便写（review-rfe-1 P3-g 实证）。
func TestReviewCriteriaReviewed_RejectsWithoutCriteria(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	const ref = "feat/criteria-zero"
	if err := taskpipeline.SaveTaskState(dir, &taskpipeline.TaskState{TaskRef: ref, Branch: ref}); err != nil {
		t.Fatal(err)
	}
	err := runReviewPassAtOpts(dir, ref, "", "rev-z", reviewOptions{CriteriaReviewed: intPtr(5)})
	if err == nil || !strings.Contains(err.Error(), "没有登记验收判据") {
		t.Fatalf("零判据任务认领 5 条必须被拒, got %v", err)
	}
}
