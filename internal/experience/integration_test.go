package experience

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/knowledge"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// setHomeTemp creates a temp directory and sets HOME and USERPROFILE to it
// (Windows uses USERPROFILE for os.UserHomeDir). Returns the temp dir.
func setHomeTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	origProfile := os.Getenv("USERPROFILE")
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	t.Cleanup(func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origProfile)
	})
	return dir
}

func TestAcceptProposal_CreatesKnowledgeEntry(t *testing.T) {
	homeDir := setHomeTemp(t)
	tmpRoot := t.TempDir()

	taskRef := "task-001"
	prop := &ExperienceProposal{
		SourceReview: taskRef,
		Category:     "gotchas",
		Title:        "Avoid bare asserts",
		Description:  "Always include a message in assertions",
		Patterns:     []string{"assert\\."},
		Severity:     "error",
		Status:       PropProposed,
		CreatedAt:    time.Now(),
	}
	if err := SaveProposal(tmpRoot, prop); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	if err := AcceptProposal(tmpRoot, prop.ID); err != nil {
		t.Fatalf("AcceptProposal: %v", err)
	}

	idx, err := knowledge.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	found := false
	for _, e := range idx.Entries {
		if e.ID == prop.ID {
			found = true
			if e.Source != "auto-extracted:"+taskRef {
				t.Errorf("entry Source = %q, want %q", e.Source, "auto-extracted:"+taskRef)
			}
			if e.Category != prop.Category {
				t.Errorf("entry Category = %q, want %q", e.Category, prop.Category)
			}
			if e.Title != prop.Title {
				t.Errorf("entry Title = %q, want %q", e.Title, prop.Title)
			}
			break
		}
	}
	if !found {
		// Print the knowledge dir for debugging
		knowledgeDir := filepath.Join(homeDir, ".forge", "knowledge")
		entries, _ := os.ReadDir(knowledgeDir)
		t.Errorf("knowledge entry %q not found in index; knowledge dir entries: %v", prop.ID, entries)
	}
}

func TestAcceptProposal_UpdatesProposalStatus(t *testing.T) {
	setHomeTemp(t)
	tmpRoot := t.TempDir()

	prop := &ExperienceProposal{
		SourceReview: "task-002",
		Category:     "patterns",
		Title:        "Use context timeout",
		Description:  "Always set a timeout on HTTP calls",
		Patterns:     []string{"http\\.Get"},
		Severity:     "warning",
		Status:       PropProposed,
		CreatedAt:    time.Now(),
	}
	if err := SaveProposal(tmpRoot, prop); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	if err := AcceptProposal(tmpRoot, prop.ID); err != nil {
		t.Fatalf("AcceptProposal: %v", err)
	}

	loaded, err := LoadProposal(tmpRoot, prop.ID)
	if err != nil {
		t.Fatalf("LoadProposal: %v", err)
	}
	if loaded.Status != PropAccepted {
		t.Errorf("proposal status = %q, want %q", loaded.Status, PropAccepted)
	}
}

func TestAcceptProposal_UpdatesReviewStatus(t *testing.T) {
	setHomeTemp(t)
	tmpRoot := t.TempDir()

	taskRef := "task-review-test"
	// Create a review for the task.
	review := &ReviewRequest{
		TaskRef:   taskRef,
		Score:     55,
		Grade:     "F",
		Mandatory: true,
		Status:    ReviewPending,
		CreatedAt: time.Now(),
	}
	if err := SaveReview(tmpRoot, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}

	prop := &ExperienceProposal{
		SourceReview: taskRef,
		Category:     "gotchas",
		Title:        "Missing error check",
		Description:  "Always check errors from os calls",
		Patterns:     []string{"os\\.Open"},
		Severity:     "error",
		Status:       PropProposed,
		CreatedAt:    time.Now(),
	}
	if err := SaveProposal(tmpRoot, prop); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	if err := AcceptProposal(tmpRoot, prop.ID); err != nil {
		t.Fatalf("AcceptProposal: %v", err)
	}

	updated, err := LoadReview(tmpRoot, taskRef)
	if err != nil {
		t.Fatalf("LoadReview: %v", err)
	}
	if updated.Status != ReviewResolved {
		t.Errorf("review status = %q, want %q", updated.Status, ReviewResolved)
	}
}

