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

// TestScoreProcess_PartialPassRate pins the pass-rate fix: Passed must participate in scoring —
// 1/5 gates passed with 0 retries is 20 (100*1/5), not the old free 100 that ignored Passed.
//
// TestScoreProcess_PartialPassRate 钉死通过率修复：Passed 必须参与计分——
// 1/5 通过 0 retry 得 20（100*1/5），不是旧版无视 Passed 白给的 100。
func TestScoreProcess_PartialPassRate(t *testing.T) {
	result := scoreProcess(GateHistory{TotalGates: 5, Passed: 1, Retries: 0})
	if result.Score != 20 {
		t.Fatalf("expected 20 (100*1/5 pass rate), got %d: %s", result.Score, result.Detail)
	}
}

// TestScoreProcess_PartialPassWithRetries: 3/5 pass rate (60) minus 2 retries (30) = 30.
//
// TestScoreProcess_PartialPassWithRetries：3/5 通过率（60）减 2 次 retry（30）= 30。
func TestScoreProcess_PartialPassWithRetries(t *testing.T) {
	result := scoreProcess(GateHistory{TotalGates: 5, Passed: 3, Retries: 2})
	if result.Score != 30 {
		t.Fatalf("expected 30 (60 pass rate - 30 retry penalty), got %d: %s", result.Score, result.Detail)
	}
}

// TestScoreProcess_FloorClamped: heavy retries on a low pass rate clamp at the 20 floor.
//
// TestScoreProcess_FloorClamped：低通过率叠重 retry 时钳在 20 下限。
func TestScoreProcess_FloorClamped(t *testing.T) {
	result := scoreProcess(GateHistory{TotalGates: 3, Passed: 1, Retries: 3})
	if result.Score != 20 {
		t.Fatalf("expected 20 (floor), got %d: %s", result.Score, result.Detail)
	}
}

