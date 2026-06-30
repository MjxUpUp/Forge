package cli

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestDuplicateScoreWarnings_SameBranchSameHEAD guards the real duplicate case:
// a completed task on the same branch with the same HeadCommit is a re-score
// over an identical commit range and must warn.
func TestDuplicateScoreWarnings_SameBranchSameHEAD(t *testing.T) {
	now := time.Now()
	state := &taskpipeline.TaskState{TaskRef: "feat/a", Branch: "feat/a", HeadCommit: "abc1234"}
	other := &taskpipeline.TaskState{
		TaskRef: "feat/a-prev", Branch: "feat/a", HeadCommit: "abc1234", CompletedAt: &now,
	}
	warnings := duplicateScoreWarnings(state, []*taskpipeline.TaskState{other})
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning for same-branch same-HEAD completed task, got %d: %v", len(warnings), warnings)
	}
}

// TestDuplicateScoreWarnings_CrossBranchNoWarn guards against the false-positive
// regression: every feature branch forked from the same master HEAD records that
// same HeadCommit at task start, so cross-branch matches are normal, not
// duplicates. This is the bug that fired on every task complete in the
// 2026-06-14 self-bootstrap session (4 tasks all shared master's HEAD).
func TestDuplicateScoreWarnings_CrossBranchNoWarn(t *testing.T) {
	now := time.Now()
	state := &taskpipeline.TaskState{TaskRef: "feat/a", Branch: "feat/a", HeadCommit: "abc1234"}
	other := &taskpipeline.TaskState{
		TaskRef: "fix/b", Branch: "fix/b", HeadCommit: "abc1234", CompletedAt: &now,
	}
	warnings := duplicateScoreWarnings(state, []*taskpipeline.TaskState{other})
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings for cross-branch same-HEAD (false positive), got %d: %v", len(warnings), warnings)
	}
}

// TestDuplicateScoreWarnings_IncompleteNoWarn ensures an incomplete sibling
// (same branch, same HEAD, but not yet completed) does not warn — only completed
// tasks can be duplicates of a scoring task.
func TestDuplicateScoreWarnings_IncompleteNoWarn(t *testing.T) {
	state := &taskpipeline.TaskState{TaskRef: "feat/a", Branch: "feat/a", HeadCommit: "abc1234"}
	other := &taskpipeline.TaskState{
		TaskRef: "feat/a-prev", Branch: "feat/a", HeadCommit: "abc1234", CompletedAt: nil,
	}
	warnings := duplicateScoreWarnings(state, []*taskpipeline.TaskState{other})
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings for incomplete sibling, got %d: %v", len(warnings), warnings)
	}
}
