package evalkit

// stats.go — Track A/B 的统计计算契约（docs/design/forge-evaluation-system.md §五）。
// 全部纯函数：无 IO、无时钟——golden 测试用固定输入输出钉住每一段数学。
//
// stats.go — 统计契约（设计 §五）。纯函数：无 IO、无时钟——golden 测试用固定
// 输入输出钉住每一段数学。

import (
	"fmt"
	"math"
	"sort"
)

// WilsonInterval returns the Wilson score interval (z=1.96, 95%) for a
// proportion. Track B metrics (precision/recall/capture rates) report this —
// never a bare percentage. successes ≤ 0 and trials ≤ 0 yield (0,0).
//
// WilsonInterval 返回比例的 Wilson 分数区间（z=1.96，95%）。Track B 指标
// （precision/recall/capture 率）一律报区间，不报裸百分比。successes ≤ 0 或
// trials ≤ 0 时返回 (0,0)。
func WilsonInterval(successes, trials int) (lo, hi float64) {
	if trials <= 0 {
		return 0, 0
	}
	n := float64(trials)
	p := float64(successes) / n
	z := 1.96
	denom := 1 + z*z/n
	center := (p + z*z/(2*n)) / denom
	half := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denom
	lo = math.Max(0, center-half)
	hi = math.Min(1, center+half)
	return lo, hi
}

// CohenKappa computes Cohen's κ over paired nominal judgments. raters and
// truth must have equal nonzero length; categories are taken as the union of
// observed values. κ<0.6 degrades a judge's downstream decisions to advisory
// per the design.
//
// CohenKappa 对成对名义判定计算 Cohen's κ。raters 与 truth 等长且非空；类别取
// 两列观测值的并集。κ<0.6 时该判分器的下游决策按设计降级为 advisory。
func CohenKappa(raters, truth []string) (float64, error) {
	if len(raters) == 0 || len(raters) != len(truth) {
		return 0, fmt.Errorf("evalkit: kappa 输入为空或长度不等（%d vs %d）", len(raters), len(truth))
	}
	cats := map[string]bool{}
	for i := range truth {
		cats[truth[i]] = true
		cats[raters[i]] = true
	}
	observed := 0.0
	for i := range truth {
		if raters[i] == truth[i] {
			observed++
		}
	}
	observed /= float64(len(truth))
	expected := 0.0
	for c := range cats {
		pr, pt := 0.0, 0.0
		for i := range truth {
			if raters[i] == c {
				pr++
			}
			if truth[i] == c {
				pt++
			}
		}
		expected += (pr / float64(len(truth))) * (pt / float64(len(truth)))
	}
	if expected >= 1 {
		return 1, nil
	}
	return (observed - expected) / (1 - expected), nil
}

// ReplayAgreement returns the fraction of k replays that agree with the first
// verdict, plus the distinct verdict count. Deterministic gates must report
// 1.0 / 1 distinct — anything less is a bug finding by contract.
//
// ReplayAgreement 返回 k 次重放中与首个判定一致的比例，以及不同判定数。确定性
// 门禁按契约必须报 1.0 / 1 个不同判定——低于此即 bug finding。
func ReplayAgreement(verdicts []string) (agreement float64, distinct int) {
	if len(verdicts) == 0 {
		return 0, 0
	}
	first := verdicts[0]
	agree := 0
	set := map[string]bool{}
	for _, v := range verdicts {
		set[v] = true
		if v == first {
			agree++
		}
	}
	return float64(agree) / float64(len(verdicts)), len(set)
}

// MeanAndStd returns arithmetic mean and population standard deviation.
//
// MeanAndStd 返回算术均值与总体标准差。
func MeanAndStd(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	mean = sum / float64(len(xs))
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(ss / float64(len(xs)))
}