func TestScoreTesting_AllCovered(t *testing.T) {
	result := scoreTesting(1, 1, 3, true)
	if result.Score != 100 {
		t.Fatalf(`expected 100 (all source covered), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_PartialCoverage(t *testing.T) {
	// 4/5 source files have paired tests → ratio 0.8 → 30+70*0.8 = 86 (continuous scoring, not binary collapse to 20)
	//
	// 4/5 源码文件有配对测试 → ratio 0.8 → 30+70*0.8 = 86（连续打分，非二值塌缩到 20）
	result := scoreTesting(4, 5, 5, true)
	if result.Score != 86 {
		t.Fatalf(`expected 86 (4/5 covered, continuous), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_NoneCovered(t *testing.T) {
	// 0/1 → ratio 0 → 30 (low score but not extreme collapse; covered=0 does not trigger fake-test penalty)
	//
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
	// No testable source (empty diff / all whitelisted) → 100 (no target should not be penalized)
	//
	// 无可测源码（空 diff / 全白名单）→ 100（无对象不该被惩罚）
	result := scoreTesting(0, 0, 5, true)
	if result.Score != 100 {
		t.Fatalf(`expected 100 (no source requiring tests), got %d: %s`, result.Score, result.Detail)
	}
}

func TestScoreTesting_FakeTestPenalty(t *testing.T) {
	// All paired but 0 assertions = fake test (only setup/log no assertions) → 100 * 0.6 = 60
	//
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

// TestScoreEfficiency_NegativeDuration pins the刷分向量 fix: completedAt before startedAt
// (clock skew / tampered TaskState) must NOT hit the <=15min bucket for a free 100 — it is
// untrustworthy data and scores neutral 70, same as missing timestamps.
//
// TestScoreEfficiency_NegativeDuration 钉死刷分向量修复：completedAt 早于 startedAt
// （时钟回拨 / TaskState 被改）不得命中 ≤15min 桶白拿 100——数据不可信，按缺失同待遇给中性 70。
func TestScoreEfficiency_NegativeDuration(t *testing.T) {
	start := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(-5 * time.Minute) // completed "before" it started
	result := scoreEfficiency(start, end)
	if result.Score != 70 {
		t.Fatalf("expected 70 (neutral, negative duration), got %d: %s", result.Score, result.Detail)
	}
}

// TestScoreEfficiency_Buckets pins F3: after threshold recalibration 5 tiers full coverage + boundary pinned (dogfood 1.5 core).
// Uses fixed time (not time.Now) to avoid flaky <=120 boundary due to nanosecond gap between two Now calls.
//
// TestScoreEfficiency_Buckets 钉死 F3：阈值重校准后 5 档全覆盖 + 边界 pinned（dogfood 1.5 核心）。
// 用固定时间（非 time.Now）避免 <=120 边界因两次 Now 调用的纳秒差 flaky。
func TestScoreEfficiency_Buckets(t *testing.T) {
	cases := []struct {
		name string
		mins int
		want int
	}{
		{`<=15 fast`, 15, 100},
		{`<=30 agile`, 30, 90},
		{`<=60 normal`, 60, 75},
		{`<=120 slow`, 120, 55},
		{`>120 default`, 150, 35},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			start := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
			end := start.Add(time.Duration(c.mins) * time.Minute)
			result := scoreEfficiency(start, end)
			if result.Score != c.want {
				t.Fatalf("%s (%dmin): got %d, want %d: %s", c.name, c.mins, result.Score, c.want, result.Detail)
			}
		})
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
	if len(result.Dimensions) != 7 {
		t.Fatalf("expected 7 dimensions, got %d", len(result.Dimensions))
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

// TestBuildEvidenceSummary locks the evidence summary pure function: total=0 returns nil (no evidence data,
// e.g. old task empty checklog), avoiding zero-value noise; with data computes ratio by deterministic/total.
// ratio case picks 0/1/0.5 (exact float, no tolerance comparison).
//
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

// TestEvaluate_EvidenceSummary end-to-end: Evaluate injects input's evidence counts into
// ScoreResult.Evidence. No evidence input → nil (no zero-value output).
//
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

// TestScoreExpression covers the expression (doc-artifact readability) dimension:
// neutral when no doc deliverables, lint+rubric blend when present, escape cap,
// and the missing-review floor.
//
// TestScoreExpression 覆盖表达（文档产物可读性）维度：无文档产物时中性、
// 有产物时 lint+rubric 混合、逃生封顶、未回检地板。
func TestScoreExpression(t *testing.T) {
	cfg := &scoringtypes.ScoringConfig{Weights: scoringtypes.DefaultWeights(), Thresholds: scoringtypes.DefaultThresholds()}
	find := func(in *EvaluateInput) scoringtypes.DimensionScore {
		for _, d := range Evaluate(in, cfg).Dimensions {
			if d.Dimension == scoringtypes.DimensionExpression {
				return d
			}
		}
		t.Fatal("expression dimension missing from Evaluate")
		return scoringtypes.DimensionScore{}
	}

	if got := find(&EvaluateInput{}); got.Score != 100 {
		t.Errorf("no doc deliverables → neutral 100, got %d (%s)", got.Score, got.Detail)
	}

	score80 := 80
	cases := []struct {
		name string
		in   EvaluateInput
		want int
	}{
		{"clean lint + rubric 80", EvaluateInput{HasDocDeliverables: true, DocRubricScore: &score80}, 90},
		{"missing review", EvaluateInput{HasDocDeliverables: true}, 50},
		{"3 hard issues no review", EvaluateInput{HasDocDeliverables: true, DocLintHardIssues: 3}, 27},
		{"escaped", EvaluateInput{HasDocDeliverables: true, DocGateEscaped: true}, 60},
	}
	for _, c := range cases {
		in := c.in
		if got := find(&in); got.Score != c.want {
			t.Errorf("%s: want %d, got %d (%s)", c.name, c.want, got.Score, got.Detail)
		}
	}

	// Escape cap: even a clean lint + high rubric cannot exceed 60 when escaped.
	//
	// 逃生封顶：即便 lint 全净 + rubric 高分，逃生后也不超过 60。
	in := EvaluateInput{HasDocDeliverables: true, DocRubricScore: &score80, DocGateEscaped: true}
	if got := find(&in); got.Score > 60 {
		t.Errorf("escape cap 60 violated: got %d", got.Score)
	}
}
