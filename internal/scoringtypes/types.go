// Package scoringtypes defines shared types for task quality scoring.
// Zero-dependency — shared by the scoring and taskpipeline packages to avoid
// circular imports.
//
// Package scoringtypes 定义 task 质量评分的共享类型。
// 零依赖——scoring 与 taskpipeline 两个包共用，避免循环 import。
package scoringtypes

import "time"

// Dimension identifies a scoring dimension.
//
// Dimension 标识一个评分维度。
type Dimension string

const (
	DimensionProcess     Dimension = "process"      // Gate pass rate, retries
	DimensionTesting     Dimension = "testing"      // Test file presence and ratio
	DimensionCodeQuality Dimension = "code-quality" // Compile gate result
	DimensionAssertions  Dimension = "assertions"   // Assertion hook result
	DimensionScope       Dimension = "scope"        // Change size (lines)
	DimensionEfficiency  Dimension = "efficiency"   // Time to complete
	DimensionExpression  Dimension = "expression"   // Doc-artifact readability (doclint L1 + rubric L2 evidence)
)

// DimensionScore holds the score and explanation for a single dimension.
//
// DimensionScore 存单个维度的分数与说明。
type DimensionScore struct {
	Dimension Dimension `json:"dimension"`
	Score     int       `json:"score"`  // 0-100
	Detail    string    `json:"detail"` // One-sentence justification
}

// ScoreResult is the output of a task quality evaluation.
//
// ScoreResult 是 task 质量评估的输出。
type ScoreResult struct {
	TaskRef    string           `json:"task_ref"`
	Dimensions []DimensionScore `json:"dimensions"`
	Overall    float64          `json:"overall"` // Weighted average 0-100
	Grade      string           `json:"grade"`   // A/B/C/D/F
	ScoredAt   time.Time        `json:"scored_at"`
	// Evidence summarizes the source distribution of this task's evidence chain
	// (deterministic vs agent-claim). Observability first, not part of scoring: it lets
	// review/scoring consumers see how much deterministic evidence backs a completion
	// claim, hedging against the LLM-judge blind spot where the agent skips prerequisites
	// and still declares completion. nil = no evidence data.
	//
	// Evidence 摘要本任务证据链的来源分布（deterministic vs agent-claim）。可观测先行，
	// 不参与打分：让 review/评分消费者看到"完成声明背后有多少 deterministic 证据"，
	// 对冲 LLM-judge 看不出"agent 跳过前置就声明完成"的盲区。nil=无证据数据。
	Evidence *EvidenceSummary `json:"evidence,omitempty"`
	// CappedReason, when non-empty, records that Overall was capped after evaluation and why
	// (currently: the task used an escape hatch — per-task override or env-form escape — so its
	// total is capped at 89, the top of the B band: escape makes A unreachable, and escape must
	// have a visible cost instead of still taking home 96-99/A). Observability only; the cap
	// itself is applied by taskpipeline.ScoreTask, not Evaluate, so golden fixtures are unaffected.
	//
	// CappedReason 非空时记录 Overall 在评分后被封顶及原因（当前：任务用了逃生舱——
	// per-task override 或 env 形式逃生——总分封顶 89（B 档上限）：逃生拿不到 A，
	// 逃生必须有可见代价，不能照拿 96-99/A）。仅可观测；封顶由
	// taskpipeline.ScoreTask 施加而非 Evaluate，故 golden fixture 不受影响。
	CappedReason string `json:"capped_reason,omitempty"`
}

// EvidenceSummary summarizes the source distribution of a task's evidence chain.
// Deterministic = hook/gate actually ran (unforgeable); AgentClaim = agent self-report.
// Ratio = Deterministic/Total, a hard signal for `completion-claim credibility` —
// downstream steps can use it to trigger review or fold it into scoring.
//
// EvidenceSummary 摘要任务证据链的来源分布。Deterministic=hook/gate 实跑（不可伪造），
// AgentClaim=agent 自述。Ratio=Deterministic/Total，是"完成声明可信度"的硬信号——
// 后续步骤可据此驱动 review 触发或纳入打分。
type EvidenceSummary struct {
	Deterministic int     `json:"deterministic"`
	AgentClaim    int     `json:"agent_claim"`
	Total         int     `json:"total"`
	Ratio         float64 `json:"ratio"` // 0-1；total=0 时为 0
	// ReviewPasses / CompleteRejections are the review-rework loop metric (技控 result
	// indicator): how many `forge review pass` events and how many task-complete rejections
	// the task went through. Observability only — deliberately NOT a scoring dimension
	// (folding it into weights would invite Goodhart gaming). Filled by taskpipeline.ScoreTask
	// from TaskState history; zero values mean no review/rework recorded.
	//
	// ReviewPasses / CompleteRejections 是审查-返工循环度量（技控结果指标）：本任务经历
	// 的 `forge review pass` 次数与 task-complete 被拒次数。仅可观测——刻意不进评分
	// 维度（进权重会引来 Goodhart 博弈）。由 taskpipeline.ScoreTask 从 TaskState 历史
	// 填充；零值 = 无审查/返工记录。
	ReviewPasses       int `json:"review_passes,omitempty"`
	CompleteRejections int `json:"complete_rejections,omitempty"`
}

// ScoringConfig controls dimension weights and grade thresholds.
//
// ScoringConfig 控制维度权重与 grade 阈值。
type ScoringConfig struct {
	Weights    map[string]float64 `yaml:"weights"    json:"weights"`    // dimension id -> weight (must sum to 1.0)
	Thresholds map[string]float64 `yaml:"thresholds" json:"thresholds"` // grade -> minimum score
}

// DefaultWeights returns the standard dimension weights.
//
// DefaultWeights 返回标准维度权重。
func DefaultWeights() map[string]float64 {
	// v1.43: expression (0.10) added — taken from process/testing/code-quality/assertions
	// proportionally; still sums to 1.0. Tasks without doc deliverables score the dimension
	// neutral 100, so pure-code tasks are unaffected.
	//
	// v1.43：新增 expression（0.10）——从 process/testing/code-quality/assertions 按比例
	// 匀出；合计仍为 1.0。无文档产物的任务该维度打中性 100 分，纯代码任务不受影响。
	return map[string]float64{
		string(DimensionProcess):     0.22,
		string(DimensionTesting):     0.23,
		string(DimensionCodeQuality): 0.18,
		string(DimensionAssertions):  0.12,
		string(DimensionScope):       0.10,
		string(DimensionEfficiency):  0.05,
		string(DimensionExpression):  0.10,
	}
}

// DefaultThresholds returns the standard grade thresholds.
//
// DefaultThresholds 返回标准 grade 阈值。
func DefaultThresholds() map[string]float64 {
	return map[string]float64{
		"A": 90,
		"B": 80,
		"C": 70,
		"D": 60,
		"F": 0,
	}
}

// GradeFromScore maps a numeric score to a letter grade.
//
// GradeFromScore 把数值分数映射到字母 grade。
func GradeFromScore(score float64, thresholds map[string]float64) string {
	def := DefaultThresholds()
	for _, grade := range []string{"A", "B", "C", "D", "F"} {
		v, ok := thresholds[grade]
		// A missing key would read as 0.0 and let any score >= 0 grab that grade
		// (partial user config → everything A) — fall back to the default threshold
		// instead of trusting the zero value.
		//
		// 缺键会读成 0.0，任意 score >= 0 直接命中该档（用户部分配置 → 全 A）——
		// 回退默认阈值而不是信任零值。
		if !ok {
			v = def[grade]
		}
		if score >= v {
			return grade
		}
	}
	return "F"
}
