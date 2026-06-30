package cli

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestTestCoverageVerdict_RecordedPass guards the B3 happy path: the task-verify
// gate ran test-coverage and passed → scoreTask reads (true, true) so the
// testing dimension scores 100, NOT the old heuristic's 20 that fired whenever
// the task's changes were committed before `task start` (empty diff).
func TestTestCoverageVerdict_RecordedPass(t *testing.T) {
	checks := map[checklog.CheckName]*checklog.Entry{
		taskpipeline.CheckNameTestCoverage: {Check: taskpipeline.CheckNameTestCoverage, Passed: true, Checked: true},
	}
	passed, checked := testCoverageVerdict(checks)
	if !passed || !checked {
		t.Fatalf("recorded pass: want (true, true), got (%v, %v)", passed, checked)
	}
}

// TestTestCoverageVerdict_RecordedFail guards that a genuine gate FAILURE (source
// changed without a test) flows through as (false, true) → testing=20. This is
// the legitimate low score, distinct from the old bug where an empty diff also
// yielded 20 for tasks that DID have tests.
func TestTestCoverageVerdict_RecordedFail(t *testing.T) {
	checks := map[checklog.CheckName]*checklog.Entry{
		taskpipeline.CheckNameTestCoverage: {Check: taskpipeline.CheckNameTestCoverage, Passed: false, Checked: true},
	}
	passed, checked := testCoverageVerdict(checks)
	if passed || !checked {
		t.Fatalf("recorded fail: want (false, true), got (%v, %v)", passed, checked)
	}
}

// TestTestCoverageVerdict_NoRecordReturnsUnchecked guards the cross-session
// fallback: when no test-coverage entry exists (e.g. scoring in a different
// session than task-verify), return (false, false) so scoreTask evaluates the
// coverage live rather than silently scoring neutral.
func TestTestCoverageVerdict_NoRecordReturnsUnchecked(t *testing.T) {
	passed, checked := testCoverageVerdict(map[checklog.CheckName]*checklog.Entry{})
	if passed || checked {
		t.Fatalf("no record: want (false, false) to trigger live fallback, got (%v, %v)", passed, checked)
	}
}
