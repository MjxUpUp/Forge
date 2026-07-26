package scoring

import "time"

// EvaluateInput 持有给完成任务打分所需的全部数据。
type EvaluateInput struct {
	// GateHistory 是任务的 gate 通过/失败历史。
	GateHistory GateHistory

	// 时间范围，用于 efficiency 打分。
	StartedAt   time.Time
	CompletedAt time.Time

	// Git diff 数据——空字符串表示「不可用」（非致命）。
	GitDiffStat string // git diff --numstat 输出（「added\tdeleted\tpath」）

	// Test-coverage gate 裁决，来自 checklog 的 test-coverage-gate 条目（带实时
	// CheckTestCoverage fallback 接到 cli.scoreTask）。替代旧的 GitDiffTest 行比例
	// 启发式——后者在任务改动早于 `task start` 提交时返回常量 20（HeadCommit == HEAD
	// → 空 diff → 「检测不到测试行」）。Checked=false（gate 未运行）打中性分；
	// Checked=true 按裁决打分。
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

	// Hook 结果。
	CompilePassed   bool // auto-compile gate 通过
	AssertionPassed bool // assertion-check 通过

	// 标志位，指示 hook 数据是否可用（vs 未运行）。
	CompileChecked   bool
	AssertionChecked bool

	// 证据链来源分布（来自 checklog EvidenceChain）：deterministic=hook/gate 实跑，
	// agent-claim=agent 自述。可观测先行，不参与打分——Evaluate 据此构造
	// ScoreResult.Evidence，供 review/评分消费者判断"完成声明可信度"。
	EvidenceDeterministic int
	EvidenceAgentClaim    int
}

// GateHistory 抽象 gate 结果数据，避免 import taskpipeline。
type GateHistory struct {
	TotalGates int
	Passed     int
	Retries    int // 先前失败、retry 后通过的 gate 数
}
