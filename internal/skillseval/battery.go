package skillseval

// battery.go — held-out regression battery: aggregation of every anchored skill baseline vs
// its latest run, judged by the SAME deterministic acceptance criteria as eval-report
// (JudgeSkillAccept — single source of truth; the battery must not grow a second judgment
// that can drift from the per-skill one).
//
// Scope honesty (review F3): the battery reads the resolved eval dir (EvalDir()/--dir,
// default ~/.forge/evals — resolution chain in dir.go) shared by the whole eval command
// family — it covers every anchored skill in that dir, not an isolated per-repo subset
// (point --dir at a repo evals/ directory for repo-scoped runs, e.g. in CI). A regressed baseline from
// any project trips the gate (conservative direction: false positive, not false negative);
// per-project scoping would cut across all five eval commands and is a deliberate non-goal
// here. Corollary: on a machine/CI runner with ZERO anchored baselines the battery is EMPTY
// — Total==0, GateBlocked=false, exit 0. That exit 0 means "nothing was checked", not "no
// regression verified"; --gate prints an explicit vacuous note in that case.
//
// Field consensus (AutoDesign Eq 6 acceptance gate / held-out gating): a candidate change is
// accepted only if it improves without regressing vs baseline. Per-skill eval-report already
// computes this; the battery closes the aggregate gap — "did ANY anchored skill regress" was
// previously answerable only by looping eval-report by hand, which nobody does at release
// time. `forge skills battery --gate` turns that loop into one command with a hard exit code.
//
// Boundary: deterministic aggregation only. The battery judges anchored baselines; skills
// without baselines are surfaced as Unanchored (advisory — the coverage gap, not a failure),
// and rows whose judgment is impossible (no latest run / stale baseline mark) stay advisory
// accepts — machine criteria unavailable escalates to human review, mirroring JudgeSkillAccept's
// incomparable branch rather than forcing a fake reject.
//
// battery.go — held-out 回归电池：聚合所有已锚定 baseline 的 skill（各 vs 其最新 run），
// 判据与 eval-report 完全同源（JudgeSkillAccept——单一真相源；电池不得长出第二套可与之
// 漂移的判定）。
//
// 范围诚实性（审查 F3）：电池读的是解析出的 eval 目录（EvalDir()/--dir，默认
// ~/.forge/evals——解析链见 dir.go），整个 eval 命令族共享——覆盖该目录下所有已锚定
// skill，而非按仓库隔离的子集（CI 里把 --dir 指向仓库 evals/ 即按仓库跑）。
// 任何项目的 baseline 回归都会触发门禁（保守方向：假阳性而非假阴性）；按项目隔离要横切
// 全部五个 eval 命令，是本处的刻意非目标。推论：零锚定 baseline 的机器/CI runner 上电池
// 为空——Total==0、GateBlocked=false、exit 0。该 exit 0 意为「没检查任何东西」，不是
// 「已验证无回归」；--gate 在该情形打印显式 vacuous 提示。
//
// 领域共识（AutoDesign Eq 6 验收门 / held-out gating）：候选变更须「有改善且不回归」才
// 接受。单 skill 的 eval-report 已算这个；电池补的是聚合缺口——「有没有任何已锚定
// skill 回归」此前只能手动逐个 eval-report，发版时刻没人做。`forge skills battery
// --gate` 把这个循环变成一条命令 + 硬退出码。
//
// 边界：只做确定性聚合。电池判已锚定的 baseline；未锚定的 skill 浮出为 Unanchored
//（advisory——覆盖缺口，不是失败）；判定不可能的行（无最新 run/baseline 标记过期）保持
// advisory accept——机器判据不可用就交人工复核，对齐 JudgeSkillAccept 的不可比分支，
// 不强造假 reject。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BatterySkillRow is one anchored skill's battery verdict.
//
// BatterySkillRow 是单个已锚定 skill 的电池判定行。
type BatterySkillRow struct {
	Skill string `json:"skill"`
	// BaselineRun: the anchored baseline run id (non-empty — the battery iterates anchored
	// baselines only).
	BaselineRun string `json:"baseline_run"`
	// LatestRun: the latest run id ("" = no run at all — judgment impossible, advisory).
	LatestRun string `json:"latest_run"`
	// JudgmentImpossible: no latest run, or the baseline mark points at a run that no longer
	// exists (stale anchor). Such rows carry Accept=true + advisory Reasons and never GateBlock.
	JudgmentImpossible bool     `json:"judgment_impossible"`
	Comparable         bool     `json:"comparable"`
	IncomparableReason string   `json:"incomparable_reason,omitempty"`
	NetRegressions     int      `json:"net_regressions"`
	RegressionCount    int      `json:"regression_count"`
	ImprovementCount   int      `json:"improvement_count"`
	HealthScore        float64  `json:"health_score"`
	Accept             bool     `json:"accept"`
	Reasons            []string `json:"reasons,omitempty"`
}

