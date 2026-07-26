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
	return map[string]float64{
		string(DimensionProcess):     0.25,
		string(DimensionTesting):     0.25,
		string(DimensionCodeQuality): 0.20,
		string(DimensionAssertions):  0.15,
		string(DimensionScope):       0.10,
		string(DimensionEfficiency):  0.05,
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
	for _, grade := range []string{"A", "B", "C", "D", "F"} {
		if score >= thresholds[grade] {
			return grade
		}
	}
	return "F"
}
