package scoring

// GoldenCase is a single regression fixture for the scoring evaluator: a representative
// EvaluateInput paired with the score the evaluator produced when the fixture was recorded.
//
// GoldenCase 是 scoring evaluator 的单个回归 fixture：一个有代表性的 EvaluateInput
// 配上 fixture 记录时 evaluator 产出的分数。golden test 重跑 Evaluate 并断言结果
// 匹配——故任何打分函数的非预期漂移都体现为测试失败而非静默质量变化。
//
// 区别于 evaluator_test.go（构造最小输入探单个 scoreXxx 函数边界），golden case 承载
// 真实任务中出现的 realistic、完整 EvaluateInput 形状——anti-pattern/tool-call/diff 的
// 实际组合。这一层抓的是「我调了 scoreScope 不小心把所有 B 级任务降到 C」这类回归。
type GoldenCase struct {
	Name string `json:"name"`
	// Rationale records why this case exists and which dimension/score path it pins,
	// so future maintainers changing scoring logic know whether the expected value
	// should move with the change.
	//
	// Rationale 记录本 case 为何存在、钉住哪个 dimension/score 路径，让未来维护者
	// 改打分逻辑时知道期望值是否要随变更移动。
	Rationale string        `json:"rationale"`
	Input     EvaluateInput `json:"input"`
	Expected  ExpectedScore `json:"expected"`
	// Meta records the collection source and known drift dimensions. omitempty →
	// older canonical fixtures without Meta remain backward compatible.
	//
	// Meta 记录采集来源与已知漂移维度。omitempty → 老 canonical fixture 无 Meta，向后兼容。
	Meta GoldenMeta `json:"meta,omitempty"`
}

// GoldenMeta records the fixture's collection source and known drift dimensions
// so the golden test can treat them differently.
//
// GoldenMeta 记录 fixture 的采集来源与已知漂移维度，供 golden test 区分对待。
//
// 业界弱先例：insta 的 .snap front-matter（source=断言位置）、EleutherAI 的
// metadata.version（版本门控）。主流是"消除漂移源"（mock 时钟/normalizer/redaction），
// 但我们的场景 git HEAD 天然漂移（任务完成后必然推进），用 drift_known 显式标注比靠
// mock 消除更务实——采集的是已固化快照，漂移在采集那刻已发生，无法事后 mock 掉。
type GoldenMeta struct {
	// Source: hand-curated (manually reverse-engineered precise baseline,
	// trustworthy across all dimensions) / auto-collected (collected from TaskState
	// via forge verify --collect-golden, may contain drift).
	//
	// Source：hand-curated（人工反推精确基线，全维度可信）/ auto-collected
	//（forge verify --collect-golden 从 TaskState 采集，可能含漂移）。
	Source string `json:"source,omitempty"`
	// DriftKnown lists dimension names known to be unreliable due to state drift at
	// collection time (currently only scope.
	//
	// DriftKnown 列出已知因采集时刻状态漂移而不可靠的维度名（当前仅 scope——GitDiffStat
	// 含事后 HEAD 推进的改动）。golden test 对这些维度 advisory 不 fail：固化后稳定，但
	// 数值不反映任务真实 diff。留作回归基线仍有价值——scope 之外的维度照常断言。
	DriftKnown []string `json:"drift_known,omitempty"`
}

// ExpectedScore is the subset of ScoreResult that a golden case pins.
//
// ExpectedScore 是 golden case 钉住的 ScoreResult 子集。我们按维度钉分数（非仅
// overall），因为加权平均会用一个维度的补偿性变化掩盖另一个维度的回归。
type ExpectedScore struct {
	Overall    float64        `json:"overall"`
	Grade      string         `json:"grade"`
	Dimensions map[string]int `json:"dimensions"` // dimension name -> score
}
