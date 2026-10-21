package taskpipeline

// loopverdict.go —— 循环判词（loop-contract B1-2）：停止条件的单一真相源。
// 外层循环（Claude Code /loop、定时任务、Codex automation）每一轮从这里拿一个
// 可枚举的判词 escalate / done / continue，每条原因附带解除方式。
// EvaluateLoopVerdict 是纯函数（只看传入的 LoopInputs）；ComputeLoopVerdict 负责
// 从 git / checklog / toollog / hazard 事件流收集输入——只读，不写盘、不落审计
// （status 与 resume 都会调用它，判词不能有副作用）。
//
// loopverdict.go — the loop verdict (loop-contract B1-2): single source of
// truth for stop conditions. Each turn, the outer loop reads one enumerable
// verdict (escalate / done / continue) with a resolve command per reason.
// EvaluateLoopVerdict is pure over LoopInputs; ComputeLoopVerdict gathers the
// inputs from git / checklog / toollog / hazard events — read-only, no
// persistence, no audit row (status and resume call it; a verdict must have no
// side effects).

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// Loop verdict values, highest priority first.
//
// 判词取值，优先级从高到低。
const (
	LoopVerdictEscalate = "escalate"
	LoopVerdictDone     = "done"
	LoopVerdictContinue = "continue"
)

// Loop reason codes (closed enum; every code maps to one Resolve in loopResolve).
//
// 判词原因 Code（封闭枚举；每个 Code 在 loopResolve 里恰有一条解除方式）。
const (
	LoopCodeAcceptanceRecurrence = "acceptance-recurrence"
	LoopCodeLoopExhausted        = "loop-exhausted"
	LoopCodeHazardPending        = "hazard-pending"
	LoopCodeInputRequired        = "input-required"
	LoopCodeBudgetFailedVerifies = "budget-failed-verifies"
	LoopCodeBudgetTokens         = "budget-tokens"
	LoopCodeBudgetChangedFiles   = "budget-changed-files"
	LoopCodeNoProgress           = "no-progress"
	LoopCodeGateRepeatBlocked    = "gate-repeat-blocked"
)

// gateRepeatBlockedThreshold: consecutive BLOCKED results of one gate since the
// last reset that escalate.
//
// gateRepeatBlockedThreshold：自上次重置起同一门禁连续 BLOCKED 几次即升级。
const gateRepeatBlockedThreshold = 2

// loopResetCmd is the single human exit every window-based reason resolves to.
//
// loopResetCmd 是所有窗口类原因共用的唯一人工出口。
const loopResetCmd = `forge task finding --reset-loop --note "<新方向>"`

// loopResolve maps each code to its resolve command (human-only ones say so).
//
// loopResolve 把每个 Code 映射到解除命令（需人工的注明）。
var loopResolve = map[string]string{
	LoopCodeAcceptanceRecurrence: "需人工：" + loopResetCmd,
	LoopCodeLoopExhausted:        "需人工：" + loopResetCmd,
	LoopCodeHazardPending:        "需人工：forge hazard confirm --last",
	LoopCodeInputRequired:        "需人工：forge task answer --ref <ref> --content <答复>",
	LoopCodeBudgetFailedVerifies: "需人工：" + loopResetCmd + "，或调高 protocol.yml loop.max_failed_verifies",
	LoopCodeBudgetTokens:         "需人工：" + loopResetCmd + "，或调高 protocol.yml loop.token_budget",
	LoopCodeBudgetChangedFiles:   "需人工：拆分任务，或调高 protocol.yml loop.max_changed_files",
	LoopCodeNoProgress:           "需人工：" + loopResetCmd,
	LoopCodeGateRepeatBlocked:    "需人工：" + loopResetCmd,
}

// LoopReason is one reason behind an escalate verdict.
//
// LoopReason 是 escalate 判词的一条原因。
type LoopReason struct {
	Code    string `json:"code"`
	Detail  string `json:"detail"`
	Resolve string `json:"resolve"`
}

// LoopVerdict is what the outer loop reads each turn.
//
// LoopVerdict 是外层循环每轮读取的判词。
type LoopVerdict struct {
	Verdict string       `json:"verdict"`
	Reasons []LoopReason `json:"reasons,omitempty"`
	// Next is NextDecision's single next command — the closing sequence after
	// done, or the gate to push while continuing.
	//
	// Next 是 NextDecision 给出的下一条确切命令——done 之后的收尾序列，或 continue
	// 时该推的门禁。
	Next string `json:"next"`
}

