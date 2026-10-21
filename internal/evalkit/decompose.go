package evalkit

// decompose.go — 方差分解编排（Track A · docs/design/forge-evaluation-system.md
// §六 P3）：在 profile×model 网格上跑同一 frozen manifest，产出两必报统计量
// （HV̄/MV̄、排名翻转数；η²_p 需格内重复数据，单观测格下数学不可定义——见
// stats.go 注释）与三档差值（full−off 整体贡献、full−gates-only context 注入层
// 贡献、gates-only−off 纯门禁代价）。
//
// decompose.go — variance-decomposition orchestration: run the same frozen
// manifest across the profile×model grid and produce the three mandatory
// statistics (HV̄/MV̄, reversal count, η²_p) plus the three deltas (full−off
// overall contribution, full−gates-only context-injection contribution,
// gates-only−off pure-gate cost).

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// DecomposeGrid is one campaign's experimental grid.
//
// DecomposeGrid 是一次战役的实验网格。
type DecomposeGrid struct {
	Profiles []Profile
	Models   []string
}

// Validate requires ≥2 profiles × ≥2 models (minimum meaningful factorial).
//
// Validate 要求 ≥2 profile × ≥2 model（最小有效 factorial）。
func (g *DecomposeGrid) Validate() error {
	if len(g.Profiles) < 2 || len(g.Models) < 2 {
		return fmt.Errorf("evalkit: 方差分解需要 ≥2 profile × ≥2 model（得到 %d×%d）", len(g.Profiles), len(g.Models))
	}
	for _, p := range g.Profiles {
		if !ValidProfile(p) {
			return fmt.Errorf("evalkit: profile %q 非法", p)
		}
	}
	return nil
}

// DecomposeReport is the campaign's result: the grid means, the decomposition,
// and the three deltas with their honest-interval framing. Sandbox records the
// execution backend — a scripted report is offline-stand-in data and must be
// visibly labeled wherever consumed (adversarial review I6).
//
// DecomposeReport 是战役结果：网格均值、分解统计量，以及三档差值的区间式表述。
// Sandbox 记录执行后端——scripted 报告是离线替身数据，任何消费面都必须可见地
// 标注（对抗审查 I6）。
type DecomposeReport struct {
	GeneratedAt   time.Time              `json:"generated_at"`
	Grid          DecomposeGrid          `json:"grid"`
	Benchmark     string                 `json:"benchmark"`
	Split         string                 `json:"split"`
	Sandbox       string                 `json:"sandbox"`
	ManifestFP    string                 `json:"manifest_fingerprint"`
	CellMeans     map[string]float64     `json:"cell_means"` // "profile|model" → 全部 attempt 的平均通过率（与三档差值同口径）
	Decomposition *VarianceDecomposition `json:"decomposition"`
	// DeltaFullOff / DeltaFullGates / DeltaGatesOff: per-model paired deltas with
	// per-model values (interval statements — never single-side numbers).
	DeltaFullOff   []ModelDelta `json:"delta_full_off"`
	DeltaFullGates []ModelDelta `json:"delta_full_gates"`
	DeltaGatesOff  []ModelDelta `json:"delta_gates_off"`
	// Perturbation is measured | unmeasured (R4): unmeasured CIs omit the
	// prompt-variant variance source and may be too narrow.
	//
	// Perturbation 是 measured | unmeasured（R4）：unmeasured 的 CI 缺 prompt 变体
	// 方差源，可能偏窄。
	Perturbation string `json:"perturbation"`
	// Preflight is the grid-wide validity preflight (run once, shared by every
	// cell — R3).
	//
	// Preflight 是整张网格共用的有效性 preflight（只跑一次，各格共享——R3）。
	Preflight *PreflightReport `json:"preflight,omitempty"`
}

// ModelDelta is one model's paired difference between two profiles.
//
// ModelDelta 是一个模型在两个 profile 间的配对差。
type ModelDelta struct {
	Model string `json:"model"`
	// Delta is a − b. With a CI it is the CI's own point estimate (mean pass
	// rate over all attempts of the common tasks) so the number and its
	// interval never disagree; without one it falls back to the cell-mean
	// difference (same all-attempt pass rate).
	//
	// Delta 是 a − b。有 CI 时取 CI 自己的点估计（共同任务上全部 attempt 的平均
	// 通过率），数字与区间永不矛盾；无 CI 时退回格均值之差（同为全部 attempt
	// 的通过率）。
	Delta float64 `json:"delta"`
	// CI is the paired bootstrap interval (nil with CIError when not computable).
	//
	// CI 是配对 bootstrap 区间（不可算时为 nil，原因在 CIError）。
	CI      *DeltaCI `json:"ci,omitempty"`
	CIError string   `json:"ci_error,omitempty"`
}