func TestAcceptProposal_OnNonProposedReturnsError(t *testing.T) {
	setHomeTemp(t)
	tmpRoot := t.TempDir()

	prop := &ExperienceProposal{
		SourceReview: "task-003",
		Category:     "apis",
		Title:        "Use io.ReadAll",
		Description:  "Prefer io.ReadAll over ioutil",
		Patterns:     []string{"ioutil"},
		Severity:     "info",
		Status:       PropAccepted,
		CreatedAt:    time.Now(),
	}
	if err := SaveProposal(tmpRoot, prop); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	err := AcceptProposal(tmpRoot, prop.ID)
	if err == nil {
		t.Fatal("expected error when accepting non-proposed proposal, got nil")
	}
	want := "proposal " + prop.ID + " is accepted, not proposed"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestAcceptProposal_NonexistentReturnsError(t *testing.T) {
	setHomeTemp(t)
	tmpRoot := t.TempDir()

	err := AcceptProposal(tmpRoot, "exp-nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent proposal, got nil")
	}
}

func TestRejectProposal_ChangesStatus(t *testing.T) {
	tmpRoot := t.TempDir()

	prop := &ExperienceProposal{
		SourceReview: "task-004",
		Category:     "patterns",
		Title:        "Deprecated API usage",
		Description:  "Use v2 API instead",
		Patterns:     []string{"v1/api"},
		Severity:     "warning",
		Status:       PropProposed,
		CreatedAt:    time.Now(),
	}
	if err := SaveProposal(tmpRoot, prop); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	if err := RejectProposal(tmpRoot, prop.ID); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	loaded, err := LoadProposal(tmpRoot, prop.ID)
	if err != nil {
		t.Fatalf("LoadProposal: %v", err)
	}
	if loaded.Status != PropRejected {
		t.Errorf("proposal status = %q, want %q", loaded.Status, PropRejected)
	}
}

func TestRejectProposal_DoesNotModifyKnowledgeStore(t *testing.T) {
	homeDir := setHomeTemp(t)
	tmpRoot := t.TempDir()

	prop := &ExperienceProposal{
		SourceReview: "task-005",
		Category:     "gotchas",
		Title:        "Unused variable",
		Description:  "Remove unused variables",
		Patterns:     []string{"var\\s+\\w+\\s+"},
		Severity:     "info",
		Status:       PropProposed,
		CreatedAt:    time.Now(),
	}
	if err := SaveProposal(tmpRoot, prop); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	if err := RejectProposal(tmpRoot, prop.ID); err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	// Verify knowledge store has no entries
	idx, err := knowledge.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if len(idx.Entries) != 0 {
		t.Errorf("knowledge index has %d entries, want 0", len(idx.Entries))
	}

	// Also verify no knowledge files were written
	knowledgeDir := filepath.Join(homeDir, ".forge", "knowledge")
	if _, err := os.Stat(knowledgeDir); !os.IsNotExist(err) {
		// Dir exists — check for files
		entries, _ := os.ReadDir(knowledgeDir)
		for _, e := range entries {
			if e.Name() != "index.json" {
				t.Errorf("unexpected file in knowledge dir: %s", e.Name())
			}
		}
	}
}

