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
	Rationale string       `json:"rationale"`
	Input    EvaluateInput `json:"input"`
	Expected ExpectedScore `json:"expected"`
	// Meta 记录采集来源与已知漂移维度。omitempty → 老 canonical fixture 无 Meta，向后兼容。
	Meta GoldenMeta `json:"meta,omitempty"`
}

// GoldenMeta 记录 fixture 的采集来源与已知漂移维度，供 golden test 区分对待。
//
// 业界弱先例：insta 的 .snap front-matter（source=断言位置）、EleutherAI 的
// metadata.version（版本门控）。主流是"消除漂移源"（mock 时钟/normalizer/redaction），
// 但我们的场景 git HEAD 天然漂移（任务完成后必然推进），用 drift_known 显式标注比靠
// mock 消除更务实——采集的是已固化快照，漂移在采集那刻已发生，无法事后 mock 掉。
type GoldenMeta struct {
	// Source：hand-curated（人工反推精确基线，全维度可信）/ auto-collected
	//（forge verify --collect-golden 从 TaskState 采集，可能含漂移）。
	Source string `json:"source,omitempty"`
	// DriftKnown 列出已知因采集时刻状态漂移而不可靠的维度名（当前仅 scope——GitDiffStat
	// 含事后 HEAD 推进的改动）。golden test 对这些维度 advisory 不 fail：固化后稳定，但
	// 数值不反映任务真实 diff。留作回归基线仍有价值——scope 之外的维度照常断言。
	DriftKnown []string `json:"drift_known,omitempty"`
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