// VarianceDecomposition is the 2×2 (or 2×3 profile grid) variance split per
// arXiv 2605.23950: per-model harness variance (HV), per-harness model
// variance (MV), their aggregate means, and the count of ranking reversals
// across model-pair×harness-pair comparisons. Partial eta-squared of the
// interaction is included — always reported alongside reversal counts (small
// grids bias η²_p upward; the reversal count is the honest co-report).
//
// VarianceDecomposition 是 2×2（或 2×3 profile 网格）的方差分解，沿 arXiv
// 2605.23950：每模型 harness 方差（HV）、每 harness 模型方差（MV）、两者的聚合
// 均值，以及 model-pair×harness-pair 比较中的排名翻转数。交互项的 partial
// eta-squared 一并给出——必须与翻转数配对报告（小网格正偏 η²_p；翻转数是诚实
// 的配对报告项）。
type VarianceDecomposition struct {
	// Models[i].ScoresByHarness[h] = mean score of model i under harness/profile h.
	Models []ModelScores // Models[i].ScoresByHarness[h] = 模型 i 在 harness/profile h 下的均值分
	// HVPerModel[k] = population variance across harnesses for model k.
	HVPerModel []float64 // HVPerModel[k] = 模型 k 跨 harness 的总体方差
	// MVPerHarness[k] = population variance across models for harness k.
	MVPerHarness []float64 // MVPerHarness[k] = harness k 跨模型的总体方差
	// HvOverMv = mean(HVPerModel) / mean(MVPerHarness); 0 with HvOverMvUndefined
	// when mean MV is 0 (degenerate grid — models tie on every harness, e.g. a
	// single-task manifest; the ratio has no meaning and +Inf is unserializable).
	//
	// HvOverMv = mean(HV)/mean(MV)；mean MV 为 0 时置 0 并标 HvOverMvUndefined
	// （退化网格——模型在每个 harness 上同分，如单任务 manifest；比值无意义且
	// +Inf 不可 JSON 序列化——docker decompose 首跑实测 2026-09-04）。
	HvOverMv float64
	// HvOverMvUndefined marks a degenerate grid (mean MV == 0).
	HvOverMvUndefined bool `json:"hv_over_mv_undefined,omitempty"`
	// Reversals = ranking flips across all model-pair×harness-pair comparisons.
	Reversals int // Reversals = 所有 model-pair×harness-pair 比较中的翻转数
	// PairComparisons = total number of those comparisons.
	PairComparisons int // PairComparisons = 这些比较的总数
	// EtaSquaredP = SS_interaction/(SS_interaction+SS_error) on the two-way grid.
	EtaSquaredP float64 // EtaSquaredP = 二元网格上的 SS_interaction/(SS_interaction+SS_error)
}

// ModelScores holds one model's mean scores keyed by harness/profile.
//
// ModelScores 保存一个模型按 harness/profile 键控的均值分。
type ModelScores struct {
	Name           string             `json:"name"`
	ScoresByHarness map[string]float64 `json:"scores_by_harness"`
}

