package cli

import (
	"strings"
	"testing"

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

// TestPrintHealth_LowDimsHistogram pins the 2026-09-22 low-dim rendering upgrade:
// each recurrent low dim prints its full-bucket histogram and a pattern verdict —
// spread (any ≥80 sample) reads as a metric/granularity signal, clustered-low as a
// discipline gap. Bare ×N misled the retrospective into "deposit an iron rule" for
// what was a threshold/granularity artifact (scope×57: 46/57 still grade A).
//
// TestPrintHealth_LowDimsHistogram 钉 2026-09-22 低分维度渲染升级：每个复发低分
// 维度输出全档直方图 + 形态判读——跨全档（含 ≥80）读作量纲/粒度信号，集中低档
// 读作纪律缺口。纯 ×N 曾把量纲伪象（scope×57：46/57 仍 A 级）误导成"该沉淀铁律"
// （见 docs/plans/low-dim-recurrence-2026-09.md）。
func TestPrintHealth_LowDimsHistogram(t *testing.T) {
	s := health.Summary{
		TotalTasks: 3,
		AvgScore:   88,
		LowDims: []health.DimFreq{
			{Dimension: "scope", Count: 2, Scores: map[int]int{40: 1, 100: 1}},
			{Dimension: "testing", Count: 1, Scores: map[int]int{0: 1}},
		},
	}
	out := captureStdout(t, func() { printHealth(s) })
	if !strings.Contains(out, `（40×1 100×1）`) {
		t.Errorf("scope 行应带升序直方图, got: %s", out)
	}
	if !strings.Contains(out, `跨全档＝量纲/任务粒度信号`) {
		t.Errorf("跨全档维度应判量纲/粒度信号, got: %s", out)
	}
	// 整行锚（防被恒打印的尾行"集中低档才是纪律缺口"掩蔽——分类器坏了此断言仍红）。
	if !strings.Contains(out, `×1（0×1）——集中低档＝纪律缺口`) {
		t.Errorf("testing 维度应整行判集中低档/纪律缺口, got: %s", out)
	}
}

// TestScoreHisto pins the histogram formatter: ascending score order (low buckets
// leftmost), "（40×31 60×26）" shape, empty map → empty string (legacy conclusions
// without DimScores fall back to bare ×N).
//
// TestScoreHisto 钉直方图格式化：分数升序（低档在左）、"（40×31 60×26）" 形态、
// 空直方图 → 空串（无 DimScores 的存量结论退回纯 ×N）。
func TestScoreHisto(t *testing.T) {
	cases := []struct {
		name   string
		scores map[int]int
		want   string
	}{
		{"跨全档", map[int]int{100: 31, 60: 26, 80: 25, 40: 31}, `（40×31 60×26 80×25 100×31）`},
		{"集中低档", map[int]int{40: 9}, `（40×9）`},
		{"空直方图", nil, ``},
	}
	for _, c := range cases {
		if got := scoreHisto(c.scores); got != c.want {
			t.Errorf(`%s: scoreHisto()=%q want %q`, c.name, got, c.want)
		}
	}
}
