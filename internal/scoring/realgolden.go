package scoring

// Golden master 采集层：把真实任务的 EvaluateInput 固化成 golden 回归 fixture。
//
// 与 canonical golden（testdata/golden/，人工 clean/poor 钉算法边界）正交：
// golden_real 钉的是**真实 dogfood 任务的评分形状**——防「我调了 scoreScope 把所有
// B 级真实任务降到 C」这类靠人工 fixture 抓不到、只在真实组合上才暴露的回归。
//
// 采集原语 GoldenCaseFromInput：输入 EvaluateInput，跑 Evaluate，把 (input, expected)
// 封装成 GoldenCase。落盘/读取复用 GoldenCase 的 JSON 格式 + LoadGoldenCases（仅 dir 不同）。

import "github.com/MjxUpUp/Forge/internal/scoringtypes"

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
