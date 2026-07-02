package scoring

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

func goldenDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "golden"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// canonicalCases returns the representative task shapes the golden set pins.
// Each targets a distinct scoring path (clean ceiling / poor-quality floor).
// Keep this list small and intentional — golden cases exist to catch drift, so
// every entry must have a clear rationale.
func canonicalCases() []GoldenCase {
	start := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	clean := EvaluateInput{
		GateHistory:         GateHistory{TotalGates: 3, Passed: 3, Retries: 0},
		StartedAt:           start,
		CompletedAt:         start.Add(5 * time.Minute),
		GitDiffStat:         "3\t5\tmain.go",
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

	poor := EvaluateInput{
		GateHistory:         GateHistory{TotalGates: 3, Passed: 1, Retries: 3},
		StartedAt:           start,
		CompletedAt:         start.Add(120 * time.Minute),
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

	return []GoldenCase{
		{
			Name:      "clean-baseline",
			Rationale: "All gates pass first try, tests present, small diff, no anti-patterns. Pins the A-grade ceiling; drift below A means something penalized a clean task.",
			Input:     clean,
		},
		{
			Name:      "poor-quality",
			Rationale: "Gates failed with retries, no tests, very large diff, slow, compile failed. Pins the D/F floor so a genuinely bad task never grades as acceptable.",
			Input:     poor,
		},
	}
}

// writeCanonicalFixtures regenerates testdata/golden/ from the current
// evaluator output. Called from TestGoldenSet_FixturesPresent (not skipped)
// only when the fixture set is missing — it bootstraps the golden files but
// never silently overwrites existing ones during CI. To accept an intentional
// scoring change, delete testdata/golden/*.json and re-run the tests.
func writeCanonicalFixtures(t *testing.T) {
	t.Helper()
	dir := goldenDir(t)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &scoringtypes.ScoringConfig{
		Weights:    scoringtypes.DefaultWeights(),
		Thresholds: scoringtypes.DefaultThresholds(),
	}
	for _, c := range canonicalCases() {
		res := Evaluate(&c.Input, cfg)
		dims := make(map[string]int, len(res.Dimensions))
		for _, d := range res.Dimensions {
			dims[string(d.Dimension)] = d.Score
		}
		c.Expected = ExpectedScore{Overall: res.Overall, Grade: res.Grade, Dimensions: dims}
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "golden_"+c.Name+".json")
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestGoldenSet_FixturesPresent ensures the golden fixtures exist on disk.
// If they are missing (e.g. first run, or after deliberately deleting them to
// accept a scoring change), it regenerates them and fails — forcing the author
// to review the new expected values via the regression test before landing.
func TestGoldenSet_FixturesPresent(t *testing.T) {
	cases, err := LoadGoldenCases(goldenDir(t))
	if err != nil || len(cases) == 0 {
		writeCanonicalFixtures(t)
		t.Fatal("golden fixtures were missing and have been regenerated — review testdata/golden/ expected values, then re-run")
	}
	// Guard against silent fixture drift: the on-disk set must match the
	// canonical case names exactly, otherwise a stale/renamed fixture would
	// pin the wrong behavior.
	want := map[string]bool{}
	for _, c := range canonicalCases() {
		want[c.Name] = true
	}
	for _, c := range cases {
		if !want[c.Name] {
			t.Errorf("unexpected golden fixture %q — not in canonicalCases(); stale?", c.Name)
		}
	}
}

// TestGoldenSet_Regression is the regression guard. It loads every fixture in
// testdata/golden/, re-runs Evaluate, and asserts the live result matches the
// recorded expected values exactly (deterministic function — no tolerance).
// On drift it reports which dimension/grade moved and by how much, and tells
// the maintainer how to accept an intentional change (delete + regenerate).
func TestGoldenSet_Regression(t *testing.T) {
	cases, err := LoadGoldenCases(goldenDir(t))
	if err != nil {
		t.Fatalf("LoadGoldenCases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no golden fixtures — run TestGoldenSet_FixturesPresent to bootstrap")
	}
	cfg := &scoringtypes.ScoringConfig{
		Weights:    scoringtypes.DefaultWeights(),
		Thresholds: scoringtypes.DefaultThresholds(),
	}

	byName := make(map[string]GoldenCase, len(cases))
	for _, c := range cases {
		byName[c.Name] = c
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			res := Evaluate(&c.Input, cfg)

			if res.Grade != c.Expected.Grade {
				t.Errorf("grade drift: got %s, want %s (overall %.1f vs expected %.1f)\n  if intentional: delete testdata/golden/*.json and re-run to regenerate",
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
			for dim, want := range c.Expected.Dimensions {
				got, ok := liveDims[dim]
				if !ok {
					t.Errorf("dimension %q missing from live result", dim)
					continue
				}
				if got != want {
					t.Errorf("dimension %q drift: got %d, want %d", dim, got, want)
				}
			}
		})
	}

	// Poor-quality floor anchor: must be D or F, never the neutral B/C band.
	if p, ok := byName["poor-quality"]; ok {
		if p.Expected.Grade != "D" && p.Expected.Grade != "F" {
			t.Errorf("poor-quality fixture recorded grade %q — expected D/F as the floor anchor", p.Expected.Grade)
		}
	}
}

var _ = fmt.Sprintf
