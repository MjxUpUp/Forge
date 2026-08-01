package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/health"
)

// captureStdout reuses the definition in skills_install_test.go (same package).
// runForgeStreams reuses the definition in task_nongit_test.go (same package).
//
// captureStdout 复用 skills_install_test.go 的定义（同包）。
// runForgeStreams 复用 task_nongit_test.go 的定义（同包）。

func TestPrintHealth_Empty(t *testing.T) {
	out := captureStdout(t, func() { printHealth(health.Summary{}) })
	if !strings.Contains(out, "尚无完成任务结论") {
		t.Errorf("空数据应提示无结论，got: %s", out)
	}
}

func TestPrintHealth_BlindSpotWarning(t *testing.T) {
	// Blind-spot rate 2/3 ~= 0.67 >= 0.5 -> must print the systemic blind-spot warning (project-level headline signal).
	//
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
	// Blind-spot rate 0 -> should not show the systemic blind-spot warning (avoid noise).
	//
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

// TestHealth_NonGitFriendlyMessage pins dogfood 5.2: running forge health in a
// non-git directory must not surface a bare confusing error like "forgedata: cwd
// is not in a git repository" (AwesomeMutiAgent abandoned after 1 session).
// After the user-level-assets anchor contract, an unregistered project (no
// registry entry, no legacy .forge/) exits non-zero — but the message must stay
// actionable (points at `forge init`), not a raw internals dump.
//
// TestHealth_NonGitFriendlyMessage 钉死 dogfood 5.2：非 git 目录跑 forge health
// 不裸报 "forgedata: cwd is not in a git repository" 这类令人困惑的底层 error
// （AwesomeMutiAgent 1 session 放弃）。user-level-assets 锚点契约后，未登记项目
// （无注册表条目、无遗留 .forge/）退出非零——但消息必须可行动（指向
// `forge init`），而非内部细节裸奔。
func TestHealth_NonGitFriendlyMessage(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	tmpDir := t.TempDir()
	// No git, no .forge — the AwesomeMutiAgent scenario.
	//
	// 无 git、无 .forge —— AwesomeMutiAgent 场景
	_, stderr, code := runForgeStreams(t, tmpDir, "health")
	if code == 0 {
		t.Fatal("forge health 未登记项目应 exit 非零（锚点契约：无注册表条目且无 .forge/）")
	}
	// The message must be actionable — point the user at forge init.
	//
	// 消息必须可行动——指引用户 forge init
	if !strings.Contains(stderr, "forge init") {
		t.Errorf("非 git health stderr 应指引 forge init\nstderr: %s", stderr)
	}
	// Should not surface the underlying error.
	//
	// 不应裸露底层错误
	if strings.Contains(stderr, "forgedata: cwd is not in a git repository") {
		t.Errorf("不应裸报底层 error，got: %s", stderr)
	}
}
