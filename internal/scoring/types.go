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

	// Tool usage data for tool-selection and skill-hit scoring.
	ToolCalls        int              // total tool calls recorded
	AntiPatterns     []AntiPatternHit // detected anti-pattern violations
	SkillHits        []SkillHitData   // detected skill invocations
	RecommendedSkills int             // number of recommended skills for the task
	ToolCounts       map[string]int   // tool_name -> call count
}

// GateHistory abstracts the gate result data to avoid importing taskpipeline.
type GateHistory struct {
	TotalGates int
	Passed     int
	Retries    int // gates that failed then passed on retry
}

// AntiPatternHit represents a tool anti-pattern violation for scoring.
type AntiPatternHit struct {
	RuleID     string
	ToolName   string
	PreferTool string
	Severity   string // "major" or "minor"
	Detail     string
}

// SkillHitData represents a skill invocation detected during task execution.
type SkillHitData struct {
	SkillName string
	Source    string // "skill-tool" or "forge-cli"
}
