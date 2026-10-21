package evalkit

// preflight.go — Track A 有效性前置（周期总纲 R3；docs/design/forge-evaluation-system.md
// §六 P2 / §十）：主评测开跑前，先用两条基线证明"评测本身合法"——
//   - oracle 臂（参考解 + 判分）必须全部通过：不通过 = 环境坏了，分数无从谈起；
//   - do-nothing 臂（什么都不做直接判分）必须全部失败：通过 = 判分定义有缺陷，
//     高分可能来自判分漏洞而非能力。
// 任一违例即拒绝启动（PreflightError → CLI exit 2）——这是设计 §十唯一许可的
// BLOCKED 形态（"只阻断评测自身不合法"）。另跑可选的 leakage 臂（只读题面的
// 退化解，由 manifest 的 leakage_cmd 声明）：它不阻断，只把"题面直接可答"的
// 程度与主分数并列披露，并把这些任务排进复核清单。
//
// 基线只在声明了独立判分命令（test_cmd）的任务、且 runner 实现 BaselineRunner
// 时有定义；command-suite 任务的命令既是解法又是判分，基线无意义，如实标 n/a。
//
// preflight.go — Track A validity preflight (cycle plan R3): before the main
// run, the oracle arm (reference solution + grader) must pass every task and
// the do-nothing arm (grader alone) must fail every task; any violation
// refuses the run (PreflightError → exit 2). An optional leakage arm
// (statement-only degenerate solution, manifest leakage_cmd) is disclosed next
// to the main score and feeds the review queue. Baselines are defined only
// for tasks with a separate grader (test_cmd) on runners implementing
// BaselineRunner; otherwise the status is n/a.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// BaselineArm names one preflight baseline.
//
// BaselineArm 命名一条 preflight 基线。
type BaselineArm string

const (
	ArmOracle    BaselineArm = "oracle"     // 参考解 + 判分：必须通过
	ArmDoNothing BaselineArm = "do-nothing" // 只判分：必须失败
	ArmLeakage   BaselineArm = "leakage"    // 只读题面的退化解 + 判分：只披露
)

// Preflight statuses (single source for scorecard / CLI rendering).
//
// Preflight 状态（scorecard / CLI 渲染的单一真相源）。
const (
	PreflightPassed  = "passed"  // 全部判分任务的基线都合法
	PreflightNA      = "n/a"     // runner 不支持基线或无判分任务——未校验，如实披露
	PreflightSkipped = "skipped" // 逃生舱跳过（证据封顶 Weak）
)

// PreflightDisableEnv is the escape hatch: FORGE_EVAL_PREFLIGHT=skip skips the
// preflight (the caller lands an escape-hatch row; evidence is capped Weak).
//
// PreflightDisableEnv 是逃生舱：FORGE_EVAL_PREFLIGHT=skip 跳过 preflight（调用方
// 落 escape-hatch 行；证据封顶 Weak）。
const PreflightDisableEnv = "FORGE_EVAL_PREFLIGHT"

// reviewQueueSize is how many top-score tasks the review queue is filled up to
// (leakage-pass tasks always enter, even beyond this size).
//
// reviewQueueSize 是复核清单按高分补足到的条数（leakage 通过的任务一律入列，
// 可超出此数）。
const reviewQueueSize = 5

// BaselineRunner is implemented by runners that can execute preflight arms.
// applicable=false means the arm has no definition for this task on this
// backend (no test_cmd, or no leakage_cmd for the leakage arm).
//
// BaselineRunner 由能执行 preflight 基线的 runner 实现。applicable=false 表示该
// 基线对此任务在本后端无定义（无 test_cmd，或 leakage 臂无 leakage_cmd）。
type BaselineRunner interface {
	RunBaseline(ctx context.Context, spec RunSpec, task BenchTask, arm BaselineArm) (res TaskResult, applicable bool, err error)
}