// LoopInputs is everything EvaluateLoopVerdict looks at — plain data so the
// verdict is testable without disk or git.
//
// LoopInputs 是 EvaluateLoopVerdict 看的全部输入——纯数据，判词可以不碰盘和 git
// 就测。
type LoopInputs struct {
	State       *TaskState
	Cfg         LoopConfig
	Recurrences []AcceptanceRecurrence
	// LoopExhausted: persisted marker or recomputed exhaustion.
	LoopExhausted bool
	// HazardPendingOwn: unconfirmed hazard intercepts owned by this task's session.
	HazardPendingOwn int
	// FailedVerifies: failed verify-acceptance runs since the last reset.
	FailedVerifies int
	// PassCounts: per verify-acceptance run since the last reset, the passed
	// count (oldest first); runs without the Meta key are omitted.
	PassCounts []int
	// TokensUsed: estimated tokens since the last reset.
	TokensUsed int
	// ChangedFiles: len(TaskChangedFiles).
	ChangedFiles int
	// AcceptanceFresh: acceptanceFreshCore ok (verify snapshot matches current source).
	AcceptanceFresh bool
	Branch          string
	Dirty           bool
	BehindRemote    int
}

// EvaluateLoopVerdict is the pure verdict: escalate when any reason holds;
// done when no reason holds, at least one acceptance is registered, every
// criterion passed and the snapshot is fresh; continue otherwise. Next always
// comes from NextDecision (never NextHint, which records a row).
//
// EvaluateLoopVerdict 是纯判词：任一原因成立 → escalate；无原因、至少登记一条
// 验收、每条都通过且快照新鲜 → done；其余 → continue。Next 恒取自
// NextDecision（绝不调会落行的 NextHint）。
func EvaluateLoopVerdict(in LoopInputs) LoopVerdict {
	var reasons []LoopReason
	add := func(code, detail string) {
		reasons = append(reasons, LoopReason{Code: code, Detail: detail, Resolve: loopResolve[code]})
	}
	for _, r := range in.Recurrences {
		add(LoopCodeAcceptanceRecurrence, fmt.Sprintf("验收 #%d（%s）改动后同因失败 ×%d（指纹 %s）", r.CriterionIdx+1, r.Run, r.Count, r.Fingerprint))
	}
	if in.LoopExhausted {
		detail := "审查回环耗尽"
		if in.State != nil && in.State.LoopExhausted != nil {
			detail += "（" + in.State.LoopExhausted.Reason + "）"
		}
		add(LoopCodeLoopExhausted, detail)
	}
	if in.HazardPendingOwn > 0 {
		add(LoopCodeHazardPending, fmt.Sprintf("%d 个高危拦截待人工确认", in.HazardPendingOwn))
	}
	if in.State != nil && in.State.Assignment != nil && in.State.Assignment.Status == tasktypes.AssignInputRequired {
		q := strings.TrimSpace(in.State.Assignment.LastQuestion)
		if q == "" {
			q = "（未记录问题）"
		}
		add(LoopCodeInputRequired, "任务在等人回答: "+q)
	}
	if in.Cfg.MaxFailedVerifies > 0 && in.FailedVerifies >= in.Cfg.MaxFailedVerifies {
		add(LoopCodeBudgetFailedVerifies, fmt.Sprintf("verify-acceptance 已失败 %d 次（预算 %d）", in.FailedVerifies, in.Cfg.MaxFailedVerifies))
	}
	if in.Cfg.TokenBudget > 0 && in.TokensUsed >= in.Cfg.TokenBudget {
		add(LoopCodeBudgetTokens, fmt.Sprintf("估算 token %d（预算 %d）", in.TokensUsed, in.Cfg.TokenBudget))
	}
	if in.Cfg.MaxChangedFiles > 0 && in.ChangedFiles >= in.Cfg.MaxChangedFiles {
		add(LoopCodeBudgetChangedFiles, fmt.Sprintf("改动 %d 个文件（预算 %d）", in.ChangedFiles, in.Cfg.MaxChangedFiles))
	}
	if k := in.Cfg.MaxVerifiesWithoutProgress; k > 0 && noProgressRuns(in.PassCounts) >= k {
		add(LoopCodeNoProgress, fmt.Sprintf("连续 %d 次 verify-acceptance 通过条数未增加", k))
	}
	if gate, n := repeatBlockedGate(in.State); n >= gateRepeatBlockedThreshold {
		add(LoopCodeGateRepeatBlocked, fmt.Sprintf("门禁 %s 连续 %d 次 BLOCKED", gate, n))
	}

	v := LoopVerdict{Reasons: reasons, Next: NextDecision(in.Branch, in.Dirty, in.BehindRemote, in.State).Next}
	switch {
	case len(reasons) > 0:
		v.Verdict = LoopVerdictEscalate
	case acceptanceConverged(in.State) && in.AcceptanceFresh:
		v.Verdict = LoopVerdictDone
	default:
		v.Verdict = LoopVerdictContinue
	}
	return v
}

// acceptanceConverged: at least one criterion registered and every one Passed.
// Zero criteria never converge (an empty exam cannot be "all passed").
//
// acceptanceConverged：至少登记一条验收且每条都 Passed。零条验收永不收敛
// （空考卷不算"全过"）。
func acceptanceConverged(st *TaskState) bool {
	if st == nil || len(st.Acceptance) == 0 {
		return false
	}
	for _, c := range st.Acceptance {
		if !c.Passed {
			return false
		}
	}
	return true
}

