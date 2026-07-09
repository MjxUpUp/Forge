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

func TestScoreTesting_AllCovered(t *testing.T) {
	result := scoreTesting(1, 1, 3, true)
	if result.Score != 100 {
		t.Fatalf(`expected 100 (all source covered), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_PartialCoverage(t *testing.T) {
	// 4/5 源码文件有配对测试 → ratio 0.8 → 30+70*0.8 = 86（连续打分，非二值塌缩到 20）
	result := scoreTesting(4, 5, 5, true)
	if result.Score != 86 {
		t.Fatalf(`expected 86 (4/5 covered, continuous), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_NoneCovered(t *testing.T) {
	// 0/1 → ratio 0 → 30（低分但不极端塌缩；covered=0 不触发假测试惩罚）
	result := scoreTesting(0, 1, 0, true)
	if result.Score != 30 {
		t.Fatalf(`expected 30 (none covered), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_NotChecked(t *testing.T) {
	result := scoreTesting(0, 0, 0, false)
	if result.Score != 70 {
		t.Fatalf(`expected 70 (coverage not checked, neutral), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_NoSourceNeedsTest(t *testing.T) {
	// 无可测源码（空 diff / 全白名单）→ 100（无对象不该被惩罚）
	result := scoreTesting(0, 0, 5, true)
	if result.Score != 100 {
		t.Fatalf(`expected 100 (no source requiring tests), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_FakeTestPenalty(t *testing.T) {
	// 全配对但 0 断言 = 假测试（只有 setup/log 无断言）→ 100 * 0.6 = 60
	result := scoreTesting(1, 1, 0, true)
	if result.Score != 60 {
		t.Fatalf(`expected 60 (fake-test penalty: covered>0 but 0 assertions), got %d: %s`, result.Score, result.Detail)
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
	if result.Score != 55 {
		t.Fatalf("expected 55 (slow, 90min ≤120 bucket), got %d: %s", result.Score, result.Detail)
	}
}

func TestEvaluate_Full(t *testing.T) {
	input := &EvaluateInput{
		GateHistory: GateHistory{
			TotalGates: 5,
			Passed:     5,
			Retries:    0,
		},
		StartedAt:           time.Now().Add(-10 * time.Minute),
		CompletedAt:         time.Now(),
		GitDiffStat:         "5\t5\tmain.go",
		TestCoveragePassed:  true,
		TestCoverageChecked: true,
		TestCoverageCovered: 1,
		TestCoverageTotal:   1,
		TestAssertionCount:  3,
		TestFileCount:       1,
		CompilePassed:       true,
		CompileChecked:      true,
		AssertionPassed:     true,
		AssertionChecked:    true,
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
		StartedAt:           time.Now().Add(-120 * time.Minute),
		CompletedAt:         time.Now(),
		GitDiffStat:         "300\t300\tmain.go",
		TestCoveragePassed:  false,
		TestCoverageChecked: true,
		TestCoverageCovered: 0,
		TestCoverageTotal:   1,
		TestAssertionCount:  0,
		TestFileCount:       1,
		CompilePassed:       false,
		CompileChecked:      true,
		AssertionPassed:     false,
		AssertionChecked:    true,
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

// TestBuildEvidenceSummary 锁定证据摘要纯函数：total=0 返回 nil（无证据数据，
// 如旧任务 checklog 为空），避免零值噪声；有数据时按 deterministic/total 算 ratio。
// ratio case 选 0/1/0.5（浮点精确，免容差比较）。
func TestBuildEvidenceSummary(t *testing.T) {
	cases := []struct {
		name       string
		det, claim int
		wantNil    bool
		wantRatio  float64
	}{
		{`empty returns nil`, 0, 0, true, 0},
		{`all deterministic`, 5, 0, false, 1.0},
		{`all agent-claim`, 0, 3, false, 0.0},
		{`mixed half`, 1, 1, false, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildEvidenceSummary(c.det, c.claim)
			if c.wantNil {
				if got != nil {
					t.Fatalf(`buildEvidenceSummary(%d,%d) = %+v, want nil`, c.det, c.claim, got)
				}
				return
			}
			if got == nil {
				t.Fatalf(`buildEvidenceSummary(%d,%d) = nil, want non-nil`, c.det, c.claim)
			}
			if got.Total != c.det+c.claim {
				t.Fatalf(`total: got %d, want %d`, got.Total, c.det+c.claim)
			}
			if got.Ratio != c.wantRatio {
				t.Fatalf(`ratio: got %v, want %v`, got.Ratio, c.wantRatio)
			}
		})
	}
}

// TestEvaluate_EvidenceSummary 端到端：Evaluate 把 input 的证据计数注入
// ScoreResult.Evidence。无证据输入 → nil（不输出零值）。
func TestEvaluate_EvidenceSummary(t *testing.T) {
	t.Run(`nil when no evidence input`, func(t *testing.T) {
		input := &EvaluateInput{GateHistory: GateHistory{TotalGates: 3, Passed: 3}}
		result := Evaluate(input, defaultConfig())
		if result.Evidence != nil {
			t.Fatalf(`expected nil Evidence when no evidence input, got %+v`, result.Evidence)
		}
	})
	t.Run(`populated from input counts`, func(t *testing.T) {
		input := &EvaluateInput{
			GateHistory:           GateHistory{TotalGates: 3, Passed: 3},
			EvidenceDeterministic: 4,
			EvidenceAgentClaim:    1,
		}
		result := Evaluate(input, defaultConfig())
		if result.Evidence == nil {
			t.Fatal(`expected non-nil Evidence`)
		}
		if result.Evidence.Deterministic != 4 || result.Evidence.AgentClaim != 1 || result.Evidence.Total != 5 {
			t.Fatalf(`evidence buckets: got det=%d claim=%d total=%d, want 4/1/5`,
				result.Evidence.Deterministic, result.Evidence.AgentClaim, result.Evidence.Total)
		}
	})
}
