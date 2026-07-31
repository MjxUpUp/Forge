package scoring

// Golden master collection layer: freezes a real task EvaluateInput into a golden regression fixture.
//
// Orthogonal to canonical golden (testdata/golden/, hand-written clean/poor cases pinning algorithm boundaries):
// golden_real pins the real scoring shape of actual dogfood tasks — guarding against regressions like I tweaked scoreScope and
// dropped every real B-grade task to C, which hand-written fixtures cannot catch and only surface on real combinations.
//
// Collection primitive GoldenCaseFromInput: takes an EvaluateInput, runs Evaluate, and wraps (input, expected)
// into a GoldenCase. Persistence reuses the GoldenCase JSON format; loading happens test-side via
// LoadGoldenCases (golden_test.go), only the dir differs.
//
// Golden master 采集层：把真实任务的 EvaluateInput 固化成 golden 回归 fixture。
//
// 与 canonical golden（testdata/golden/，人工 clean/poor 钉算法边界）正交：
// golden_real 钉的是**真实 dogfood 任务的评分形状**——防「我调了 scoreScope 把所有
// B 级真实任务降到 C」这类靠人工 fixture 抓不到、只在真实组合上才暴露的回归。
//
// 采集原语 GoldenCaseFromInput：输入 EvaluateInput，跑 Evaluate，把 (input, expected)
// 封装成 GoldenCase。落盘复用 GoldenCase 的 JSON 格式；读取在测试侧走
// LoadGoldenCases（golden_test.go），仅 dir 不同。

import "github.com/MjxUpUp/Forge/internal/scoringtypes"

// GoldenCaseFromInput builds a GoldenCase with Expected filled in from an EvaluateInput.
// Collector primitive: a real task scoring input -> golden regression fixture. Expected is computed by the current Evaluate,
// so collection and regression testing must use the same ScoringConfig (DefaultWeights), otherwise config
// drift would be misreported as an algorithm regression. Production collection should further pin the config fingerprint (to be added when the collect command lands).
//
// GoldenCaseFromInput 从 EvaluateInput 构造一个填好 Expected 的 GoldenCase。
// 采集器原语：真实任务的评分输入 → golden 回归 fixture。Expected 由当前 Evaluate
// 算出，故采集与回归测试**必须用同一 ScoringConfig**（DefaultWeights），否则 config
// 漂移会被误报成算法回归。生产采集应进一步固化 config 指纹（待采集命令落地时补）。
func GoldenCaseFromInput(name, rationale string, input *EvaluateInput, config *scoringtypes.ScoringConfig) *GoldenCase {
	res := Evaluate(input, config)
	dims := make(map[string]int, len(res.Dimensions))
	for _, d := range res.Dimensions {
		dims[string(d.Dimension)] = d.Score
	}
	return &GoldenCase{
		Name:      name,
		Rationale: rationale,
		Input:     *input,
		Expected: ExpectedScore{
			Overall:    res.Overall,
			Grade:      res.Grade,
			Dimensions: dims,
		},
	}
}