// PreflightReport records what the preflight checked.
//
// PreflightReport 记录 preflight 校验了什么。
type PreflightReport struct {
	Status          string   `json:"status"`
	Checked         int      `json:"checked"` // 有判分命令、实际跑了基线的任务数
	OracleFailed    []string `json:"oracle_failed,omitempty"`
	OracleMissing   []string `json:"oracle_missing,omitempty"`
	DoNothingPassed []string `json:"do_nothing_passed,omitempty"`
	Note            string   `json:"note,omitempty"`
}

// PreflightError is the refusal: the evaluation itself is invalid.
//
// PreflightError 是拒绝启动：评测自身不合法。
type PreflightError struct {
	Report *PreflightReport
}

// Error renders the refusal with the first-ship BLOCKED declaration required
// by compat-commitments §二 第 1 条②.
//
// Error 渲染拒绝文案，带 compat-commitments 第二节第 1 条②要求的首发声明。
func (e *PreflightError) Error() string {
	var parts []string
	if len(e.Report.OracleMissing) > 0 {
		parts = append(parts, fmt.Sprintf("判分任务未声明 oracle_cmd（无法证明环境可解）: %s", strings.Join(e.Report.OracleMissing, ", ")))
	}
	if len(e.Report.OracleFailed) > 0 {
		parts = append(parts, fmt.Sprintf("oracle 未通过（环境坏了）: %s", strings.Join(e.Report.OracleFailed, ", ")))
	}
	if len(e.Report.DoNothingPassed) > 0 {
		parts = append(parts, fmt.Sprintf("do-nothing 通过（判分定义有缺陷）: %s", strings.Join(e.Report.DoNothingPassed, ", ")))
	}
	return fmt.Sprintf("evalkit: preflight 违例，拒绝启动——%s（首发即 blocked，见 docs/design/compat-commitments.md；确需跳过设 %s=skip，证据封顶 Weak）",
		strings.Join(parts, "；"), PreflightDisableEnv)
}

// LeakageSummary is the leakage-baseline result rendered next to pass@1.
//
// LeakageSummary 是与 pass@1 并列渲染的 leakage 基线结果。
type LeakageSummary struct {
	Status      string    `json:"status"` // measured | unmeasured
	Rate        RateValue `json:"rate"`
	PassedTasks []string  `json:"passed_tasks,omitempty"`
	// TimedOut counts leakage arms cut by the wallclock budget (excluded from
	// the denominator, disclosed so "unmeasured" is never misread as undeclared).
	//
	// TimedOut 统计被墙钟预算截断的 leakage 臂（不进分母，但披露——免得
	// unmeasured 被误读成未声明）。
	TimedOut int `json:"timed_out,omitempty"`
}

// Leakage statuses.
//
// Leakage 状态。
const (
	LeakageMeasured   = "measured"
	LeakageUnmeasured = "unmeasured"
)

// ReviewItem is one task queued for manual review.
//
// ReviewItem 是排进人工复核清单的一个任务。
type ReviewItem struct {
	TaskID   string  `json:"task_id"`
	PassRate float64 `json:"pass_rate"`
	Tokens   int     `json:"tokens"`
	Reason   string  `json:"reason"` // leakage-pass | top-score
}

// Review reasons.
//
// 复核原因。
const (
	ReviewLeakagePass = "leakage-pass" // 只读题面就能通过——疑似泄漏或判分过宽
	ReviewTopScore    = "top-score"    // 高分 attempt——高分集中了违规（Identical Runs）
)

// preflightResult bundles one preflight outcome so a caller can run it once
// and hand it to several RunBenchmark calls (RunDecompose).
//
// preflightResult 打包一次 preflight 结果，供调用方跑一次后交给多次
// RunBenchmark 复用（RunDecompose）。
type preflightResult struct {
	report *PreflightReport
	leak   *LeakageSummary
}

// PreflightEscaped reports whether the preflight escape hatch is armed.
//
// PreflightEscaped 报告 preflight 逃生舱是否开启。
func PreflightEscaped() bool {
	return os.Getenv(PreflightDisableEnv) == "skip"
}

