package scoring

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// GoldenCase is a single regression fixture for the scoring evaluator: a
// representative EvaluateInput paired with the score the evaluator produced
// when the fixture was recorded. The golden test re-runs Evaluate and asserts
// the result matches — so any unintended drift in the scoring functions shows
// up as a test failure rather than a silent quality change.
//
// Unlike evaluator_test.go (which constructs minimal inputs to probe one
// scoreXxx function's boundaries), golden cases carry realistic, full EvaluateInput
// shapes — the anti-pattern/tool-call/diff combinations that actually occur in
// real tasks. This is the layer that catches "I tweaked scoreScope and
// accidentally dropped every B-grade task to a C."
type GoldenCase struct {
	Name string `json:"name"`
	// Rationale documents why this case exists and which dimension/score
	// path it pins, so a future maintainer changing scoring knows whether the
	// expected value must move with the change.
	Rationale string        `json:"rationale"`
	Input    EvaluateInput  `json:"input"`
	Expected ExpectedScore  `json:"expected"`
}

// ExpectedScore is the subset of ScoreResult that golden cases pin. We pin
// per-dimension scores (not just overall) because a weighted-average can mask
// a regression in one dimension behind compensating movement in another.
type ExpectedScore struct {
	Overall    float64        `json:"overall"`
	Grade      string         `json:"grade"`
	Dimensions map[string]int `json:"dimensions"` // dimension name -> score
}

// LoadGoldenCases reads every *.json fixture under testdata/golden. Returning
// a slice (not a map) preserves deterministic test ordering by filename via
// filepath.Glob's sort.
func LoadGoldenCases(dir string) ([]GoldenCase, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "golden_*.json"))
	if err != nil {
		return nil, err
	}
	var cases []GoldenCase
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var c GoldenCase
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, &os.PathError{Op: "parse", Path: path, Err: err}
		}
		cases = append(cases, c)
	}
	return cases, nil
}
