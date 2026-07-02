package experience

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

func TestGenerateProposalsForReview_CreatesOnePerDimension(t *testing.T) {
	tmpRoot := t.TempDir()
	lows := []LowDimension{
		{Dimension: scoringtypes.DimensionScope, Score: 35, Detail: "7 anti-patterns"},
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
		{Dimension: scoringtypes.DimensionScope, Score: 35, Detail: ""},
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
			{Dimension: scoringtypes.DimensionScope, Score: 35, Detail: ""},
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

// TestDimensionTemplatesCoverAllDimensions guards against the silent-deadlock
// regression: if a new scoring dimension is added (to scoringtypes.DefaultWeights)
// but no proposal template is added for it, a low score in that dimension would
// get silently skipped and — if it's the only low dim — leave a mandatory review
// with zero proposals to accept, re-deadlocking it.
func TestDimensionTemplatesCoverAllDimensions(t *testing.T) {
	for dim := range scoringtypes.DefaultWeights() {
		if _, ok := dimensionTemplates[scoringtypes.Dimension(dim)]; !ok {
			t.Errorf("dimension %q has a scoring weight but no proposal template — a low score here would silently get no proposal and could re-deadlock a mandatory review", dim)
		}
	}
}

// TestGenerateFallbackProposal_ResolvesBGradeDeadlock is the proof the B-grade
// deadlock path is closed: a mandatory review with EMPTY LowDimensions (B-grade
// task upgraded to mandatory due to missing hooks, every dim ≥70). With no low
// dims, GenerateProposalsForReview returns 0; the fallback must create a
// proposal that accept can resolve.
func TestGenerateFallbackProposal_ResolvesBGradeDeadlock(t *testing.T) {
	setHomeTemp(t)
	tmpRoot := t.TempDir()
	taskRef := "task-bgrade"

	review := &ReviewRequest{
		TaskRef:       taskRef,
		Score:         75,
		Grade:         "B",
		Mandatory:     true,
		Status:        ReviewPending,
		LowDimensions: nil, // empty — the dangerous case
		CreatedAt:     time.Now(),
	}
	if err := SaveReview(tmpRoot, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}

	// Empty lows must yield zero from the dimension-driven path (old deadlock).
	n, err := GenerateProposalsForReview(tmpRoot, taskRef, nil)
	if err != nil {
		t.Fatalf("GenerateProposalsForReview: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 from empty lows, got %d", n)
	}

	// Fallback must create exactly one resolvable proposal.
	fn, err := GenerateFallbackProposal(tmpRoot, taskRef)
	if err != nil {
		t.Fatalf("GenerateFallbackProposal: %v", err)
	}
	if fn != 1 {
		t.Fatalf("fallback created %d, want 1", fn)
	}

	props, _ := ListProposals(tmpRoot, PropProposed)
	if len(props) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(props))
	}

	// Accepting it must resolve the review — deadlock broken.
	if err := AcceptProposal(tmpRoot, props[0].ID); err != nil {
		t.Fatalf("AcceptProposal: %v", err)
	}
	loaded, err := LoadReview(tmpRoot, taskRef)
	if err != nil {
		t.Fatalf("LoadReview: %v", err)
	}
	if loaded.Status != ReviewResolved {
		t.Errorf("review status = %q, want resolved (B-grade deadlock not broken)", loaded.Status)
	}
}

func TestGenerateFallbackProposal_Idempotent(t *testing.T) {
	tmpRoot := t.TempDir()
	if n, err := GenerateFallbackProposal(tmpRoot, "task-fallback-idem"); err != nil || n != 1 {
		t.Fatalf("first call: n=%d err=%v", n, err)
	}
	// Second call must be a no-op — the review already has a proposal.
	if n, err := GenerateFallbackProposal(tmpRoot, "task-fallback-idem"); err != nil || n != 0 {
		t.Errorf("second call: n=%d err=%v, want 0 (idempotent)", n, err)
	}
	props, _ := ListProposals(tmpRoot, "")
	if len(props) != 1 {
		t.Errorf("after two calls, %d proposals stored, want 1", len(props))
	}
}

// TestGenerateForExistingReview_MandatoryEmptyLowsBackfills verifies the
// `forge experience generate` repair path also backfills a fallback for an
// existing mandatory review with empty LowDimensions (otherwise the documented
// repair command would print "Generated 0" and leave the user blocked).
func TestGenerateForExistingReview_MandatoryEmptyLowsBackfills(t *testing.T) {
	setHomeTemp(t)
	tmpRoot := t.TempDir()
	review := &ReviewRequest{
		TaskRef:   "task-bgrade-existing",
		Score:     75,
		Grade:     "B",
		Mandatory: true,
		Status:    ReviewPending,
		CreatedAt: time.Now(),
	}
	if err := SaveReview(tmpRoot, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	n, err := GenerateForExistingReview(tmpRoot, "task-bgrade-existing")
	if err != nil {
		t.Fatalf("GenerateForExistingReview: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 fallback proposal for empty-lows mandatory review, got %d", n)
	}
}

// TestResolveReview_ResolvesIndependently proves the decoupling: a review can be
// resolved WITHOUT any proposal to accept. Before this, AcceptProposal was the
// only path to ReviewResolved, so a review with zero proposals deadlocked.
func TestResolveReview_ResolvesIndependently(t *testing.T) {
	tmpRoot := t.TempDir()
	taskRef := "task-resolve"
	review := &ReviewRequest{
		TaskRef:   taskRef,
		Score:     60,
		Grade:     "D",
		Mandatory: true,
		Status:    ReviewPending,
		CreatedAt: time.Now(),
	}
	if err := SaveReview(tmpRoot, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	// No proposals exist for this review — resolve must still work.
	if err := ResolveReview(tmpRoot, taskRef); err != nil {
		t.Fatalf("ResolveReview: %v", err)
	}
	loaded, err := LoadReview(tmpRoot, taskRef)
	if err != nil {
		t.Fatalf("LoadReview: %v", err)
	}
	if loaded.Status != ReviewResolved {
		t.Errorf("status = %q, want resolved (decoupling failed)", loaded.Status)
	}
}

func TestProposalsForReview_FiltersByTaskRef(t *testing.T) {
	tmpRoot := t.TempDir()
	for _, ref := range []string{"task-a", "task-b"} {
		if err := SaveProposal(tmpRoot, &ExperienceProposal{
			SourceReview: ref,
			Category:     "gotchas",
			Title:        "title-" + ref,
			Status:       PropProposed,
		}); err != nil {
			t.Fatalf("SaveProposal %s: %v", ref, err)
		}
	}
	got, err := ProposalsForReview(tmpRoot, "task-a", PropProposed)
	if err != nil {
		t.Fatalf("ProposalsForReview: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 proposal for task-a, got %d", len(got))
	}
	if got[0].SourceReview != "task-a" {
		t.Errorf("SourceReview = %q, want task-a", got[0].SourceReview)
	}
}