// runPreflight executes the baselines. A violation returns *PreflightError;
// execution errors propagate unchanged (an infra failure is not a verdict).
//
// runPreflight 执行基线。违例返回 *PreflightError；执行错误原样上抛（基础设施
// 故障不是裁决）。
func runPreflight(ctx context.Context, spec RunSpec, manifest *BenchmarkManifest, runner TaskRunner) (*PreflightReport, *LeakageSummary, error) {
	leak := &LeakageSummary{Status: LeakageUnmeasured}
	br, ok := runner.(BaselineRunner)
	if !ok {
		return &PreflightReport{Status: PreflightNA, Note: "执行后端 " + sandboxLabel(runner) + " 不支持基线（无独立判分命令）"}, leak, nil
	}
	if PreflightEscaped() {
		return &PreflightReport{Status: PreflightSkipped, Note: PreflightDisableEnv + "=skip——基线未校验，证据封顶 Weak"}, leak, nil
	}
	rep := &PreflightReport{}
	leakPassed, leakTotal := 0, 0
	for _, task := range manifest.Tasks {
		dn, applicable, err := br.RunBaseline(ctx, spec, task, ArmDoNothing)
		if err == nil {
			err = inconclusive(task.ID, ArmDoNothing, dn)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("evalkit: 任务 %s 的 do-nothing 基线执行失败: %w", task.ID, err)
		}
		if !applicable {
			continue
		}
		rep.Checked++
		if dn.Pass {
			rep.DoNothingPassed = append(rep.DoNothingPassed, task.ID)
		}
		if task.OracleCmd == "" {
			rep.OracleMissing = append(rep.OracleMissing, task.ID)
		} else {
			or, _, err := br.RunBaseline(ctx, spec, task, ArmOracle)
			if err == nil {
				err = inconclusive(task.ID, ArmOracle, or)
			}
			if err != nil {
				return nil, nil, fmt.Errorf("evalkit: 任务 %s 的 oracle 基线执行失败: %w", task.ID, err)
			}
			if !or.Pass {
				rep.OracleFailed = append(rep.OracleFailed, task.ID)
			}
		}
		lk, lkApplicable, err := br.RunBaseline(ctx, spec, task, ArmLeakage)
		if err != nil {
			return nil, nil, fmt.Errorf("evalkit: 任务 %s 的 leakage 基线执行失败: %w", task.ID, err)
		}
		if lkApplicable && lk.BudgetCut {
			leak.TimedOut++
		}
		if lkApplicable && !lk.BudgetCut { // 超时的 leakage 臂无法判定，不进分母
			leakTotal++
			if lk.Pass {
				leakPassed++
				leak.PassedTasks = append(leak.PassedTasks, task.ID)
			}
		}
	}
	if rep.Checked == 0 {
		rep.Status = PreflightNA
		rep.Note = "manifest 无带 test_cmd 的判分任务——基线无定义"
		return rep, leak, nil
	}
	if len(rep.OracleFailed)+len(rep.OracleMissing)+len(rep.DoNothingPassed) > 0 {
		return nil, nil, &PreflightError{Report: rep}
	}
	rep.Status = PreflightPassed
	if leakTotal > 0 {
		leak.Status = LeakageMeasured
		leak.Rate = newRateValue(&MetricDef{ID: "e2e_leakage_pass", MinSamples: 1}, leakPassed, leakTotal)
	}
	return rep, leak, nil
}

// inconclusive turns a budget-cut baseline into an execution error: a timed-out
// oracle is not proof the environment is broken, and a timed-out do-nothing is
// not proof the grader rejects — neither may become a verdict (exit 1 with a
// hint, never exit 2).
//
// inconclusive 把预算截断的基线转成执行错误：oracle 超时不能证明环境坏了，
// do-nothing 超时也不能证明判分会拒绝——两者都不得成为裁决（exit 1 并提示，
// 绝不 exit 2）。
func inconclusive(taskID string, arm BaselineArm, res TaskResult) error {
	if !res.BudgetCut {
		return nil
	}
	return fmt.Errorf("%s 臂超出单任务墙钟预算，无法判定（任务 %s；调大 --wallclock 后重跑）", arm, taskID)
}

