package scoring

import "time"

// EvaluateInput holds all data needed to score a completed task.
type EvaluateInput struct {
	// GateHistory is the task's gate pass/fail history.
	GateHistory GateHistory

	// Time range for efficiency scoring.
	StartedAt   time.Time
	CompletedAt time.Time

	// Git diff data — empty string means "unavailable" (non-fatal).
	GitDiffStat string // git diff --numstat output ("added\tdeleted\tpath")

	// Test-coverage gate verdict, sourced from checklog's test-coverage-gate
	// entry (with a live CheckTestCoverage fallback wired in cli.scoreTask).
	// Replaces the old GitDiffTest line-ratio heuristic, which returned a
	// constant 20 when a task's changes were committed before `task start`
	// (HeadCommit == HEAD → empty diff → "no test lines detected"). Checked=false
	// (gate never ran) scores neutral; Checked=true scores from the verdict.
	TestCoveragePassed  bool
	TestCoverageChecked bool

	// Hook results.
	CompilePassed   bool // auto-compile gate passed
	AssertionPassed bool // assertion-check passed

	// Flags indicating whether hook data is available (vs not run).
	CompileChecked   bool
	AssertionChecked bool
}

// GateHistory abstracts the gate result data to avoid importing taskpipeline.
type GateHistory struct {
	TotalGates int
	Passed     int
	Retries    int // gates that failed then passed on retry
}
