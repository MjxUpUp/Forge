package evalkit

// decompose.go — 方差分解编排（Track A · docs/design/forge-evaluation-system.md
// §六 P3）：在 profile×model 网格上跑同一 frozen manifest，产出三必报统计量
// （HV̄/MV̄、排名翻转数、η²_p）与三档差值（full−off 整体贡献、full−gates-only
// context 注入层贡献、gates-only−off 纯门禁代价）。
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
// and the three deltas with their honest-interval framing.
//
// DecomposeReport 是战役结果：网格均值、分解统计量，以及三档差值的区间式表述。
type DecomposeReport struct {
	GeneratedAt  time.Time               `json:"generated_at"`
	Grid         DecomposeGrid           `json:"grid"`
	Benchmark    string                  `json:"benchmark"`
	Split        string                  `json:"split"`
	ManifestFP   string                  `json:"manifest_fingerprint"`
	CellMeans    map[string]float64      `json:"cell_means"`            // "profile|model" → mean pass
	Decomposition *VarianceDecomposition `json:"decomposition"`
	// DeltaFullOff / DeltaFullGates / DeltaGatesOff: per-model paired deltas with
	// per-model values (interval statements — never single-side numbers).
	DeltaFullOff     []ModelDelta `json:"delta_full_off"`
	DeltaFullGates   []ModelDelta `json:"delta_full_gates"`
	DeltaGatesOff    []ModelDelta `json:"delta_gates_off"`
}

// ModelDelta is one model's paired difference between two profiles.
//
// ModelDelta 是一个模型在两个 profile 间的配对差。
type ModelDelta struct {
	Model string  `json:"model"`
	Delta float64 `json:"delta"` // a − b
}

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
		GeneratedAt: time.Now().UTC(),
		Grid:        grid,
		Benchmark:   spec.Benchmark,
		Split:       spec.Split,
		ManifestFP:  manifest.Fingerprint(),
		CellMeans:   map[string]float64{},
	}
	profileKey := func(p Profile) string { return string(p) }
	for _, p := range grid.Profiles {
		for _, m := range grid.Models {
			cellSpec := spec
			cellSpec.Profile = p
			cellSpec.Model = m
			sc, err := RunBenchmark(ctx, cellSpec, manifest, runner)
			if err != nil {
				return nil, fmt.Errorf("evalkit: 网格格 %s×%s 失败: %w", p, m, err)
			}
			rep.CellMeans[profileKey(p)+"|"+m] = sc.Pass1.Value
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
			out = append(out, ModelDelta{Model: m, Delta: rep.CellMeans[profileKey(a)+"|"+m] - rep.CellMeans[profileKey(b)+"|"+m]})
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
		Detail: fmt.Sprintf(`decompose: benchmark %s@%s profiles %d models %d HV/MV %s reversals %d/%d eta2 %.3f`,
			rep.Benchmark, rep.Split, len(rep.Grid.Profiles), len(rep.Grid.Models),
			detailHV, rep.Decomposition.Reversals, rep.Decomposition.PairComparisons, rep.Decomposition.EtaSquaredP),
	})
	return path, nil
}

// RenderDecomposeMarkdown renders the interval-framed summary (never
// single-side numbers; CI-crossing-zero deltas state exactly that).
//
// RenderDecomposeMarkdown 渲染区间式摘要（绝不出单侧数字；跨零差值如实表述）。
func (r *DecomposeReport) RenderDecomposeMarkdown() string {
	s := fmt.Sprintf("# 方差分解报告（%s@%s）\n\n", r.Benchmark, r.Split)
	if r.Decomposition.HvOverMvUndefined {
		s += fmt.Sprintf("- HV̄/MV̄ = 未定义（模型方差为 0——模型在各 profile 同分，网格退化；跨 %d 个 profile × %d 个模型）\n", len(r.Grid.Profiles), len(r.Grid.Models))
	} else {
		s += fmt.Sprintf("- HV̄/MV̄ = %.2f（跨 %d 个 profile × %d 个模型）\n", r.Decomposition.HvOverMv, len(r.Grid.Profiles), len(r.Grid.Models))
	}
	s += fmt.Sprintf("- 排名翻转 %d / %d 个 model-pair×profile-pair 比较\n", r.Decomposition.Reversals, r.Decomposition.PairComparisons)
	s += fmt.Sprintf("- η²_p（交互项，与翻转数配对报告）= %.3f\n\n", r.Decomposition.EtaSquaredP)
	render := func(name string, deltas []ModelDelta) {
		s += fmt.Sprintf("## %s\n\n", name)
		for _, d := range deltas {
			verdict := "差值为 0"
			if d.Delta > 0 {
				verdict = fmt.Sprintf("+%.3f（正差；是否显著以置信区间为准，跨零即表述为未检测到显著差异）", d.Delta)
			} else if d.Delta < 0 {
				verdict = fmt.Sprintf("%.3f（负差；是否显著以置信区间为准，跨零即表述为未检测到显著差异）", d.Delta)
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