// BatteryReport is the repo-level battery result.
//
// BatteryReport 是仓库级电池结果。
type BatteryReport struct {
	Skills []BatterySkillRow `json:"skills"`
	// Total/Rejected/Accepted count battery rows (anchored skills only).
	Total    int `json:"total"`
	Rejected int `json:"rejected"`
	Accepted int `json:"accepted"`
	// Unanchored: skills that have runs but no anchored baseline — outside the battery's
	// protection (advisory coverage gap; anchor via eval-baseline to bring them in).
	Unanchored []string `json:"unanchored,omitempty"`
	// GateBlocked: any row rejected (Eq 6's non-regress half failed for at least one skill).
	GateBlocked bool `json:"gate_blocked"`
}

// BuildBattery aggregates every anchored baseline vs its latest run into one report.
// dir is the eval data dir (EvalDir()). Errors reading baselines/runs propagate — a battery
// that silently skipped unreadable data would report a false all-green (fail-closed).
//
// BuildBattery 把每个已锚定 baseline vs 其最新 run 聚合成一份报告。dir 为 eval 数据目录
// （EvalDir()）。baselines/runs 读取错误上抛——静默跳过不可读数据的电池会报假全绿
// （fail-closed）。
func BuildBattery(dir string) (*BatteryReport, error) {
	baselines, err := LoadBaselines(dir)
	if err != nil {
		return nil, fmt.Errorf("read baselines: %w", err)
	}
	rep := &BatteryReport{Skills: []BatterySkillRow{}}

	for _, skill := range sortedBaselineSkills(baselines) {
		bl := baselines[skill]
		if bl.RunID == "" {
			continue // 空标记不入电池
		}
		row := BatterySkillRow{Skill: skill, BaselineRun: bl.RunID, Accept: true}

		runs, rerr := LoadRuns(dir, skill)
		if rerr != nil {
			return nil, fmt.Errorf("read runs for %s: %w", skill, rerr)
		}
		if len(runs) == 0 {
			row.JudgmentImpossible = true
			row.Reasons = []string{"已锚定 baseline 但无任何 run——判定不可能（run 被清理或从未跑过），交人工复核"}
			rep.Skills = append(rep.Skills, row)
			continue
		}
		latest := &runs[len(runs)-1]
		row.LatestRun = latest.RunID
		row.HealthScore = latest.HealthScore

		// Resolve the anchored baseline run; a stale mark (run file cleaned) degrades to
		// advisory, not hard failure — the mark is metadata, the runs are the record.
		//
		// 解析锚定的 baseline run；标记过期（run 文件已清）降级 advisory 而非硬失败——
		// 标记是元数据，runs 才是记录。
		var baseline *EvalRun
		for i := range runs {
			if runs[i].RunID == bl.RunID {
				baseline = &runs[i]
				break
			}
		}
		if baseline == nil {
			row.JudgmentImpossible = true
			row.Reasons = []string{fmt.Sprintf("baseline run %s 已不存在（标记过期）——判定不可能，交人工复核", bl.RunID)}
			rep.Skills = append(rep.Skills, row)
			continue
		}

		rr := CompareRuns(latest, baseline)
		row.Comparable = rr.Comparable
		row.IncomparableReason = rr.IncomparableReason
		row.NetRegressions = rr.NetRegressions
		row.RegressionCount = len(rr.Regressions)
		row.ImprovementCount = len(rr.Improvements)
		// Single source of truth: the same judge as eval-report consumes.
		//
		// 单一真相源：与 eval-report 同一个判据消费。
		row.Accept, row.Reasons = JudgeSkillAccept(rr)
		rep.Skills = append(rep.Skills, row)
	}

	// Unanchored coverage gap: skills with runs but no baseline mark.
	//
	// 未锚定覆盖缺口：有 run 但无 baseline 标记的 skill。
	unanchored, uerr := skillsWithRunsNoBaseline(dir, baselines)
	if uerr != nil {
		return nil, uerr
	}
	rep.Unanchored = unanchored

	for _, r := range rep.Skills {
		rep.Total++
		if r.Accept {
			rep.Accepted++
		} else {
			rep.Rejected++
		}
	}
	rep.GateBlocked = rep.Rejected > 0
	return rep, nil
}

// sortedBaselineSkills returns the anchored skill names in sorted order (deterministic output).
//
// sortedBaselineSkills 返回排序后的已锚定 skill 名（输出确定性）。
func sortedBaselineSkills(baselines map[string]Baseline) []string {
	names := make([]string, 0, len(baselines))
	for s, bl := range baselines {
		if bl.RunID != "" {
			names = append(names, s)
		}
	}
	sort.Strings(names)
	return names
}

// skillsWithRunsNoBaseline lists skills that have a runs file but no anchored baseline —
// the battery's blind spot made visible (advisory). A runs dir read error propagates.
//
// skillsWithRunsNoBaseline 列出有 runs 文件但未锚定 baseline 的 skill——把电池盲区显性化
// （advisory）。runs 目录读取错误上抛。
func skillsWithRunsNoBaseline(dir string, baselines map[string]Baseline) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "runs"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		skill := strings.TrimSuffix(name, ".jsonl")
		if skill == "" {
			continue
		}
		if bl, ok := baselines[skill]; !ok || bl.RunID == "" {
			out = append(out, skill)
		}
	}
	sort.Strings(out)
	return out, nil
}
