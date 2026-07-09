package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/health"
)

// captureStdout 复用 skills_install_test.go 的定义（同包）。
// runForgeStreams 复用 task_nongit_test.go 的定义（同包）。

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

// TestHealth_NonGitFriendlyMessage 钉死 dogfood 5.2：非 git 目录跑 forge health 不再裸报
// "forgedata: cwd is not in a git repository"（AwesomeMutiAgent 1 session 放弃），改友好提示
// 指引 git init/forge init。退出码 0（用户错误而非程序错误）。
func TestHealth_NonGitFriendlyMessage(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	tmpDir := t.TempDir()
	// 无 git、无 .forge —— AwesomeMutiAgent 场景
	stdout, _, code := runForgeStreams(t, tmpDir, "health")
	if code != 0 {
		t.Fatalf("forge health 非 git 目录应 exit 0（友好提示，非程序错误），got %d", code)
	}
	for _, want := range []string{"需在 git 项目内运行", "不是 git 仓库", "git init"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("非 git health stdout 缺 %q\nstdout: %s", want, stdout)
		}
	}
	// 不应裸露底层错误
	if strings.Contains(stdout, "forgedata: cwd is not in a git repository") {
		t.Errorf("不应裸报底层 error，got: %s", stdout)
	}
}
