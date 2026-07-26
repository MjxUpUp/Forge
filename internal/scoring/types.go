package scoring

import "time"

// EvaluateInput holds all the data needed to score a completed task.
//
// EvaluateInput 持有给完成任务打分所需的全部数据。
type EvaluateInput struct {
	// GateHistory is the gate pass/fail history of the task.
	//
	// GateHistory 是任务的 gate 通过/失败历史。
	GateHistory GateHistory

	// Time range, used for efficiency scoring.
	//
	// 时间范围，用于 efficiency 打分。
	StartedAt   time.Time
	CompletedAt time.Time

	// Git diff data — empty string means `unavailable` (non-fatal).
	//
	// Git diff 数据——空字符串表示「不可用」（非致命）。
	// GitDiffStat: git diff --numstat output (`added\tdeleted\tpath`).
	GitDiffStat string // git diff --numstat 输出（「added\tdeleted\tpath」）

	// Test-coverage gate verdict, from the test-coverage-gate entry in checklog (with real-time
	// CheckTestCoverage fallback wired to cli.scoreTask). Replaces the old GitDiffTest line-ratio
	// heuristic — the latter returned a constant 20 when task changes predate the `task start` commit (HeadCommit == HEAD
	// → empty diff → `no test lines detected`). Checked=false (gate did not run) scores neutral;
	// Checked=true scores by the verdict.
	//
	// Test-coverage gate 裁决，来自 checklog 的 test-coverage-gate 条目（带实时
	// CheckTestCoverage fallback 接到 cli.scoreTask）。替代旧的 GitDiffTest 行比例
	// 启发式——后者在任务改动早于 `task start` 提交时返回常量 20（HeadCommit == HEAD
	// → 空 diff → 「检测不到测试行」）。Checked=false（gate 未运行）打中性分；
	// Checked=true 按裁决打分。
	TestCoveragePassed  bool
	TestCoverageChecked bool

	// TestCoverageCovered/Total drives continuous scoring of the testing dimension (ratio=covered/total).
	// Replaces the old binary all-or-20 model: 4 of 5 source files covered → ~86 score instead of collapsing to 20.
	// Both come from real-time CheckTestCoverage (objective, same input and logic as the gate).
	//
	// TestCoverageCovered/Total 驱动 testing 维度的连续打分（ratio=covered/total）。
	// 替代旧的二值全或 20 模型：5 个源码文件覆盖 4 个 → ~86 分而非塌缩到 20。
	// 两者均来自实时 CheckTestCoverage（客观，与门禁同输入同逻辑）。
	// TestCoverageCovered: number of source files with paired tests.
	TestCoverageCovered int // 有配对测试的源码文件数
	// TestCoverageTotal: number of source files that should have paired tests.
	TestCoverageTotal   int // 应配对测试的源码文件数

	// Assertion-density signal, used for fake-test detection (industry STREW Assertion-McCabe ratio).
	// A test file with only setup/log and no assertions is not real coverage — the testing dimension down-scores cases where covered>0
	// but AssertionCount==0.
	//
	// 断言密度信号，用于假测试检测（业界 STREW Assertion-McCabe ratio）。
	// 只有 setup/log 无断言的测试文件不是真覆盖——testing 维度对 covered>0
	// 但 AssertionCount==0 的情况降分。
	// TestAssertionCount: total assertion markers in changed test files.
	TestAssertionCount int // changed 测试文件的断言标记总数
	// TestFileCount: number of changed test files.
	TestFileCount      int // changed 测试文件数

	// Hook results.
	//
	// Hook 结果。
	// CompilePassed: auto-compile gate passed.
	CompilePassed   bool // auto-compile gate 通过
	// AssertionPassed: assertion-check passed.
	AssertionPassed bool // assertion-check 通过

	// Flag indicating whether hook data is available (vs. not run).
	//
	// 标志位，指示 hook 数据是否可用（vs 未运行）。
	CompileChecked   bool
	AssertionChecked bool

	// Evidence-chain source distribution (from checklog EvidenceChain): deterministic=hook/gate actually run,
	// agent-claim=agent self-report. Observability first, not part of scoring — Evaluate uses this to build
	// ScoreResult.Evidence so review/scoring consumers can judge `completion-claim credibility`.
	//
	// 证据链来源分布（来自 checklog EvidenceChain）：deterministic=hook/gate 实跑，
	// agent-claim=agent 自述。可观测先行，不参与打分——Evaluate 据此构造
	// ScoreResult.Evidence，供 review/评分消费者判断"完成声明可信度"。
	EvidenceDeterministic int
	EvidenceAgentClaim    int
}

// GateHistory abstracts gate result data to avoid importing taskpipeline.
//
// GateHistory 抽象 gate 结果数据，避免 import taskpipeline。
type GateHistory struct {
	TotalGates int
	Passed     int
	// Retries: number of gates that previously failed and passed after retry.
	Retries    int // 先前失败、retry 后通过的 gate 数
}