// buildReviewQueue lists every leakage-pass task, then fills up to
// reviewQueueSize with the highest pass-rate tasks (ties: fewer tokens first —
// a suspiciously cheap success is the cheat signature — then task ID). The
// top-score fill skips tasks that never passed the main run (leakage-pass
// tasks are queued regardless).
//
// buildReviewQueue 先列出全部 leakage 通过的任务，再按通过率降序补足到
// reviewQueueSize（同分时 token 少者优先——便宜得可疑的成功是作弊特征——再按
// 任务 ID）。按高分补足时，主评测从未通过的任务不入列（leakage 通过的任务不受
// 此限）。
func buildReviewQueue(attempts []Attempt, leak *LeakageSummary) []ReviewItem {
	type agg struct {
		pass, n, tokens int
	}
	byTask := map[string]*agg{}
	for _, a := range attempts {
		g := byTask[a.TaskID]
		if g == nil {
			g = &agg{}
			byTask[a.TaskID] = g
		}
		g.n++
		g.tokens += a.Tokens
		if a.Pass {
			g.pass++
		}
	}
	item := func(id, reason string) ReviewItem {
		g := byTask[id]
		if g == nil || g.n == 0 {
			return ReviewItem{TaskID: id, Reason: reason}
		}
		return ReviewItem{TaskID: id, PassRate: float64(g.pass) / float64(g.n), Tokens: g.tokens, Reason: reason}
	}
	var out []ReviewItem
	queued := map[string]bool{}
	if leak != nil {
		ids := append([]string(nil), leak.PassedTasks...)
		sort.Strings(ids)
		for _, id := range ids {
			out = append(out, item(id, ReviewLeakagePass))
			queued[id] = true
		}
	}
	var rest []ReviewItem
	for id, g := range byTask {
		if queued[id] || g.pass == 0 {
			continue
		}
		rest = append(rest, item(id, ReviewTopScore))
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].PassRate != rest[j].PassRate {
			return rest[i].PassRate > rest[j].PassRate
		}
		if rest[i].Tokens != rest[j].Tokens {
			return rest[i].Tokens < rest[j].Tokens
		}
		return rest[i].TaskID < rest[j].TaskID
	})
	for _, r := range rest {
		if len(out) >= reviewQueueSize {
			break
		}
		out = append(out, r)
	}
	return out
}

// RecordPreflightBlock lands the eval-preflight refusal row (Passed=false).
//
// RecordPreflightBlock 落 eval-preflight 拒绝行（Passed=false）。
func RecordPreflightBlock(repoRoot string, spec RunSpec, pe *PreflightError) {
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckEvalPreflight,
		Passed:  false,
		Checked: true,
		Level:   checklog.LevelBlocked,
		Detail: fmt.Sprintf(`preflight 违例: benchmark %s@%s profile=%s model=%s checked %d oracle_failed %d oracle_missing %d do_nothing_passed %d`,
			spec.Benchmark, spec.Split, spec.Profile, spec.Model, pe.Report.Checked,
			len(pe.Report.OracleFailed), len(pe.Report.OracleMissing), len(pe.Report.DoNothingPassed)),
	})
}

// RecordPreflightEscape lands the escape-hatch row for FORGE_EVAL_PREFLIGHT=skip.
//
// RecordPreflightEscape 为 FORGE_EVAL_PREFLIGHT=skip 落 escape-hatch 行。
func RecordPreflightEscape(repoRoot string) {
	_ = checklog.Record(repoRoot, checklog.EscapeHatchEntry(string(checklog.CheckEvalPreflight), checklog.EscapeReasonEnv, "env",
		PreflightDisableEnv+"=skip：Track A preflight（oracle/do-nothing）未校验，本次分数证据封顶 Weak"))
}
