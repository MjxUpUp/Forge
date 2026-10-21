package evalkit

// stats.go — Track A/B 的统计计算契约（docs/design/forge-evaluation-system.md §五）。
// 全部纯函数：无 IO、无时钟——golden 测试用固定输入输出钉住每一段数学。
//
// stats.go — 统计契约（设计 §五）。纯函数：无 IO、无时钟——golden 测试用固定
// 输入输出钉住每一段数学。

import (
	"fmt"
	"math"
	"math/rand"
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
		// 两列完全同分布 → 分母为 0，κ 数学上无定义。返回错误而非满分（对抗
		// 审查 M8：满分是把"无法判定"伪装成"完美一致"）。
		return 0, fmt.Errorf("evalkit: κ 无定义（两列边际分布相同，期望一致率=1）")
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
	// EtaSquaredP: REMOVED 2026-09-04 (adversarial review C2). With one
	// observation per cell (pre-aggregated means) SS_error ≡ 0 and
	// SS_interaction ≡ SS_cells, so η²_p ≡ 0.5 — a constant dressed as a
	// statistic. A real interaction η²_p needs within-cell repeats; the API
	// must accept raw per-attempt scores first. 三必报变两必报（HV̄/MV̄ + 翻转数）。
}

// ModelScores holds one model's mean scores keyed by harness/profile.
//
// ModelScores 保存一个模型按 harness/profile 键控的均值分。
type ModelScores struct {
	Name            string             `json:"name"`
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

	// （η²_p 计算已移除——见 struct 注释：单观测格下恒为 0.5 的废数。）
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

// DeltaCI is a paired profile difference (arm a − arm b, mean pass rate over
// common tasks) with its percentile bootstrap interval (cycle plan R4/R7).
// SE is the bootstrap standard deviation; MDE = (z_{0.975}+z_{0.8})·SE, the
// smallest effect this sample could have detected at α=0.05 two-sided, power 0.8.
//
// DeltaCI 是配对的 profile 差（a 臂 − b 臂，共同任务上的平均通过率）及其分位数
// bootstrap 区间（周期总纲 R4/R7）。SE 是 bootstrap 标准差；MDE =
// (z_{0.975}+z_{0.8})·SE，即该样本在 α=0.05 双侧、power 0.8 下能检出的最小效应。
type DeltaCI struct {
	Delta        float64 `json:"delta"`
	Lo           float64 `json:"lo"`
	Hi           float64 `json:"hi"`
	SE           float64 `json:"se"`
	MDE          float64 `json:"mde"`
	NTasks       int     `json:"n_tasks"`
	NRuns        int     `json:"n_runs"`       // 两臂在共同任务上 attempt 数的较小者
	Perturbation string  `json:"perturbation"` // measured | unmeasured
	CrossesZero  bool    `json:"crosses_zero"`
}

// mdeZ is z_{0.975} + z_{0.8} (α=0.05 two-sided, power 0.8).
//
// mdeZ 是 z_{0.975} + z_{0.8}（α=0.05 双侧、power 0.8）。
const mdeZ = 1.96 + 0.8416

// defaultBootstrapIters is the resample count when the caller passes ≤0.
//
// defaultBootstrapIters 是调用方传 ≤0 时的重采样次数。
const defaultBootstrapIters = 2000

// PairedDeltaCI bootstraps the a−b difference hierarchically: resample tasks
// (task-sampling source), then — only when every common task has
// ≥MinPerturbationVariants variants shared by both arms — resample variants once per task
// and apply the same draw to both arms (the arms share the prompt variants, so
// the variant layer is paired), then resample repeats inside each drawn
// variant per arm (run-to-run source). Without enough variants a task arm's
// attempts are resampled as one pool and Perturbation is unmeasured. The point
// estimate uses the same weighting as the bootstrap (variant-equal when
// measured, attempt-pooled otherwise). Deterministic for a given seed.
//
// PairedDeltaCI 分层 bootstrap a−b 差：先重采任务（任务抽样源）；仅当每个共同任务上
// 两臂共有 ≥MinPerturbationVariants 个变体时，再按任务重采一次变体并让两臂共用同一组
// 抽样（两臂用的是同一组 prompt 变体，变体层是配对的）；再在抽中的变体内按臂
// 重采重复（运行间源）。变体不足时任务臂内 attempts 作为一个池重采，Perturbation
// 记 unmeasured。点估计与 bootstrap 同权（measured 时变体等权，否则按 attempt
// 合并）。给定 seed 结果确定。
func PairedDeltaCI(a, b []Attempt, iters int, seed int64) (*DeltaCI, error) {
	ga, gb := groupAttempts(a), groupAttempts(b)
	var tasks []string
	for id := range ga {
		if _, ok := gb[id]; ok {
			tasks = append(tasks, id)
		}
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("evalkit: 配对差无共同任务（a %d 条、b %d 条 attempt）", len(a), len(b))
	}
	sort.Strings(tasks)
	// 变体层只在两臂共同的变体上配对：任一任务共同变体不足 MinPerturbationVariants
	// 就退回两源（unmeasured），不静默丢变体又标 measured。
	measured := true
	for _, id := range tasks {
		if len(commonVariants(ga[id], gb[id])) < MinPerturbationVariants {
			measured = false
			break
		}
	}
	out := &DeltaCI{NTasks: len(tasks), Perturbation: PerturbationUnmeasured}
	if measured {
		out.Perturbation = PerturbationMeasured
	}
	runsA, runsB := 0, 0
	sharedKeys := map[string][]int{}
	for _, id := range tasks {
		runsA += countOutcomes(ga[id])
		runsB += countOutcomes(gb[id])
		sharedKeys[id] = commonVariants(ga[id], gb[id])
		out.Delta += armPoint(ga[id], sharedKeys[id], measured) - armPoint(gb[id], sharedKeys[id], measured)
	}
	out.Delta /= float64(len(tasks))
	out.NRuns = runsA
	if runsB < out.NRuns {
		out.NRuns = runsB
	}

	if iters <= 0 {
		iters = defaultBootstrapIters
	}
	rng := rand.New(rand.NewSource(seed))
	draws := make([]float64, iters)
	for i := range draws {
		sum := 0.0
		for range tasks {
			id := tasks[rng.Intn(len(tasks))]
			if measured {
				keys := sharedKeys[id]
				drawn := make([]int, len(keys))
				for k := range drawn {
					drawn[k] = keys[rng.Intn(len(keys))]
				}
				sum += resampleVariants(rng, ga[id], drawn) - resampleVariants(rng, gb[id], drawn)
			} else {
				sum += resamplePool(rng, ga[id]) - resamplePool(rng, gb[id])
			}
		}
		draws[i] = sum / float64(len(tasks))
	}
	sort.Float64s(draws)
	out.Lo = percentile(draws, 0.025)
	out.Hi = percentile(draws, 0.975)
	_, out.SE = MeanAndStd(draws)
	out.MDE = mdeZ * out.SE
	out.CrossesZero = out.Lo <= 0 && out.Hi >= 0
	return out, nil
}

// NullResultStatement renders the fixed-form conclusion: a CI crossing zero
// reads "在 n=X 次运行下未检测到显著差异；最小可检测效应 Y pp" (never a one-sided
// claim); otherwise the difference and its interval, in percentage points. A
// zero-variance bootstrap carries no interval information (design §五: zero
// error under a deterministic setup is a calculation alarm) and says so
// instead of printing "MDE 0.0 pp".
//
// NullResultStatement 渲染固定格式结论：CI 跨零时为"在 n=X 次运行下未检测到显著
// 差异；最小可检测效应 Y pp"（禁止单侧表述）；否则给出差值与区间（百分点）。
// bootstrap 方差为 0 时区间不含信息（设计 §五：确定性配置 0 误差 = 计算异常告警），
// 如实说明而不是打印"MDE 0.0 pp"。
func NullResultStatement(ci *DeltaCI) string {
	if ci == nil {
		return "区间不可得"
	}
	if ci.SE == 0 {
		return fmt.Sprintf("差值 %+.1f pp，bootstrap 方差为 0（n=%d 次运行）——区间不含信息，按评测设计 §五作计算异常复核（确定性替身或样本无变化）", ci.Delta*100, ci.NRuns)
	}
	if ci.CrossesZero {
		return fmt.Sprintf("在 n=%d 次运行下未检测到显著差异；最小可检测效应 %.1f pp", ci.NRuns, ci.MDE*100)
	}
	return fmt.Sprintf("差值 %+.1f pp，95%% CI [%+.1f, %+.1f] pp（n=%d 次运行）", ci.Delta*100, ci.Lo*100, ci.Hi*100, ci.NRuns)
}

// groupAttempts indexes pass outcomes as task → variant → outcomes (1/0).
//
// groupAttempts 把通过结果索引为 任务 → 变体 → 结果（1/0）。
func groupAttempts(xs []Attempt) map[string]map[int][]float64 {
	out := map[string]map[int][]float64{}
	for _, x := range xs {
		if out[x.TaskID] == nil {
			out[x.TaskID] = map[int][]float64{}
		}
		v := 0.0
		if x.Pass {
			v = 1
		}
		out[x.TaskID][x.Variant] = append(out[x.TaskID][x.Variant], v)
	}
	return out
}

// commonVariants returns the sorted variant keys present in both arms.
//
// commonVariants 返回两臂都有的变体键（升序）。
func commonVariants(a, b map[int][]float64) []int {
	var keys []int
	for k := range a {
		if _, ok := b[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Ints(keys)
	return keys
}

// countOutcomes counts one task arm's attempts.
//
// countOutcomes 统计一个任务臂的 attempt 数。
func countOutcomes(vs map[int][]float64) int {
	n := 0
	for _, xs := range vs {
		n += len(xs)
	}
	return n
}

// armPoint is one task arm's point estimate: the mean of per-variant means over
// the shared variants when measured, else the pooled mean of every attempt.
//
// armPoint 是一个任务臂的点估计：measured 时为共同变体上各变体均值的均值，否则
// 为全部 attempt 的合并均值。
func armPoint(vs map[int][]float64, keys []int, measured bool) float64 {
	if measured {
		sum := 0.0
		for _, k := range keys {
			sum += mean(vs[k])
		}
		return sum / float64(len(keys))
	}
	var pool []float64
	for _, xs := range vs {
		pool = append(pool, xs...)
	}
	return mean(pool)
}

// resampleVariants draws repeats with replacement inside each of the given
// (already drawn, shared) variants and averages the variant means.
//
// resampleVariants 在给定的（已抽好、两臂共用的）各变体内有放回地重采重复，
// 再对变体均值取平均。
func resampleVariants(rng *rand.Rand, vs map[int][]float64, drawn []int) float64 {
	sum := 0.0
	for _, k := range drawn {
		xs := vs[k]
		sub := 0.0
		for range xs {
			sub += xs[rng.Intn(len(xs))]
		}
		sum += sub / float64(len(xs))
	}
	return sum / float64(len(drawn))
}

// resamplePool draws one same-size bootstrap mean from a task arm's pooled
// attempts (variant keys visited in sorted order for determinism).
//
// resamplePool 从一个任务臂的合并 attempts 中抽一次同样大小的 bootstrap 均值
// （按变体键升序遍历，保证确定性）。
func resamplePool(rng *rand.Rand, vs map[int][]float64) float64 {
	keys := make([]int, 0, len(vs))
	for k := range vs {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var pool []float64
	for _, k := range keys {
		pool = append(pool, vs[k]...)
	}
	sum := 0.0
	for range pool {
		sum += pool[rng.Intn(len(pool))]
	}
	return sum / float64(len(pool))
}

// mean is the arithmetic mean (0 for an empty slice).
//
// mean 是算术均值（空切片为 0）。
func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// percentile reads the q-quantile of an ascending slice (nearest-rank).
//
// percentile 读取升序切片的 q 分位数（最近秩）。
func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(q*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}