// attemptPassRate is the pass rate over every attempt of one cell — the
// decomposition and the deltas share this estimator (pass@1 alone would let
// the variance split and the deltas disagree on the same report).
//
// attemptPassRate 是一格全部 attempt 的通过率——方差分解与三档差值共用这一
// 估计量（只用 pass@1 会让同一份报告里的分解与差值口径打架）。
func attemptPassRate(as []Attempt) float64 {
	if len(as) == 0 {
		return 0
	}
	n := 0
	for _, a := range as {
		if a.Pass {
			n++
		}
	}
	return float64(n) / float64(len(as))
}

// decomposeBootstrapSeed fixes the bootstrap stream so a report re-renders
// identically from the same attempts.
//
// decomposeBootstrapSeed 固定 bootstrap 随机流——同一批 attempts 重算出同一份报告。
const decomposeBootstrapSeed = 20261013

// RunDecompose executes the grid and computes the decomposition. Cell means
// come from the same runner abstraction as Track A runs (ScriptedRunner in
// offline tests).
//
// RunDecompose 执行网格并计算分解。格均值来自与 Track A 运行相同的 runner 抽象
// （离线测试用 ScriptedRunner）。
func RunDecompose(ctx context.Context, grid DecomposeGrid, spec RunSpec, manifest *BenchmarkManifest, runner TaskRunner) (*DecomposeReport, error) {
	if err := grid.Validate(); err != nil {
		return nil, err
	}
	// 基准 spec 的 profile 由网格逐格覆盖——这里置合法默认以满足 spec 校验。
	spec.Profile = ProfileFull
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	rep := &DecomposeReport{
		GeneratedAt:  time.Now().UTC(),
		Grid:         grid,
		Benchmark:    spec.Benchmark,
		Split:        spec.Split,
		Sandbox:      sandboxLabel(runner),
		ManifestFP:   manifest.Fingerprint(),
		CellMeans:    map[string]float64{},
		Perturbation: spec.PerturbationStatus(),
	}
	// preflight 与 profile 无关：整张网格只跑一次（NFR-1 口径），各格复用。
	report, leak, err := runPreflight(ctx, spec, manifest, runner)
	if err != nil {
		return nil, err
	}
	rep.Preflight = report
	shared := &preflightResult{report: report, leak: leak}
	cellAttempts := map[string][]Attempt{}
	profileKey := func(p Profile) string { return string(p) }
	for _, p := range grid.Profiles {
		for _, m := range grid.Models {
			cellSpec := spec
			cellSpec.Profile = p
			cellSpec.Model = m
			cellSpec.preflight = shared
			sc, err := RunBenchmark(ctx, cellSpec, manifest, runner)
			if err != nil {
				return nil, fmt.Errorf("evalkit: 网格格 %s×%s 失败: %w", p, m, err)
			}
			rep.CellMeans[profileKey(p)+"|"+m] = attemptPassRate(sc.Attempts)
			cellAttempts[profileKey(p)+"|"+m] = sc.Attempts
		}
	}
	// 组装方差分解网格。
	var rows []ModelScores
	for _, m := range grid.Models {
		scores := map[string]float64{}
		for _, p := range grid.Profiles {
			scores[string(p)] = rep.CellMeans[profileKey(p)+"|"+m]
		}
		rows = append(rows, ModelScores{Name: m, ScoresByHarness: scores})
	}
	dec, err := DecomposeVariance(rows)
	if err != nil {
		return nil, err
	}
	rep.Decomposition = dec
	delta := func(a, b Profile) []ModelDelta {
		var out []ModelDelta
		for _, m := range grid.Models {
			ka, kb := profileKey(a)+"|"+m, profileKey(b)+"|"+m
			d := ModelDelta{Model: m, Delta: rep.CellMeans[ka] - rep.CellMeans[kb]}
			_, okA := rep.CellMeans[ka]
			_, okB := rep.CellMeans[kb]
			if !okA || !okB {
				// 网格未含其中一档 profile（如 --profiles off,full 时涉及 gates-only 的两档）。
				d.CIError = fmt.Sprintf("网格未含 %s 或 %s", a, b)
			} else if ci, err := PairedDeltaCI(cellAttempts[ka], cellAttempts[kb], 0, decomposeBootstrapSeed); err != nil {
				d.CIError = err.Error()
			} else {
				d.CI = ci
				d.Delta = ci.Delta
			}
			out = append(out, d)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
		return out
	}
	rep.DeltaFullOff = delta(ProfileFull, ProfileOff)
	rep.DeltaFullGates = delta(ProfileFull, ProfileGatesOnly)
	rep.DeltaGatesOff = delta(ProfileGatesOnly, ProfileOff)
	return rep, nil
}

// PersistDecomposeReport writes the JSON report and the eval-decompose row.
//
// PersistDecomposeReport 写 JSON 报告与 eval-decompose 行。
func PersistDecomposeReport(evalDir string, repoRoot string, rep *DecomposeReport) (string, error) {
	dir := evalDataDir(evalDir)
	data, err := jsonMarshal(rep)
	if err != nil {
		return "", err
	}
	path := filepathJoin(dir, fmt.Sprintf("decompose-%s.json", rep.GeneratedAt.UTC().Format("20060102-150405")))
	if err := atomicWriteFile(path, data); err != nil {
		return "", err
	}
	detailHV := fmt.Sprintf("%.2f", rep.Decomposition.HvOverMv)
	if rep.Decomposition.HvOverMvUndefined {
		detailHV = "undefined(MV=0)"
	}
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckEvalDecompose,
		Passed:  true,
		Checked: true,
		Detail: fmt.Sprintf(`decompose: benchmark %s@%s sandbox=%s profiles %d models %d HV/MV %s reversals %d/%d（eta2 单观测格不可定义，未产出）`,
			rep.Benchmark, rep.Split, rep.Sandbox, len(rep.Grid.Profiles), len(rep.Grid.Models),
			detailHV, rep.Decomposition.Reversals, rep.Decomposition.PairComparisons),
	})
	return path, nil
}

