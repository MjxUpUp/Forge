package scoring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// goldenRealDir 是真实 dogfood 任务采集的 golden fixture 目录，与 canonical
// testdata/golden 正交：canonical 钉算法边界（人工 clean/poor），golden_real 钉
// 真实任务评分形状不漂移。LoadGoldenCases 复用，只是 dir 不同。
func goldenRealDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "golden_real"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// realCases 是从真实 dogfood 任务反推的 EvaluateInput，验证「真实协作记录→golden→CI
// 比对」链路。数据来自 DataDir/tasks/feat-review-snapshot.json 的 Score.Dimensions：
// process 100 (3/3, 0 retries) / testing 77 (2/3 covered, 167 assertions) /
// scope 80 (166 行 Medium) / efficiency 55 (61 min) / code-quality+assertions 100。
//
// GitDiffStat 用「100\t66\tstamp.go」模拟 166 行（真实 numstat 未在 TaskState 快照，
// 用同 totalLines 的等价输入）——spike 验证机制；生产采集器应在 task score 完成那刻
// 快照真实 git 状态（事后 HeadCommit 推进会让 diff 漂移）。
func realCases() []GoldenCase {
	start := time.Date(2026, 7, 1, 19, 41, 41, 0, time.FixedZone("CST", 8*3600))
	reviewSnapshot := EvaluateInput{
		GateHistory:           GateHistory{TotalGates: 3, Passed: 3, Retries: 0},
		StartedAt:             start,
		CompletedAt:           start.Add(61 * time.Minute),
		GitDiffStat:           "100\t66\tinternal/review/stamp.go",
		TestCoveragePassed:    true,
		TestCoverageChecked:   true,
		TestCoverageCovered:   2,
		TestCoverageTotal:     3,
		TestAssertionCount:    167,
		TestFileCount:         2,
		CompilePassed:         true,
		CompileChecked:        true,
		AssertionPassed:       true,
		AssertionChecked:      true,
		EvidenceDeterministic: 78,
		EvidenceAgentClaim:    2,
	}
	cfg := &scoringtypes.ScoringConfig{
		Weights:    scoringtypes.DefaultWeights(),
		Thresholds: scoringtypes.DefaultThresholds(),
	}
	gc := GoldenCaseFromInput(
		`review-snapshot`,
		`真实 dogfood 任务 feat/review-snapshot（commit 8e00456，Score 89B）的评分形状：3/3 门禁、2/3 源码覆盖+167 断言、166 行改动、61 分钟。钉真实组合不漂移——改 scoreScope/scoreTesting 等若让此真实任务评分漂移即 CI 挂。`,
		&reviewSnapshot,
		cfg,
	)
	// review-snapshot 是人工从真实 Score.Dimensions 反推的精确基线（GitDiffStat 用等价
	// totalLines 模拟，非事后采集），全维度可信——标 hand-curated，无 drift_known。
	gc.Meta = GoldenMeta{Source: `hand-curated`}
	return []GoldenCase{*gc}
}

// writeRealFixtures 从 realCases() 重新生成 testdata/golden_real/。仅在 fixture 缺失时
// 调用（bootstrap），CI 不静默覆盖已有 fixture。接受 intentional scoring change 的流程：
// 删 testdata/golden_real/*.json → 重跑测试让它重建 → review 新 Expected。
func writeRealFixtures(t *testing.T) {
	t.Helper()
	dir := goldenRealDir(t)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, c := range realCases() {
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "golden_real_"+c.Name+".json")
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestGoldenReal_FixturesPresent 确保真实 golden fixture 在盘。缺失则重建并 fail，
// 强制作者 review 新 Expected（复刻 canonical 的 FixturesPresent 语义）。
func TestGoldenReal_FixturesPresent(t *testing.T) {
	cases, err := LoadGoldenCases(goldenRealDir(t))
	if err != nil || len(cases) == 0 {
		writeRealFixtures(t)
		t.Fatal("golden_real fixtures were missing and have been regenerated — review testdata/golden_real/ expected values, then re-run")
	}
}

// TestGoldenReal_Regression 是真实 golden 的回归守卫：load 每个 fixture，重跑 Evaluate，
// 断言与记录的 Expected 精确一致（deterministic 函数，无容差）。drift 时报哪个维度/grade
// 漂移、幅度多少，并告知如何接受 intentional change（删 fixture 重跑）。
func TestGoldenReal_Regression(t *testing.T) {
	cases, err := LoadGoldenCases(goldenRealDir(t))
	if err != nil {
		t.Fatalf("LoadGoldenCases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no golden_real fixtures — run TestGoldenReal_FixturesPresent to bootstrap")
	}
	cfg := &scoringtypes.ScoringConfig{
		Weights:    scoringtypes.DefaultWeights(),
		Thresholds: scoringtypes.DefaultThresholds(),
	}
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			res := Evaluate(&c.Input, cfg)

			if res.Grade != c.Expected.Grade {
				t.Errorf("grade drift: got %s, want %s (overall %.1f vs expected %.1f)\n  if intentional: delete testdata/golden_real/*.json and re-run to regenerate",
					res.Grade, c.Expected.Grade, res.Overall, c.Expected.Overall)
			}
			if diff := res.Overall - c.Expected.Overall; diff < -0.05 || diff > 0.05 {
				t.Errorf("overall drift: got %.4f, want %.4f (Δ%.4f)\n  rationale: %s",
					res.Overall, c.Expected.Overall, diff, c.Rationale)
			}

			liveDims := make(map[string]int, len(res.Dimensions))
			for _, d := range res.Dimensions {
				liveDims[string(d.Dimension)] = d.Score
			}
			drift := make(map[string]bool, len(c.Meta.DriftKnown))
			for _, d := range c.Meta.DriftKnown {
				drift[d] = true
			}
			for dim, want := range c.Expected.Dimensions {
				if drift[dim] {
					t.Logf(`dimension %q in drift_known (source=%s) — advisory skip, not asserted`, dim, c.Meta.Source)
					continue
				}
				got, ok := liveDims[dim]
				if !ok {
					t.Errorf(`dimension %q missing from live result`, dim)
					continue
				}
				if got != want {
					t.Errorf(`dimension %q drift: got %d, want %d`, dim, got, want)
				}
			}
		})
	}
}
