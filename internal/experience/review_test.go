package experience

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

func TestShouldReview(t *testing.T) {
	tests := []struct {
		score         float64
		wantCreate    bool
		wantMandatory bool
	}{
		{69, true, true},
		{70, true, false},
		{75, true, false},
		{80, false, false},
		{95, false, false},
		{50, true, true},
	}
	for _, tt := range tests {
		create, mandatory := ShouldReview(tt.score)
		if create != tt.wantCreate {
			t.Errorf("ShouldReview(%.0f): create = %v, want %v", tt.score, create, tt.wantCreate)
		}
		if mandatory != tt.wantMandatory {
			t.Errorf("ShouldReview(%.0f): mandatory = %v, want %v", tt.score, mandatory, tt.wantMandatory)
		}
	}
}

func makeScoreResult(taskRef string, overall float64, dims []scoringtypes.DimensionScore) *scoringtypes.ScoreResult {
	return &scoringtypes.ScoreResult{
		TaskRef:    taskRef,
		Dimensions: dims,
		Overall:    overall,
		Grade:      "C",
		ScoredAt:   time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}
}

func TestLowDimensions(t *testing.T) {
	result := makeScoreResult("task-1", 65, []scoringtypes.DimensionScore{
		{Dimension: scoringtypes.DimensionTesting, Score: 90, Detail: "good"},
		{Dimension: scoringtypes.DimensionProcess, Score: 45, Detail: "bad process"},
		{Dimension: scoringtypes.DimensionAssertions, Score: 60, Detail: "weak assertions"},
		{Dimension: scoringtypes.DimensionCodeQuality, Score: 80, Detail: "decent"},
	})

	lows := LowDimensions(result)
	if len(lows) != 2 {
		t.Fatalf("LowDimensions returned %d items, want 2", len(lows))
	}

	wantDims := map[scoringtypes.Dimension]int{
		scoringtypes.DimensionProcess:    45,
		scoringtypes.DimensionAssertions: 60,
	}
	for _, ld := range lows {
		wantScore, ok := wantDims[ld.Dimension]
		if !ok {
			t.Errorf("unexpected dimension %q in low dimensions", ld.Dimension)
			continue
		}
		if ld.Score != wantScore {
			t.Errorf("dimension %q: score = %d, want %d", ld.Dimension, ld.Score, wantScore)
		}
		delete(wantDims, ld.Dimension)
	}
	if len(wantDims) > 0 {
		t.Errorf("missing dimensions in low dimensions: %v", wantDims)
	}
}

func TestLowDimensions_None(t *testing.T) {
	result := makeScoreResult("task-2", 85, []scoringtypes.DimensionScore{
		{Dimension: scoringtypes.DimensionTesting, Score: 90, Detail: "good"},
		{Dimension: scoringtypes.DimensionProcess, Score: 80, Detail: "ok"},
	})
	lows := LowDimensions(result)
	if len(lows) != 0 {
		t.Errorf("LowDimensions returned %d items, want 0", len(lows))
	}
}

func TestCreateReview_Idempotent(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	result := makeScoreResult("feature/branch-1", 65, []scoringtypes.DimensionScore{
		{Dimension: scoringtypes.DimensionProcess, Score: 50, Detail: "low"},
	})

	if err := CreateReview(root, result.TaskRef, result); err != nil {
		t.Fatalf("first CreateReview: %v", err)
	}
	if err := CreateReview(root, result.TaskRef, result); err != nil {
		t.Fatalf("second CreateReview: %v", err)
	}

	// Verify exactly 1 file exists in reviews dir.
	entries, err := filepath.Glob(filepath.Join(root.ReviewsDir(), "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 review file, got %d", len(entries))
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	ts := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)

	review := &ReviewRequest{
		TaskRef: "feature/auth",
		Score:   65.5,
		Grade:   "D",
		LowDimensions: []LowDimension{
			{Dimension: scoringtypes.DimensionProcess, Score: 50, Detail: "poor process"},
		},
		Mandatory: true,
		Status:    ReviewPending,
		CreatedAt: ts,
	}

	if err := SaveReview(root, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}

	loaded, err := LoadReview(root, "feature/auth")
	if err != nil {
		t.Fatalf("LoadReview: %v", err)
	}

	if loaded.TaskRef != review.TaskRef {
		t.Errorf("TaskRef = %q, want %q", loaded.TaskRef, review.TaskRef)
	}
	if loaded.Score != review.Score {
		t.Errorf("Score = %f, want %f", loaded.Score, review.Score)
	}
	if loaded.Grade != review.Grade {
		t.Errorf("Grade = %q, want %q", loaded.Grade, review.Grade)
	}
	if loaded.Mandatory != review.Mandatory {
		t.Errorf("Mandatory = %v, want %v", loaded.Mandatory, review.Mandatory)
	}
	if loaded.Status != review.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, review.Status)
	}
	if !loaded.CreatedAt.Equal(review.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", loaded.CreatedAt, review.CreatedAt)
	}
	if len(loaded.LowDimensions) != len(review.LowDimensions) {
		t.Fatalf("LowDimensions count = %d, want %d", len(loaded.LowDimensions), len(review.LowDimensions))
	}
	if loaded.LowDimensions[0].Dimension != review.LowDimensions[0].Dimension {
		t.Errorf("LowDimension.Dimension = %q, want %q", loaded.LowDimensions[0].Dimension, review.LowDimensions[0].Dimension)
	}
	if loaded.LowDimensions[0].Score != review.LowDimensions[0].Score {
		t.Errorf("LowDimension.Score = %d, want %d", loaded.LowDimensions[0].Score, review.LowDimensions[0].Score)
	}
	if loaded.LowDimensions[0].Detail != review.LowDimensions[0].Detail {
		t.Errorf("LowDimension.Detail = %q, want %q", loaded.LowDimensions[0].Detail, review.LowDimensions[0].Detail)
	}
}

