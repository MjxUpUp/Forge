package scoring

import "time"

// EvaluateInput holds all data needed to score a completed task.
type EvaluateInput struct {
	// GateHistory is the task's gate pass/fail history.
	GateHistory GateHistory

	// Time range for efficiency scoring.
	StartedAt   time.Time
	CompletedAt time.Time

	// Git diff data — empty strings mean "unavailable" (non-fatal).
	GitDiffTest string // git diff content for test files
	GitDiffStat string // git diff --stat output

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