// DecomposeVariance computes the variance decomposition over a models×harness
// score grid. Requires ≥2 models × ≥2 harnesses (the minimum meaningful
// factorial per the design); scores within a cell are pre-aggregated means.
//
// DecomposeVariance 在 models×harness 分数网格上计算方差分解。要求 ≥2 模型 ×
// ≥2 harness（设计中最小有效 factorial）；格内分数是预先聚合的均值。
func DecomposeVariance(models []ModelScores) (*VarianceDecomposition, error) {
	if len(models) < 2 {
		return nil, fmt.Errorf("evalkit: 方差分解需要 ≥2 个模型，得到 %d", len(models))
	}
	// 统一 harness 轴（网格必须完整——缺失格是规格错误，不是 0 分）。
	harnesses := make([]string, 0, len(models[0].ScoresByHarness))
	for h := range models[0].ScoresByHarness {
		harnesses = append(harnesses, h)
	}
	sort.Strings(harnesses)
	if len(harnesses) < 2 {
		return nil, fmt.Errorf("evalkit: 方差分解需要 ≥2 个 harness/profile，得到 %d", len(harnesses))
	}
	grid := make([][]float64, len(models)) // grid[model][harness]
	for mi, m := range models {
		if len(m.ScoresByHarness) != len(harnesses) {
			return nil, fmt.Errorf("evalkit: 模型 %s 的 harness 格数（%d）与首个模型（%d）不一致", m.Name, len(m.ScoresByHarness), len(harnesses))
		}
		grid[mi] = make([]float64, len(harnesses))
		for hi, h := range harnesses {
			v, ok := m.ScoresByHarness[h]
			if !ok {
				return nil, fmt.Errorf("evalkit: 模型 %s 缺 harness %s 的分数", m.Name, h)
			}
			grid[mi][hi] = v
		}
	}

	out := &VarianceDecomposition{Models: models}
	for mi := range grid {
		_, std := MeanAndStd(grid[mi])
		out.HVPerModel = append(out.HVPerModel, std*std)
	}
	for hi := range harnesses {
		col := make([]float64, len(grid))
		for mi := range grid {
			col[mi] = grid[mi][hi]
		}
		_, std := MeanAndStd(col)
		out.MVPerHarness = append(out.MVPerHarness, std*std)
	}
	meanHV, _ := MeanAndStd(out.HVPerModel)
	meanMV, _ := MeanAndStd(out.MVPerHarness)
	if meanMV == 0 {
		out.HvOverMv = 0
		out.HvOverMvUndefined = true
	} else {
		out.HvOverMv = meanHV / meanMV
	}

	// 排名翻转：每对模型 (a,b)，在每对 harness (h1,h2) 上比较 a 与 b 的排序是否
	// 改变。a==b 的平局不计翻转（无信息）。
	for a := 0; a < len(grid); a++ {
		for b := a + 1; b < len(grid); b++ {
			for h1 := 0; h1 < len(harnesses); h1++ {
				for h2 := h1 + 1; h2 < len(harnesses); h2++ {
					out.PairComparisons++
					ab := sign(grid[a][h1] - grid[b][h1])
					ba := sign(grid[a][h2] - grid[b][h2])
					if ab != 0 && ba != 0 && ab != ba {
						out.Reversals++
					}
				}
			}
		}
	}

	// η²_p（交互项）：二元 ANOVA 的 SS_interaction/(SS_interaction+SS_error)。
	// 固定效应口径，与 2605.23950 Eq.(2) 一致；小网格正偏——恒与 Reversals 配对
	// 报告。
	nM, nH := float64(len(grid)), float64(len(harnesses))
	gm := 0.0
	for mi := range grid {
		for hi := range harnesses {
			gm += grid[mi][hi]
		}
	}
	gm /= nM * nH
	var ssModel, ssHarness float64
	rowMeans := make([]float64, len(grid))
	colMeans := make([]float64, len(harnesses))
	for mi := range grid {
		m, _ := MeanAndStd(grid[mi])
		rowMeans[mi] = m
		ssModel += nH * (m - gm) * (m - gm)
	}
	for hi := range harnesses {
		col := make([]float64, len(grid))
		for mi := range grid {
			col[mi] = grid[mi][hi]
		}
		m, _ := MeanAndStd(col)
		colMeans[hi] = m
		ssHarness += nM * (m - gm) * (m - gm)
	}
	var ssTotal, ssCells float64
	for mi := range grid {
		for hi := range harnesses {
			d := grid[mi][hi] - gm
			ssTotal += d * d
			c := grid[mi][hi] - rowMeans[mi] - colMeans[hi] + gm
			ssCells += c * c
		}
	}
	ssInter := ssTotal - ssModel - ssHarness
	if ssInter < 0 {
		ssInter = 0
	}
	if ssInter+ssCells > 0 {
		out.EtaSquaredP = ssInter / (ssInter + ssCells)
	}
	return out, nil
}

func sign(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	default:
		return 0
	}
}