// RenderDecomposeMarkdown renders the interval-framed summary (never
// single-side numbers; CI-crossing-zero deltas state exactly that).
//
// RenderDecomposeMarkdown 渲染区间式摘要（绝不出单侧数字；跨零差值如实表述）。
func (r *DecomposeReport) RenderDecomposeMarkdown() string {
	s := fmt.Sprintf("# 方差分解报告（%s@%s，sandbox=%s）\n\n", r.Benchmark, r.Split, r.Sandbox)
	if r.Sandbox == SandboxScripted {
		s += "> ⚠ sandbox=scripted——离线确定性替身数据，仅供管线自检，不得作为性能结论引用或入季度报告。\n\n"
	}
	if r.Decomposition.HvOverMvUndefined {
		s += fmt.Sprintf("- HV̄/MV̄ = 未定义（模型方差为 0——模型在各 profile 同分，网格退化；跨 %d 个 profile × %d 个模型）\n", len(r.Grid.Profiles), len(r.Grid.Models))
	} else {
		s += fmt.Sprintf("- HV̄/MV̄ = %.2f（跨 %d 个 profile × %d 个模型）\n", r.Decomposition.HvOverMv, len(r.Grid.Profiles), len(r.Grid.Models))
	}
	s += fmt.Sprintf("- 排名翻转 %d / %d 个 model-pair×profile-pair 比较\n", r.Decomposition.Reversals, r.Decomposition.PairComparisons)
	s += "- η²_p：单观测格（格均值输入）下数学不可定义（SS_error≡0）——未产出；接入格内重复数据后再补\n"
	if r.Perturbation == PerturbationMeasured {
		s += "- perturbation=measured：CI 含任务抽样、prompt 变体、运行间三个方差源\n\n"
	} else {
		s += fmt.Sprintf("- perturbation=unmeasured：未声明 ≥%d 个语义等价 prompt 变体，CI 只含任务抽样与运行间两源，可能偏窄\n\n", MinPerturbationVariants)
	}
	if r.Preflight != nil {
		s += fmt.Sprintf("- preflight=%s（整张网格共用一次）\n\n", r.Preflight.Status)
	}
	render := func(name string, deltas []ModelDelta) {
		s += fmt.Sprintf("## %s\n\n", name)
		for _, d := range deltas {
			verdict := NullResultStatement(d.CI)
			if d.CI == nil && d.CIError != "" {
				verdict = "区间不可得（" + d.CIError + "）——不出点差结论"
			}
			s += fmt.Sprintf("- %s: %s\n", d.Model, verdict)
		}
		s += "\n"
	}
	render("整体贡献（full − off）", r.DeltaFullOff)
	render("Context 注入层贡献（full − gates-only）", r.DeltaFullGates)
	render("纯门禁代价（gates-only − off）", r.DeltaGatesOff)
	return s
}
