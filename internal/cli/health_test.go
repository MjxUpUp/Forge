package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/health"
)

// captureStdout reuses the definition in skills_install_test.go (same package). runForgeStreams reuses the definition in task_nongit_test.go (same package).
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
	// 盲区率 2/3 ≈ 0.67 ≥ 0.5 → 必须打印系统性盲区告警（项目级头条信号）。
	s := health.Summary{
		TotalTasks:     3,
		BlindSpotCount: 2,
		BlindSpotRate:  0.67,
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
		BlindSpotCount: 0,
		BlindSpotRate:  0,
		StrengthDist:   map[string]int{"Strong": 2},
	}
	out := captureStdout(t, func() { printHealth(s) })
	if strings.Contains(out, "系统性盲区") {
		t.Errorf("盲区率 0 不该告警，got: %s", out)
	}
}

// TestPrintHealth_FactsOnly pins the 2.3 rewrite: forge health prints facts (criteria,
// red-first, review rounds, escapes) and no composite score/grade/trend/low-dim histogram —
// a composite hides the "looks good but zero deterministic evidence" blind spot.
//
// TestPrintHealth_FactsOnly 钉住 2.3 改写：forge health 只打事实（判据/先失败证明/评审/
// 逃生舱），不打均分/档/趋势/低分维度直方图——合成分会掩盖"看起来好但零实跑证据"的盲区。
func TestPrintHealth_FactsOnly(t *testing.T) {
	s := health.Summary{
		TotalTasks:      2,
		StrengthDist:    map[string]int{"Strong": 2},
		Criteria:        7,
		CriteriaPassed:  6,
		RedFirstTrials:  3,
		RedFirstClosed:  2,
		ReviewRounds:    4,
		Escapes:         1,
		NudgeCount:      1,
		BlindSpotRate:   0,
		CappedWeakCount: 1,
	}
	out := captureStdout(t, func() { printHealth(s) })
	for _, want := range []string{`判据:   通过 6/7`, `先失败证明: 2/3 已转绿`, `评审: 4 轮  逃生: 1 次`, `强度: Strong=2`, `回顾触发: 1/2`} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少事实行 %q, got: %s", want, out)
		}
	}
	// 合成分的措辞不得回归（均分/档分布/低分维度/趋势）。
	for _, banned := range []string{`均分`, `中位`, `档分布`, `低分维度`, `趋势`} {
		if strings.Contains(out, banned) {
			t.Errorf("不应再输出合成分 %q, got: %s", banned, out)
		}
	}
}

// TestHealth_NonGitFriendlyMessage pins dogfood 5.2.
//
// TestHealth_NonGitFriendlyMessage 钉死 dogfood 5.2：非 git 目录跑 forge health
// 不裸报 "forgedata: cwd is not in a git repository" 这类令人困惑的底层 error
// （AwesomeMutiAgent 1 session 放弃）。user-level-assets 锚点契约后，未登记项目
// （无注册表条目、无遗留 .forge/）退出非零——但消息必须可行动（指向
// `forge init`），而非内部细节裸奔。
func TestHealth_NonGitFriendlyMessage(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	// 需要空注册表——理由同 TestStatusWithoutInit。
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	// 无 git、无 .forge —— AwesomeMutiAgent 场景
	_, stderr, code := runForgeStreams(t, tmpDir, "health")
	if code == 0 {
		t.Fatal("forge health 未登记项目应 exit 非零（锚点契约：无注册表条目且无 .forge/）")
	}
	// 消息必须可行动——指引用户 forge init
	if !strings.Contains(stderr, "forge init") {
		t.Errorf("非 git health stderr 应指引 forge init\nstderr: %s", stderr)
	}
	// 不应裸露底层错误
	if strings.Contains(stderr, "forgedata: cwd is not in a git repository") {
		t.Errorf("不应裸报底层 error，got: %s", stderr)
	}
}

// TestHealthJSON_NudgeWindowed pins that `forge health --json` windows the
// alert-facing nudge counts at the real now (the pulse panel's 14-day window,
// without its ack filter), while nudge_count stays the all-history truth.
//
// TestHealthJSON_NudgeWindowed 钉住 `forge health --json` 以真实 now 开窗：
// nudge_recent / nudge_actionable 只数 14 天窗口内的 nudge（与 pulse 面板同一窗口，
// 不扣 ack），nudge_count 仍是全量。
func TestHealthJSON_NudgeWindowed(t *testing.T) {
	tmpDir, p := forgedatatest.RealProject(t)
	if out, _, code := runForge(t, tmpDir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`init: %s`, out)
	}
	now := time.Now()
	for _, c := range []act.Conclusion{
		{TaskRef: `feat/recent`, Strength: `Unverified`, Ratio: 0.4, RetrospectiveNudge: true, CompletedAt: now.Add(-3 * 24 * time.Hour)},
		{TaskRef: `feat/stale`, Strength: `Unverified`, Ratio: 0.4, RetrospectiveNudge: true, CompletedAt: now.Add(-30 * 24 * time.Hour)},
	} {
		if err := act.Append(p, &c); err != nil {
			t.Fatalf(`seed conclusion: %v`, err)
		}
	}
	out, _, code := runForge(t, tmpDir, `health`, `--json`)
	if code != 0 {
		t.Fatalf(`forge health --json exit %d: %s`, code, out)
	}
	var s health.Summary
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("health JSON 解析失败: %v\n%s", err, out)
	}
	if s.NudgeCount != 2 || s.NudgeRecent != 1 || s.NudgeActionable != 1 {
		t.Errorf(`nudge_count=%d want 2、nudge_recent=%d want 1、nudge_actionable=%d want 1（30 天前的 nudge 只计全量）`,
			s.NudgeCount, s.NudgeRecent, s.NudgeActionable)
	}
	// 事实面同样走 JSON：判据/先失败证明/评审/逃生舱必须可被脚本消费。
	if s.Criteria != 0 || s.CriteriaPassed != 0 {
		t.Errorf(`零判据结论应得 0 事实，got criteria=%d passed=%d`, s.Criteria, s.CriteriaPassed)
	}
}
