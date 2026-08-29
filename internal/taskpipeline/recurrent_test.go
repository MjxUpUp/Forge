package taskpipeline

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
)

// mkConcl 构造一个带指定低分维度的最小完成任务结论，供纯函数复发测试（不碰磁盘）。
// Score/Grade/Strength 设为真实低分值。存量形态（无 DimScores）：复发逻辑回落 LowDimensions。
func mkConcl(ref string, lowDims ...string) act.Conclusion {
	return act.Conclusion{
		TaskRef:       ref,
		Score:         60,
		Grade:         "C",
		Strength:      "Strong",
		LowDimensions: lowDims,
		CompletedAt:   time.Now(),
	}
}

// mkConclDims 构造带每维度原始分（DimScores）的新式结论——噪声带改造后 BuildConclusion
// 落盘的形态。
func mkConclDims(ref string, dims ...act.DimScore) act.Conclusion {
	return act.Conclusion{
		TaskRef:     ref,
		Score:       60,
		Grade:       "C",
		Strength:    "Strong",
		DimScores:   dims,
		CompletedAt: time.Now(),
	}
}

func TestLowDimCounts(t *testing.T) {
	cs := []act.Conclusion{
		mkConcl("a", "testing"),
		mkConcl("b", "testing", "scope"),
		mkConcl("c", "scope"),
		mkConcl("d"), // 无低分维度
	}
	counts := lowDimCounts(cs)
	if counts["testing"] != 2 {
		t.Errorf(`testing 应计 2 次, got %d`, counts["testing"])
	}
	if counts["scope"] != 2 {
		t.Errorf(`scope 应计 2 次, got %d`, counts["scope"])
	}
	if counts["efficiency"] != 0 {
		t.Errorf(`efficiency 应计 0 次, got %d`, counts["efficiency"])
	}
}

func TestDimRecurrent(t *testing.T) {
	cs := []act.Conclusion{
		mkConcl("a", "testing"),
		mkConcl("b", "testing"),
		mkConcl("c", "testing"),
	}
	if !dimRecurrent(cs, dimTesting, 3) {
		t.Error(`3 次 testing 低分 >= 阈值 3 → 应复发`)
	}
	if dimRecurrent(cs, dimTesting, 4) {
		t.Error(`3 次 < 阈值 4 → 不应复发`)
	}
	if dimRecurrent(nil, dimTesting, 3) {
		t.Error(`空历史 → 不复发（fail-open）`)
	}
	if dimRecurrent(cs, dimTesting, 0) {
		t.Error(`阈值 <=0 → 不复发`)
	}
	if dimRecurrent(cs, dimScope, 3) {
		t.Error(`scope 0 次 → 不复发`)
	}
}

func TestLowDimCounts_NoiseBand(t *testing.T) {
	// AutoDesign margin 校准：切线附近 0-3 分差距 ≈ 抛硬币。在 [66,70) 抖动的维度不得向
	// 复发升硬累计——有 DimScores 时只计明确低（<=65）。
	flap := mkConclDims("a", act.DimScore{Dimension: dimTesting, Score: 67})
	flap2 := mkConclDims("b", act.DimScore{Dimension: dimTesting, Score: 69})
	flap3 := mkConclDims("c", act.DimScore{Dimension: dimTesting, Score: 66})
	cs := []act.Conclusion{flap, flap2, flap3}
	if got := lowDimCounts(cs)[dimTesting]; got != 0 {
		t.Errorf(`67/69/66 属边界抖动（>65）不应计数, got %d`, got)
	}
	if dimRecurrent(cs, dimTesting, 3) {
		t.Error(`3 次边界抖动 → 不应升硬（噪声带生效）`)
	}

	clear1 := mkConclDims("d", act.DimScore{Dimension: dimTesting, Score: 65})
	clear2 := mkConclDims("e", act.DimScore{Dimension: dimTesting, Score: 40}, act.DimScore{Dimension: dimScope, Score: 90})
	clear3 := mkConclDims("f", act.DimScore{Dimension: dimTesting, Score: 30}, act.DimScore{Dimension: dimScope, Score: 64})
	clear := []act.Conclusion{clear1, clear2, clear3}
	if got := lowDimCounts(clear)[dimTesting]; got != 3 {
		t.Errorf(`65/40/30 均明确低应计 3 次, got %d`, got)
	}
	if !dimRecurrent(clear, dimTesting, 3) {
		t.Error(`3 次明确低 → 应复发`)
	}
	if got := lowDimCounts(clear)[dimScope]; got != 1 {
		t.Errorf(`scope 90 不计、64 计 → 应 1 次, got %d`, got)
	}
}

func TestLowDimCounts_LegacyFallback(t *testing.T) {
	// 存量结论早于 DimScores——只有二值 <70 的 LowDimensions，其 67 也计入（数字只是丢了）。
	// 混合队列：每条结论按自身形态判定。
	cs := []act.Conclusion{
		mkConclDims("modern-67", act.DimScore{Dimension: dimTesting, Score: 67}), // 新式：不计
		mkConcl("legacy", "testing"), // 存量：计（无数字可辨）
		mkConcl("legacy2", "testing"),
		mkConcl("legacy3", "testing"),
	}
	if got := lowDimCounts(cs)[dimTesting]; got != 3 {
		t.Errorf(`新式 67 不计 + 存量 3 计 = 3, got %d`, got)
	}
	if !dimRecurrent(cs, dimTesting, 3) {
		t.Error(`存量 3 次 → 应复发（回落路径不丢信号）`)
	}
}

func TestRecurrentThreshold(t *testing.T) {
	if recurrentThreshold() != recurrentDimThresholdDefault {
		t.Errorf(`无 env 时应用默认阈值 %d, got %d`, recurrentDimThresholdDefault, recurrentThreshold())
	}
	t.Setenv(recurrentThresholdEnv, "5")
	if recurrentThreshold() != 5 {
		t.Errorf(`FORGE_RECURRENT_THRESHOLD=5 应覆盖为 5, got %d`, recurrentThreshold())
	}
	t.Setenv(recurrentThresholdEnv, "0") // 非正整数 → 回落默认
	if recurrentThreshold() != recurrentDimThresholdDefault {
		t.Errorf(`阈值 0 非法应回落默认, got %d`, recurrentThreshold())
	}
	t.Setenv(recurrentThresholdEnv, "abc") // 非数字 → 回落默认
	if recurrentThreshold() != recurrentDimThresholdDefault {
		t.Errorf(`非数字应回落默认, got %d`, recurrentThreshold())
	}
}

func TestRecurrentHardenEnabled(t *testing.T) {
	if !recurrentHardenEnabled() {
		t.Error(`默认应开启复发升硬（opt-out 哲学）`)
	}
	t.Setenv(recurrentHardenDisableEnv, "disable")
	if recurrentHardenEnabled() {
		t.Error(`FORGE_RECURRENT_HARDEN=disable 应关闭`)
	}
	t.Setenv(recurrentHardenDisableEnv, "anything-else")
	if !recurrentHardenEnabled() {
		t.Error(`非 "disable" 的值不应关闭`)
	}
}

func TestScopeDriftSevere(t *testing.T) {
	if scopeDriftSevere([]string{"a"}) {
		t.Error(`1 文件 drift 不严重（正常预测失误）`)
	}
	if scopeDriftSevere([]string{"a", "b"}) {
		t.Error(`2 文件 drift 不严重`)
	}
	if !scopeDriftSevere([]string{"a", "b", "c"}) {
		t.Error(`3 文件 drift 严重（实质偏离计划）`)
	}
}