func TestListReviews(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())

	result1 := makeScoreResult("task-a", 65, []scoringtypes.DimensionScore{
		{Dimension: scoringtypes.DimensionProcess, Score: 50, Detail: "low"},
	})
	result2 := makeScoreResult("task-b", 72, []scoringtypes.DimensionScore{
		{Dimension: scoringtypes.DimensionTesting, Score: 60, Detail: "missing tests"},
	})

	if err := CreateReview(root, result1.TaskRef, result1); err != nil {
		t.Fatalf("CreateReview task-a: %v", err)
	}
	if err := CreateReview(root, result2.TaskRef, result2); err != nil {
		t.Fatalf("CreateReview task-b: %v", err)
	}

	reviews, err := ListReviews(root)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("ListReviews returned %d reviews, want 2", len(reviews))
	}

	found := map[string]bool{}
	for _, r := range reviews {
		found[r.TaskRef] = true
	}
	if !found["task-a"] {
		t.Error("task-a not found in list")
	}
	if !found["task-b"] {
		t.Error("task-b not found in list")
	}
}

func TestListReviews_EmptyDir(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	reviews, err := ListReviews(root)
	if err != nil {
		t.Fatalf("ListReviews on empty dir: %v", err)
	}
	if len(reviews) != 0 {
		t.Errorf("expected 0 reviews, got %d", len(reviews))
	}
}

func TestUpdateReviewStatus(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	result := makeScoreResult("feature/x", 65, []scoringtypes.DimensionScore{
		{Dimension: scoringtypes.DimensionProcess, Score: 50, Detail: "low"},
	})

	if err := CreateReview(root, result.TaskRef, result); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	// Verify initial status is pending.
	r, err := LoadReview(root, "feature/x")
	if err != nil {
		t.Fatalf("LoadReview before update: %v", err)
	}
	if r.Status != ReviewPending {
		t.Fatalf("initial status = %q, want %q", r.Status, ReviewPending)
	}

	// Update to analyzed.
	if err := UpdateReviewStatus(root, "feature/x", ReviewAnalyzed); err != nil {
		t.Fatalf("UpdateReviewStatus: %v", err)
	}

	r, err = LoadReview(root, "feature/x")
	if err != nil {
		t.Fatalf("LoadReview after update: %v", err)
	}
	if r.Status != ReviewAnalyzed {
		t.Errorf("status after update = %q, want %q", r.Status, ReviewAnalyzed)
	}
}

func TestLoadReview_Nonexistent(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	_, err := LoadReview(root, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent review, got nil")
	}
	want := "not found"
	if !containsString(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// TestPendingMandatory guards the enforcement point forge task complete uses to
// block completion of a low-scoring task. Previously this force lived in the
// task-verify Stop hook (blocking session end); with that hook now advisory,
// PendingMandatory is the new gate. It must fire ONLY for a mandatory review
// still in the pending state — resolved or optional reviews never block.
func TestPendingMandatory(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	ts := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)

	// No review at all → not blocked.
	if _, ok := PendingMandatory(root, "missing-task"); ok {
		t.Error("PendingMandatory should be false when no review exists")
	}

	// Mandatory + pending → blocked, returns the review (for score/grade display).
	mand := &ReviewRequest{
		TaskRef: "low/score", Score: 60, Grade: "D",
		Mandatory: true, Status: ReviewPending, CreatedAt: ts,
	}
	if err := SaveReview(root, mand); err != nil {
		t.Fatalf("SaveReview mandatory: %v", err)
	}
	r, ok := PendingMandatory(root, "low/score")
	if !ok {
		t.Fatal("PendingMandatory should be true for a pending mandatory review")
	}
	if r.Score != 60 || r.Grade != "D" {
		t.Errorf("returned review = %.0f/%s, want 60/D", r.Score, r.Grade)
	}

	// Once resolved → no longer blocked (the complete-unblocking path).
	if err := ResolveReview(root, "low/score"); err != nil {
		t.Fatalf("ResolveReview: %v", err)
	}
	if _, ok := PendingMandatory(root, "low/score"); ok {
		t.Error("PendingMandatory should be false after the review is resolved")
	}

	// Optional review (70-79) → never blocks, even while pending.
	opt := &ReviewRequest{
		TaskRef: "mid/score", Score: 75, Grade: "C",
		Mandatory: false, Status: ReviewPending, CreatedAt: ts,
	}
	if err := SaveReview(root, opt); err != nil {
		t.Fatalf("SaveReview optional: %v", err)
	}
	if _, ok := PendingMandatory(root, "mid/score"); ok {
		t.Error("PendingMandatory should be false for an optional (non-mandatory) review")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