// TestAutoAcceptHighConfidence_AcceptsSevereBelowThreshold verifies that
// proposals for severely low-scoring dimensions (score < 60) are auto-accepted
// into the global knowledge store, while borderline dimensions (60-69) are
// left proposed for a human. This is the fix for the empty-knowledge loop
// observed in real heavy-use projects.
func TestAutoAcceptHighConfidence_AcceptsSevereBelowThreshold(t *testing.T) {
	homeDir := setHomeTemp(t)
	tmpRoot := t.TempDir()

	taskRef := "task-auto-1"
	// Create a mandatory review so auto-accept can resolve it.
	review := &ReviewRequest{
		TaskRef:   taskRef,
		Score:     50,
		Grade:     "F",
		Mandatory: true,
		Status:    ReviewPending,
		CreatedAt: time.Now(),
	}
	if err := SaveReview(tmpRoot, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}

	// One severe dim (40 < 60 → auto-accept), one borderline (65 ≥ 60 → manual).
	lows := []LowDimension{
		{Dimension: scoringtypes.DimensionCodeQuality, Score: 40},
		{Dimension: scoringtypes.DimensionScope, Score: 65},
	}
	if n, err := GenerateProposalsForReview(tmpRoot, taskRef, lows); err != nil || n != 2 {
		t.Fatalf("GenerateProposalsForReview: n=%d err=%v (want 2 proposals)", n, err)
	}

	accepted, err := AutoAcceptHighConfidence(tmpRoot, taskRef, lows)
	if err != nil {
		t.Fatalf("AutoAcceptHighConfidence: %v", err)
	}
	if accepted != 1 {
		t.Fatalf("auto-accepted %d, want 1 (only the <60 dimension)", accepted)
	}

	// Knowledge store holds exactly the severe one, tagged auto-accepted.
	idx, err := knowledge.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("knowledge index has %d entries, want 1; dir: %s", len(idx.Entries),
			filepath.Join(homeDir, ".forge", "knowledge"))
	}
	e := idx.Entries[0]
	wantSrc := "auto-accepted:high-confidence:" + taskRef
	if e.Source != wantSrc {
		t.Errorf("entry Source = %q, want %q", e.Source, wantSrc)
	}

	// The borderline dim's proposal stays proposed (not auto-accepted).
	proposals, err := ProposalsForReview(tmpRoot, taskRef, PropProposed)
	if err != nil {
		t.Fatalf("ProposalsForReview: %v", err)
	}
	if len(proposals) != 1 {
		t.Errorf("expected 1 proposal still proposed (the borderline one), got %d", len(proposals))
	}

	// Severe low score resolved the mandatory review → unblocks task completion.
	updated, err := LoadReview(tmpRoot, taskRef)
	if err != nil {
		t.Fatalf("LoadReview: %v", err)
	}
	if updated.Status != ReviewResolved {
		t.Errorf("review status = %q, want %q (severe auto-accept should resolve)", updated.Status, ReviewResolved)
	}
}

// TestAutoAcceptHighConfidence_NoOpWhenAllBorderline verifies that when every
// low dimension is borderline (60-69), nothing is auto-accepted — the human
// stays in control and the review is not auto-resolved.
func TestAutoAcceptHighConfidence_NoOpWhenAllBorderline(t *testing.T) {
	setHomeTemp(t)
	tmpRoot := t.TempDir()

	taskRef := "task-auto-2"
	review := &ReviewRequest{
		TaskRef: taskRef, Score: 68, Grade: "D", Mandatory: true,
		Status: ReviewPending, CreatedAt: time.Now(),
	}
	if err := SaveReview(tmpRoot, review); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	lows := []LowDimension{
		{Dimension: scoringtypes.DimensionScope, Score: 65},
	}
	GenerateProposalsForReview(tmpRoot, taskRef, lows)

	accepted, err := AutoAcceptHighConfidence(tmpRoot, taskRef, lows)
	if err != nil {
		t.Fatalf("AutoAcceptHighConfidence: %v", err)
	}
	if accepted != 0 {
		t.Fatalf("auto-accepted %d, want 0 (all borderline)", accepted)
	}

	idx, _ := knowledge.LoadIndex()
	if idx != nil && len(idx.Entries) != 0 {
		t.Errorf("knowledge should be empty for borderline-only, got %d entries", len(idx.Entries))
	}
	updated, _ := LoadReview(tmpRoot, taskRef)
	if updated.Status != ReviewPending {
		t.Errorf("review should stay pending for borderline, got %q", updated.Status)
	}
}
