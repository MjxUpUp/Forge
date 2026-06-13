package experience

import (
	"testing"
	"time"

	"github.com/Harness/forge/internal/scoringtypes"
)

func TestGenerateProposalsForReview_CreatesOnePerDimension(t *testing.T) {
	tmpRoot := t.TempDir()
	lows := []LowDimension{
		{Dimension: scoringtypes.DimensionToolSelection, Score: 35, Detail: "7 anti-patterns"},
		{Dimension: scoringtypes.DimensionTesting, Score: 20, Detail: "no test changes"},
	}
	n, err := GenerateProposalsForReview(tmpRoot, "task-multi", lows)
	if err != nil {
		t.Fatalf("GenerateProposalsForReview: %v", err)
	}
	if n != 2 {
		t.Fatalf("created %d proposals, want 2", n)
	}

	props, err := ListProposals(tmpRoot, "")
	if err != nil {
		t.Fatalf("ListProposals: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("ListProposals returned %d, want 2", len(props))
	}
	for _, p := range props {
		if p.SourceReview != "task-multi" {
			t.Errorf("SourceReview = %q, want task-multi", p.SourceReview)
		}
		if p.Status != PropProposed {
			t.Errorf("Status = %q, want %q", p.Status, PropProposed)
		}
		if p.Title == "" || p.Description == "" {
			t.Errorf("proposal has empty Title/Description: %+v", p)
		}
	}
}

func TestGenerateProposalsForReview_Idempotent(t *testing.T) {
	tmpRoot := t.TempDir()
	lows := []LowDimension{
		{Dimension: scoringtypes.DimensionToolSelection, Score: 35, Detail: ""},
	}

	n1, err := GenerateProposalsForReview(tmpRoot, "task-idem", lows)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("first run created %d, want 1", n1)
	}

	// Second run for the same review must NOT duplicate.
	n2, err := GenerateProposalsForReview(tmpRoot, "task-idem", lows)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second run created %d, want 0 (idempotent)", n2)
	}

	props, _ := ListProposals(tmpRoot, "")
	if len(props) != 1 {
		t.Errorf("after two runs, %d proposals stored, want 1", len(props))
	}
}

func TestGenerateProposalsForReview_SkipsUnknownDimension(t *testing.T) {
	tmpRoot := t.TempDir()
	// A dimension value with no template entry.
	lows := []LowDimension{
		{Dimension: scoringtypes.Dimension("nonexistent-dim"), Score: 10, Detail: ""},
	}
	n, err := GenerateProposalsForReview(tmpRoot, "task-unknown", lows)
	if err != nil {
		t.Fatalf("generate with unknown dimension: %v", err)
	}
	if n != 0 {
		t.Errorf("created %d for unknown dimension, want 0", n)
	}
}

// TestGenerateProposalsForReview_ResolvesReviewViaAccept is the end-to-end
// proof the deadlock is broken: generate proposals for a pending review, accept
// one, and the review must move to resolved. Before the fix there was no
// proposal to accept, so the review could never leave pending.
func TestGenerateProposalsForReview_ResolvesReviewViaAccept(t *testing.T) {
	setHomeTemp(t) // isolate global knowledge store
	tmpRoot := t.TempDir()

	taskRef := "task-deadlock"
	review := &ReviewRequest{
		TaskRef:   taskRef,
		Score:     65,
		Grade:     "D",
		Mandatory: true,
		Status:    ReviewPending,
		LowDimensions: []LowDimension{
			{Dimension: scoringtypes.DimensionToolSelection, Score: 35, Detail: ""},
		},
		CreatedAt: time.Now(),
	}
	if err := SaveReview(tmpRoot, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}

	// Backfill proposals for the existing review (what `forge experience generate` does).
	n, err := GenerateForExistingReview(tmpRoot, taskRef)
	if err != nil {
		t.Fatalf("GenerateForExistingReview: %v", err)
	}
	if n != 1 {
		t.Fatalf("generated %d, want 1", n)
	}

	props, _ := ListProposals(tmpRoot, PropProposed)
	if len(props) != 1 {
		t.Fatalf("expected 1 proposed proposal, got %d", len(props))
	}

	// Accept it — this must resolve the review (the previously-deadlocked step).
	if err := AcceptProposal(tmpRoot, props[0].ID); err != nil {
		t.Fatalf("AcceptProposal: %v", err)
	}

	loaded, err := LoadReview(tmpRoot, taskRef)
	if err != nil {
		t.Fatalf("LoadReview: %v", err)
	}
	if loaded.Status != ReviewResolved {
		t.Errorf("review status = %q, want %q (deadlock not broken)", loaded.Status, ReviewResolved)
	}
}

func TestGenerateForExistingReview_MissingReview(t *testing.T) {
	tmpRoot := t.TempDir()
	_, err := GenerateForExistingReview(tmpRoot, "no-such-task")
	if err == nil {
		t.Fatal("expected error for missing review, got nil")
	}
}
