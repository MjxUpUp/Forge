package taskpipeline

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
)

// mkConcl builds a minimal completed-task conclusion with the given low-score dimensions, for the
// pure-function recurrence tests (no disk). Score/Grade/Strength are set to realistic low-score
// values, but recurrence logic only reads LowDimensions.
//
// mkConcl 构造一个带指定低分维度的最小完成任务结论，供纯函数复发测试（不碰磁盘）。
// Score/Grade/Strength 设为真实低分值，但复发逻辑只读 LowDimensions。
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