// noProgressRuns counts, from the latest run backwards, how many consecutive
// runs did not raise the passed count over the run before them.
//
// noProgressRuns 从最新一次往回数，连续多少次的通过条数没有比前一次增加。
func noProgressRuns(passCounts []int) int {
	n := 0
	for i := len(passCounts) - 1; i >= 1; i-- {
		if passCounts[i] > passCounts[i-1] {
			break
		}
		n++
	}
	return n
}

// repeatBlockedGate returns the gate with the longest trailing run of
// Passed=false results since the last reset, and that run's length. Two
// kinds of rows are skipped: Silent ones (the Stop hook's advisory
// task-verify fires every turn, the agent never pushed that gate) and
// task-implement (its BLOCKED is the normal "commit first" rhythm — the
// 2026-10-20 review scanned ~/.forge/projects/*/tasks/*.json history[]: 6 of
// 70 tasks hit it twice and all completed).
//
// repeatBlockedGate 返回自上次重置起尾部连续 Passed=false 最长的门禁及其长度。
// 两类行跳过：Silent 行（Stop hook 的 advisory task-verify 每轮都跑，agent 并没有
// 推过那道门）与 task-implement（它的 BLOCKED 是"先提交"的正常节奏——2026-10-20
// 评审扫本机 ~/.forge/projects/*/tasks/*.json 的 history[]：70 个任务里 6 个连续
// 两次，全部正常完成）。
func repeatBlockedGate(st *TaskState) (string, int) {
	if st == nil {
		return "", 0
	}
	streak := map[string]int{}
	for _, h := range st.History {
		if st.LoopResetAt != nil && h.CompletedAt.Before(*st.LoopResetAt) {
			continue
		}
		if h.Silent || h.Gate == GateImplement {
			continue
		}
		if h.Passed {
			streak[h.Gate] = 0
		} else {
			streak[h.Gate]++
		}
	}
	bestGate, best := "", 0
	for _, g := range tasktypes.DefaultGates() {
		if n := streak[g.ID]; n > best {
			bestGate, best = g.ID, n
		}
	}
	return bestGate, best
}

// ComputeLoopVerdict gathers LoopInputs from the project ledgers (read-only)
// and evaluates. Any ledger read failure degrades that input to its zero
// value — a verdict never fails, and never writes.
//
// ComputeLoopVerdict 从项目账本（只读）收集 LoopInputs 并判定。任一账本读失败即
// 把该项输入退化为零值——判词不报错，也绝不写入。
func ComputeLoopVerdict(root string, state *TaskState) LoopVerdict {
	if state == nil {
		branch, dirty := GitBranchDirty(root)
		return EvaluateLoopVerdict(LoopInputs{Cfg: LoadLoopConfig(root), Branch: branch, Dirty: dirty, BehindRemote: GitBehindRemote(root)})
	}
	cfg := LoadLoopConfig(root)
	in := LoopInputs{
		State:         state,
		Cfg:           cfg,
		Recurrences:   DetectAcceptanceRecurrences(cfg, state.Acceptance),
		LoopExhausted: state.LoopExhausted != nil || evaluateLoopExhaustion(root, state) != nil,
		ChangedFiles:  len(TaskChangedFiles(root, state)),
	}
	if p, err := forgedata.ProjectFor(root); err == nil {
		in.HazardPendingOwn = hazard.PendingBlocksFor(p, taskSessionOwner(state, CurrentSessionID())).Own
	}
	var since time.Time
	if state.LoopResetAt != nil {
		since = *state.LoopResetAt
	}
	if entries, err := checklog.LoadForTask(root, state.TaskRef); err == nil {
		for _, e := range entries {
			if e.Check != CheckNameAcceptance || e.RecordedAt.Before(since) {
				continue
			}
			if !e.Passed {
				in.FailedVerifies++
			}
			if n, err := strconv.Atoi(e.Meta[MetaKeyAcceptancePassed]); err == nil {
				in.PassCounts = append(in.PassCounts, n)
			}
		}
	}
	if calls, err := toolusage.LoadForTaskAll(root, state.TaskRef); err == nil {
		for _, c := range calls {
			if !c.Timestamp.Before(since) {
				in.TokensUsed += c.EstTokens
			}
		}
	}
	in.AcceptanceFresh, _ = acceptanceFreshCore(root, state)
	in.Branch, in.Dirty = GitBranchDirty(root)
	in.BehindRemote = GitBehindRemote(root)
	return EvaluateLoopVerdict(in)
}

// FormatLoopVerdictLine renders the one-line form printed by verify-acceptance
// and shown in the resume header: `loop: escalate（code, code）→ next: cmd`.
//
// FormatLoopVerdictLine 渲染 verify-acceptance 末行与 resume 头部用的单行形态：
// `loop: escalate（code, code）→ next: cmd`。
func FormatLoopVerdictLine(v LoopVerdict) string {
	s := "loop: " + v.Verdict
	if len(v.Reasons) > 0 {
		codes := make([]string, 0, len(v.Reasons))
		for _, r := range v.Reasons {
			codes = append(codes, r.Code)
		}
		s += "（" + strings.Join(codes, ", ") + "）"
	}
	if v.Next != "" {
		s += " → next: " + v.Next
	}
	return s
}
