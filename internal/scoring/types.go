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

	// TestCoverageCovered/Total 驱动 testing 维度的连续打分（ratio=covered/total）。
	// 替代旧的二值全或 20 模型：5 个源码文件覆盖 4 个 → ~86 分而非塌缩到 20。
	// 两者均来自实时 CheckTestCoverage（客观，与门禁同输入同逻辑）。
	TestCoverageCovered int // 有配对测试的源码文件数
	TestCoverageTotal   int // 应配对测试的源码文件数

	// 断言密度信号，用于假测试检测（业界 STREW Assertion-McCabe ratio）。
	// 只有 setup/log 无断言的测试文件不是真覆盖——testing 维度对 covered>0
	// 但 AssertionCount==0 的情况降分。
	TestAssertionCount int // changed 测试文件的断言标记总数
	TestFileCount      int // changed 测试文件数

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
