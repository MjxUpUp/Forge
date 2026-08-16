// Package skillseval drift.go — 生产判定集 vs 仓库源 triggers 声明对比（纯逻辑）。
//
// 背景（2026-08-16 触发臂审查）：v1.32.0 build（8/15 11:07）早于 triggers 补齐提交
// （8/15 19:53）8h46m，生产 embed cache 只带 15/33 个 triggers——18 个 skill 的触发声明
// 在生产判定中不存在，8/15 后命中的独特 skill 集因此没有扩大。该漂移当时靠人工翻两个目录
// 五步才确认；本文件把它变成一行确定性输出（`forge skills usage` 漂移行），下次发版滞后
// 一秒可见。
//
// 目录扫描（skilltrigger.LoadAll）在 cli 层做——本包若 import skilltrigger 会成环
// （skilltrigger → taskpipeline → skillseval），故此处只收 skill 名集做纯对比。
//
// Package skillseval drift.go — production trigger set vs repo-source trigger declarations
// (pure logic).
//
// Background (2026-08-16 trigger-arm audit): v1.32.0 (built 8/15 11:07) predates the
// trigger-top-up commit (8/15 19:53) by 8h46m, so the production embed cache carries only
// 15/33 trigger declarations — 18 skills' triggers simply do not exist in production
// evaluation, which is why the distinct-hit skill set never widened after 8/15. Confirming
// that drift took five manual steps across two directories; this file turns it into one
// deterministic line (the `forge skills usage` drift line) so the next release lag is visible
// in a second.
//
// Directory scanning (skilltrigger.LoadAll) stays in the cli layer — importing skilltrigger
// here would create a cycle (skilltrigger → taskpipeline → skillseval), so this file only
// takes skill-name sets and does the pure comparison.
package skillseval

import (
	"slices"
)

// TriggerSetDrift 是两侧判定集的对比结果。RepoCompared=false 表示仓库源不可比较
// （非 forge 仓库内运行 / skills/ 目录不存在）——此时只有 ProdDeclared 有意义，
// MissingInProd 为空且不能解读为「无漂移」。
//
// TriggerSetDrift compares the two trigger sets. RepoCompared=false means the repo side is
// not comparable (running outside a forge repo / no skills/ directory) — then only
// ProdDeclared is meaningful, MissingInProd is empty and MUST NOT be read as "no drift".
type TriggerSetDrift struct {
	ProdDeclared  int      `json:"prod_declared"`
	RepoDeclared  int      `json:"repo_declared"`
	RepoCompared  bool     `json:"repo_compared"`
	MissingInProd []string `json:"missing_in_prod"`
}

// CompareTriggerSets 对比生产判定集与仓库源判定集（两侧均为「带 triggers 声明的 skill
// 名集」，由调用方用 skilltrigger.LoadAll 产出——数的是引擎会装载的判定集，不是文本
// grep 的近似）。repo 传 nil 表示不可比较。
//
// CompareTriggerSets compares the production trigger set against the repo-source set (both
// are "skill-name sets carrying trigger declarations", produced by the caller via
// skilltrigger.LoadAll — counting the set the ENGINE will load, not a textual grep
// approximation). Pass repo=nil for "not comparable".
func CompareTriggerSets(prod, repo map[string]bool) TriggerSetDrift {
	d := TriggerSetDrift{ProdDeclared: len(prod)}
	if repo == nil {
		return d
	}
	d.RepoCompared = true
	d.RepoDeclared = len(repo)
	for name := range repo {
		if !prod[name] {
			d.MissingInProd = append(d.MissingInProd, name)
		}
	}
	slices.Sort(d.MissingInProd)
	return d
}

// HasDrift 报告是否存在漂移（仓库侧可比较且生产缺声明）。便于调用方一行判断。
//
// HasDrift reports whether drift exists (repo comparable and production missing
// declarations). One-line check for callers.
func (d TriggerSetDrift) HasDrift() bool {
	return d.RepoCompared && len(d.MissingInProd) > 0
}
