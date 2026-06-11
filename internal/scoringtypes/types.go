// Package scoringtypes defines shared types for task quality scoring.
// Zero dependencies — used by both scoring and taskpipeline packages
// to avoid circular imports.
package scoringtypes

import "time"

// Dimension identifies a scoring axis.
type Dimension string

const (
	DimensionProcess       Dimension = "process"        // Gate pass rate, retries
	DimensionTesting       Dimension = "testing"        // Test file presence and ratio
	DimensionCodeQuality   Dimension = "code-quality"   // Compile gate result
	DimensionAssertions    Dimension = "assertions"     // Assertion hook result
	DimensionScope         Dimension = "scope"          // Change size (lines)
	DimensionEfficiency    Dimension = "efficiency"     // Time to complete
	DimensionToolSelection Dimension = "tool-selection" // Tool choice quality
	DimensionSkillHit      Dimension = "skill-hit"      // Skill usage rate
)

// DimensionScore holds the score and explanation for one dimension.
type DimensionScore struct {
	Dimension Dimension `json:"dimension"`
	Score     int       `json:"score"`  // 0-100
	Detail    string    `json:"detail"` // One-sentence justification
}

// ScoreResult is the output of a task quality evaluation.
type ScoreResult struct {
	TaskRef    string           `json:"task_ref"`
	Dimensions []DimensionScore `json:"dimensions"`
	Overall    float64          `json:"overall"` // Weighted average 0-100
	Grade      string           `json:"grade"`   // A/B/C/D/F
	ScoredAt   time.Time        `json:"scored_at"`
}

// ScoringConfig controls dimension weights and grade thresholds.
type ScoringConfig struct {
	Weights    map[string]float64 `yaml:"weights"    json:"weights"`    // dimension id -> weight (must sum to 1.0)
	Thresholds map[string]float64 `yaml:"thresholds" json:"thresholds"` // grade -> minimum score
}

// DefaultWeights returns the standard dimension weights.
func DefaultWeights() map[string]float64 {
	return map[string]float64{
		string(DimensionProcess):       0.20,
		string(DimensionTesting):       0.20,
		string(DimensionCodeQuality):   0.15,
		string(DimensionAssertions):    0.12,
		string(DimensionScope):         0.08,
		string(DimensionEfficiency):    0.05,
		string(DimensionToolSelection): 0.12,
		string(DimensionSkillHit):      0.08,
	}
}

// DefaultThresholds returns the standard grade thresholds.
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
func GradeFromScore(score float64, thresholds map[string]float64) string {
	for _, grade := range []string{"A", "B", "C", "D", "F"} {
		if score >= thresholds[grade] {
			return grade
		}
	}
	return "F"
}
