package scoring

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

func defaultConfig() *scoringtypes.ScoringConfig {
	return &scoringtypes.ScoringConfig{
		Weights:    scoringtypes.DefaultWeights(),
		Thresholds: scoringtypes.DefaultThresholds(),
	}
}

func TestScoreProcess_AllPassed(t *testing.T) {
	result := scoreProcess(GateHistory{TotalGates: 5, Passed: 5, Retries: 0})
	if result.Score != 100 {
		t.Fatalf("expected 100, got %d: %s", result.Score, result.Detail)
	}
	if result.Dimension != scoringtypes.DimensionProcess {
		t.Fatalf("expected process dimension, got %s", result.Dimension)
	}
}

func TestScoreProcess_WithRetries(t *testing.T) {
	result := scoreProcess(GateHistory{TotalGates: 5, Passed: 5, Retries: 2})
	if result.Score != 70 { // 100 - 2*15
		t.Fatalf("expected 70, got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreProcess_NoHistory(t *testing.T) {
	result := scoreProcess(GateHistory{TotalGates: 0})
	if result.Score != 70 {
		t.Fatalf("expected 70 (neutral), got %d", result.Score)
	}
}

func TestScoreTesting_HasTests(t *testing.T) {
	result := scoreTesting(true, true)
	if result.Score != 100 {
		t.Fatalf("expected 100 (tests present, coverage gate passed), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreTesting_MissingTests(t *testing.T) {
	result := scoreTesting(false, true)
	if result.Score != 20 {
		t.Fatalf("expected 20 (source changed without test), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreTesting_NotChecked(t *testing.T) {
	result := scoreTesting(false, false)
	if result.Score != 70 {
		t.Fatalf("expected 70 (coverage not checked, neutral), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreCodeQuality_Passed(t *testing.T) {
	result := scoreCodeQuality(true, true)
	if result.Score != 100 {
		t.Fatalf("expected 100, got %d", result.Score)
	}
}

func TestScoreCodeQuality_NotChecked(t *testing.T) {
	result := scoreCodeQuality(false, false)
	if result.Score != 50 {
		t.Fatalf("expected 50 (not checked), got %d", result.Score)
	}
}

func TestScoreCodeQuality_Failed(t *testing.T) {
	result := scoreCodeQuality(false, true)
	if result.Score != 0 {
		t.Fatalf("expected 0 (failed), got %d", result.Score)
	}
}

func TestScoreAssertions_Passed(t *testing.T) {
	result := scoreAssertions(true, true)
	if result.Score != 100 {
		t.Fatalf("expected 100, got %d", result.Score)
	}
}

func TestScoreAssertions_NotChecked(t *testing.T) {
	result := scoreAssertions(false, false)
	if result.Score != 70 {
		t.Fatalf("expected 70, got %d", result.Score)
	}
}

func TestScoreScope_Small(t *testing.T) {
	stat := "3\t2\tmain.go"
	result := scoreScope(stat)
	if result.Score != 100 {
		t.Fatalf("expected 100 (small), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreScope_Medium(t *testing.T) {
	stat := "50\t50\tmain.go"
	result := scoreScope(stat)
	if result.Score != 80 {
		t.Fatalf("expected 80 (medium), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreScope_Large(t *testing.T) {
	stat := "150\t150\tmain.go"
	result := scoreScope(stat)
	if result.Score != 60 {
		t.Fatalf("expected 60 (large), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreScope_VeryLarge(t *testing.T) {
	stat := "300\t300\tmain.go"
	result := scoreScope(stat)
	if result.Score != 40 {
		t.Fatalf("expected 40 (very large), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreEfficiency_Fast(t *testing.T) {
	start := time.Now().Add(-3 * time.Minute)
	end := time.Now()
	result := scoreEfficiency(start, end)
	if result.Score != 100 {
		t.Fatalf("expected 100 (fast), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreEfficiency_Slow(t *testing.T) {
	start := time.Now().Add(-90 * time.Minute)
	end := time.Now()
	result := scoreEfficiency(start, end)
	if result.Score != 40 {
		t.Fatalf("expected 40 (slow), got %d: %s", result.Score, result.Detail)
	}
}

func TestEvaluate_Full(t *testing.T) {
	input := &EvaluateInput{
		GateHistory: GateHistory{
			TotalGates: 5,
			Passed:     5,
			Retries:    0,
		},
		StartedAt:        time.Now().Add(-10 * time.Minute),
		CompletedAt:      time.Now(),
		GitDiffStat:         "5\t5\tmain.go",
		TestCoveragePassed:  true,
		TestCoverageChecked: true,
		CompilePassed:    true,
		CompileChecked:   true,
		AssertionPassed:  true,
		AssertionChecked: true,
	}

	result := Evaluate(input, defaultConfig())

	if result.Grade != "A" {
		t.Fatalf("expected grade A, got %s (overall: %.1f)", result.Grade, result.Overall)
	}
	if len(result.Dimensions) != 6 {
		t.Fatalf("expected 6 dimensions, got %d", len(result.Dimensions))
	}
	if result.Overall < 90 {
		t.Fatalf("expected overall >= 90, got %.1f", result.Overall)
	}
}

func TestEvaluate_PoorQuality(t *testing.T) {
	input := &EvaluateInput{
		GateHistory: GateHistory{
			TotalGates: 5,
			Passed:     3,
			Retries:    3,
		},
		StartedAt:        time.Now().Add(-120 * time.Minute),
		CompletedAt:      time.Now(),
		GitDiffStat:         "300\t300\tmain.go",
		TestCoveragePassed:  false,
		TestCoverageChecked: true,
		CompilePassed:    false,
		CompileChecked:   true,
		AssertionPassed:  false,
		AssertionChecked: true,
	}

	result := Evaluate(input, defaultConfig())

	if result.Grade != "F" && result.Grade != "D" {
		t.Fatalf("expected grade D or F, got %s (overall: %.1f)", result.Grade, result.Overall)
	}
}

func TestGradeFromScore(t *testing.T) {
	thresholds := scoringtypes.DefaultThresholds()

	tests := []struct {
		score    float64
		expected string
	}{
		{95, "A"},
		{90, "A"},
		{89.9, "B"},
		{80, "B"},
		{79.5, "C"},
		{70, "C"},
		{65, "D"},
		{60, "D"},
		{59, "F"},
		{0, "F"},
	}

	for _, tt := range tests {
		grade := scoringtypes.GradeFromScore(tt.score, thresholds)
		if grade != tt.expected {
			t.Errorf("GradeFromScore(%.1f) = %q, want %q", tt.score, grade, tt.expected)
		}
	}
}

