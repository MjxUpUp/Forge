package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/health"
)

// captureStdout 复用 skills_install_test.go 的定义（同包）。

func TestPrintHealth_Empty(t *testing.T) {
	out := captureStdout(t, func() { printHealth(health.Summary{}) })
	if !strings.Contains(out, "尚无完成任务结论") {
		t.Errorf("空数据应提示无结论，got: %s", out)
	}
}

func TestPrintHealth_BlindSpotWarning(t *testing.T) {
	// 盲区率 2/3 ≈ 0.67 ≥ 0.5 → 必须打印系统性盲区告警（项目级头条信号）。
	s := health.Summary{
		TotalTasks:     3,
		AvgScore:       80,
		BlindSpotCount: 2,
		BlindSpotRate:  0.67,
		GradeDist:      map[string]int{"A": 1, "D": 2},
		StrengthDist:   map[string]int{"Strong": 1, "Unverified": 2},
	}
	out := captureStdout(t, func() { printHealth(s) })
	if !strings.Contains(out, "系统性盲区") {
		t.Errorf("盲区率≥50%% 应告警系统性盲区，got: %s", out)
	}
	if !strings.Contains(out, "67%") {
		t.Errorf("应显示盲区率百分比，got: %s", out)
	}
}

func TestPrintHealth_NoBlindSpotSilent(t *testing.T) {
	// 盲区率 0 → 不该出现系统性盲区告警（避免噪声）。
	s := health.Summary{
		TotalTasks:     2,
		AvgScore:       95,
		BlindSpotCount: 0,
		BlindSpotRate:  0,
		StrengthDist:   map[string]int{"Strong": 2},
	}
	out := captureStdout(t, func() { printHealth(s) })
	if strings.Contains(out, "系统性盲区") {
		t.Errorf("盲区率 0 不该告警，got: %s", out)
	}
}
