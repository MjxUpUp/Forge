package taskpipeline

import (
	"fmt"
	"github.com/MjxUpUp/Forge/internal/ghostpath"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/specroot"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// next.go —— nextDecision 纯决策函数：给定分支、脏树、活跃任务状态，返回恰好一条下一步
// 命令 + 理由（vNext P1-2 单命令语义）。从 internal/cli/next.go 下沉（设计 B：gate/status/
// complete 的输出点也要挂 next 行，clitask 不能反向 import cli——决策逻辑进 taskpipeline，
// 两处消费同一真相源）。`forge next` CLI 走 NextDecisionCtx，clitask 的输出点走 NextHint。
//
// docs/design/harness-fixes-a-g-2026-09.md B：门禁输出是唯一被确定性阅读的界面（两机实测
// `forge next` 主动调用 0 次、gate 运行 139/113 次且拦截后 100% 重跑）——next 行挂在门禁
// 输出末尾，把「问下一步」变成「读门禁输出时顺手看到下一步」。

// NextResult 是 NextDecision 的输出契约。Next 恒非空（无事可做时回落 status）。
type NextResult struct {
	Next   string         `json:"next"`
	Reason string         `json:"reason"`
	State  map[string]any `json:"state"`
}

// NextDecision 是纯决策函数（单测锚点）：给定分支、脏树、活跃任务状态，返回恰好一条命令。
// nextCmdVerifyAcceptance 是 verify-acceptance 建议命令的单一真相源——生产
// （NextDecision）与 selfcheck 提示的锚（NextHint）同用一字面量，改名时两处
// 不会静默漂移（增量复审 P3-4）。
const nextCmdVerifyAcceptance = "forge task verify-acceptance"

// NextContext 是 next 决策需要的仓库上下文。工作区干净且声明了 spec 时直接建议
// task start（change 可后补），不再停在 forge status 空转。SpecDeclared/OpenChanges 让「入口路由」
// 成为状态而不是文案：声明了 spec/forge.yaml 的仓库，脏树且没有打开 change 时先立
// change，而不是直接 task start（JustForMe 9 个任务 0 个 change 的直接原因）。
type NextContext struct {
	Branch       string
	Dirty        bool
	BehindRemote int
	// SpecDeclared 表示仓库有 spec/forge.yaml；未声明时所有新规则不生效。
	SpecDeclared bool
	// OpenChanges 是未归档 change 的 id（按字母序）。
	OpenChanges []string
	// ReadyChanges 是其中阶段已到 ready 的 id；ReadyChanges 非空时下一步是
	// 用 task start --change 开工，而不是再补 change 产物。
	ReadyChanges []string
}

// NextRepoContext 从仓库实况推导 next 的上下文：spec 声明 + 未归档 change 及其阶段。
//
// 之所以放在 taskpipeline 而不是 CLI：NextHint 在 task gate/complete 的输出面也要用同一
// 套上下文（否则 spec 仓库在「下一步」提示里仍会退回 forge status，复现 dogfood 死循环）。
func NextRepoContext(root, branch string, dirty bool, behindRemote int) NextContext {
	ctx := NextContext{Branch: branch, Dirty: dirty, BehindRemote: behindRemote}
	ctx.SpecDeclared = specroot.Declared(root)
	if !ctx.SpecDeclared {
		return ctx
	}
	changes, err := change.List(root)
	if err != nil {
		return ctx
	}
	for _, c := range changes {
		if c.Archive {
			continue
		}
		ctx.OpenChanges = append(ctx.OpenChanges, c.ID)
		if ph, _, err := change.ComputePhase(root, c.ID); err == nil && ph == change.PhaseReady && c.HasGoal {
			// 老 change（无 goal.md）不能 --change：钉契约需要 goal 必填三段。
			ctx.ReadyChanges = append(ctx.ReadyChanges, c.ID)
		}
	}
	sort.Strings(ctx.OpenChanges)
	sort.Strings(ctx.ReadyChanges)
	return ctx
}

// NextDecision 保留旧签名（纯任务上下文）：调用方没有 spec 信息时应走这里。
func NextDecision(branch string, dirty bool, behindRemote int, st *TaskState) NextResult {
	return NextDecisionCtx(NextContext{Branch: branch, Dirty: dirty, BehindRemote: behindRemote}, st)
}

// NextDecisionCtx 是带仓库上下文的决策函数；门禁顺序与真实链严格一致：
// implement →（验收未实跑则 verify-acceptance）→ gate task-verify → review pass →
// gate task-complete → task complete。每条 Next 恰一条命令（无 && 复合）。
func NextDecisionCtx(ctx NextContext, st *TaskState) NextResult {
	branch, dirty, behindRemote := ctx.Branch, ctx.Dirty, ctx.BehindRemote
	gates := map[string]bool{}
	for _, g := range nextGateHistory(st) {
		gates[g] = true
	}
	state := map[string]any{
		"branch":        branch,
		"dirty":         dirty,
		"behind_remote": behindRemote,
		"spec_declared": ctx.SpecDeclared,
		"open_changes":  len(ctx.OpenChanges),
		"ready_changes": len(ctx.ReadyChanges),
		"active_task":   nextTaskRef(st),
		"gates_passed":  nextGateHistory(st),
		"review_passed": nextReviewPassed(st),
	}

	// 已完结任务（status --ref 的展示路径会把 CompletedAt!=nil 的状态喂进来）：完成态不
	// 建议「再跑 complete」，与 ActiveTaskState 返回 nil 的完成态同分支处理。
	if st != nil && st.CompletedAt != nil {
		st = nil
	}
	// 无活跃任务（ActiveTaskState 对已完成任务返回 nil——完成态经此分支）：归属问题优先。
	if st == nil {
		if dirty {
			// 入口路由：声明了 spec 的仓库先立 change（goal+acceptance），
			// 有打开的 change 就先推进它的阶段；任务本身始终是一等入口（不带 --change 也能开工）。
			if len(ctx.ReadyChanges) > 0 {
				return NextResult{
					Next:   "forge task start --ref feat/" + ctx.ReadyChanges[0] + " --branch --change " + ctx.ReadyChanges[0],
					Reason: "工作区有未归属变更，且已有 ready 的 change——用 task start --change 开工（任务是一等入口；change 可以事后再补）",
					State:  state,
				}
			}
			if len(ctx.OpenChanges) > 0 {
				return NextResult{
					Next:   "forge change phase " + ctx.OpenChanges[0],
					Reason: "工作区有未归属变更，且已有未完成的 change——先看它的阶段并补齐产物：没有 goal.md 的老 change 可补 goal.md，其余情况可以先不带 --change 直接 task start 开工（change 事后再补）",
					State:  state,
				}
			}
			if ctx.SpecDeclared {
				return NextResult{
					Next:   "forge change new <id>",
					Reason: "工作区有未归属变更——这个仓库用 spec 流程：可以把它立成 change（goal+acceptance）再开工，change 也能事后再补；不立 change 就直接 task start 开工",
					State:  state,
				}
			}
			return NextResult{
				Next:   `forge task start --ref <ref> --branch --title <title>`,
				Reason: "工作区有未归属变更而无活跃任务——先建任务收编（刻意的一次性小改可改走 forge task wild \"<说明>\" 申报）",
				State:  state,
			}
		}
		// 干净树但分支落后 upstream：先同步再开工——在旧基线上叠改会放大合并冲突与
		// 「本机过、远端挂」的漂移面（1.73.2 旧快照重建事故的同型风险）。
		if behindRemote > 0 {
			return NextResult{
				Next:   "git pull --ff-only",
				Reason: fmt.Sprintf("当前分支落后远端 %d 个提交——先同步再开工，避免在旧基线上叠改", behindRemote),
				State:  state,
			}
		}
		if ctx.SpecDeclared {
			return NextResult{
				Next:   `forge task start --ref <ref> --branch --title <title>`,
				Reason: "工作区干净且没有活跃任务——直接开新任务（一句话小修加 --kind task；想留可评审记录也可以先 forge change new <id>，change 可后补；不要停在 status 上空转）",
				State:  state,
			}
		}
		return NextResult{
			Next:   "forge status",
			Reason: "无活跃任务且工作区干净——查看项目状态或认领任务（forge task mine）",
			State:  state,
		}
	}

	// 活跃任务：按真实门禁链给恰好一条。
	switch {
	case !gates[GateImplement]:
		return NextResult{Next: "forge task gate task-implement", Reason: "实现未确认（有提交即可过）", State: state}
	case st != nil && !st.HasAcceptance() && !st.IsGeneric():
		// 考卷缺位（oracle-pipeline L1）：complete 硬前置零验收登记——先补考卷再谈
		// 实跑。verify-acceptance 在有 conventions 档案时会自动登记默认套件。
		return NextResult{Next: nextCmdVerifyAcceptance, Reason: "未登记验收标准（complete 硬前置）——先跑 verify-acceptance（有 conventions 档案时自动登记默认套件）或 forge task accept \"run :: expected\" 补登", State: state}
	case nextAcceptancePending(st):
		return NextResult{Next: nextCmdVerifyAcceptance, Reason: "验收标准尚未实跑回扣——先实跑（AcceptedHeadCommit 为空的标准待跑）", State: state}
	case !nextReviewPassed(st):
		// 评审连续性：本轮复审必须由同一个 code-review 子 agent 承接——把上次的
		// id 直接写进建议命令，避免「每轮换个新 id 重扫、旧问题悄悄消失」。
		cmd := "forge review pass"
		reason := "验证已过而审查未过——派只读子代理审查当前 diff 后标记（task-complete 门禁硬前置）"
		if st != nil {
			if id := reviewOwner(st); id != "" {
				cmd += " --reviewer " + strconv.Quote(id)
				reason += "；复用上一轮的评审者 " + id + "（换人用 --rotate-reviewer --reason）"
			}
		}
		return NextResult{Next: cmd, Reason: reason, State: state}
	case !gates[GateComplete]:
		return NextResult{Next: "forge task gate task-complete", Reason: "实现/审查齐备——过完成任务门（forge task complete 要求两道门禁全过）", State: state}
	default:
		return NextResult{Next: "forge task complete", Reason: "两道门禁与审查齐备——完结并评分（此后手工合并分支）", State: state}
	}
}

// reviewOwner 取任务已绑定的 code-review 子 agent id（空表示还没绑）。
func reviewOwner(st *tasktypes.TaskState) string {
	if st == nil || len(st.Agents) == 0 {
		return ""
	}
	return st.Agents["code-review"]
}

// NextHint derives the hint for a task context and records one next-hint checklog row (design B:
// B1 adoption is measured as suggested-command-executed-within-10min from these rows).
//
// NextHint 推导任务上下文的下一步并落一条 next-hint checklog 行（设计 B：B1 采纳率 =
// next-hint 行的建议命令在 10 分钟内被执行的比例）。branch/dirty 经 git 实算；记录失败只打
// stderr 不影响调用方输出。
func NextHint(root string, st *TaskState) NextResult {
	branch, dirty := GitBranchDirty(root)
	var res NextResult
	if st == nil {
		// 只有「无活跃任务」分支读 change 目录；活跃任务的门禁链不需要（避免每次 gate 输出多读一圈盘）。
		res = NextDecisionCtx(NextRepoContext(root, branch, dirty, GitBehindRemote(root)), nil)
	} else {
		res = NextDecision(branch, dirty, GitBehindRemote(root), st)
	}
	entry := &checklog.Entry{
		Check:     checklog.CheckNextHint,
		Passed:    true,
		Checked:   true,
		Level:     checklog.LevelAdvisory,
		TaskRef:   nextTaskRef(st),
		SessionID: CurrentSessionID(),
		Detail:    fmt.Sprintf("next-hint: %s（%s）", res.Next, res.Reason),
		Meta:      map[string]string{checklog.MetaKeySuggested: res.Next},
	}
	recordAudit(root, entry)
	return res
}

// GitBranchDirty returns the current branch and dirty flag (shared input of every NextHint call site).
//
// GitBranchDirty 返回当前分支与脏树标志（NextHint 各调用点的共同输入）。
func GitBranchDirty(root string) (string, bool) {
	branch := ""
	if out, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	dirty := false
	if out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" && !ghostpath.LineHasReservedPath(root, line) {
				dirty = true
				break
			}
		}
	}
	return branch, dirty
}

// GitBehindRemote returns how many commits HEAD is behind its upstream (0 when there is no
// upstream / no git — fail-open: no probe, no sync suggestion).
//
// GitBehindRemote 返回 HEAD 落后其 upstream 的提交数（无 upstream/无 git 时为 0——
// fail-open：探测不到就不建议同步）。
func GitBehindRemote(root string) int {
	out, err := exec.Command("git", "-C", root, "rev-list", "--count", "HEAD..@{upstream}").Output()
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func nextGateHistory(st *TaskState) []string {
	if st == nil {
		return nil
	}
	out := make([]string, 0, len(st.History))
	for _, h := range st.History {
		out = append(out, h.Gate)
	}
	return out
}

func nextTaskRef(st *TaskState) string {
	if st == nil {
		return ""
	}
	return st.TaskRef
}

func nextReviewPassed(st *TaskState) bool { return st != nil && st.ReviewPassed }

// nextAcceptancePending 报告是否有验收标准尚未实跑（AcceptedHeadCommit 为空）。
func nextAcceptancePending(st *TaskState) bool {
	if st == nil {
		return false
	}
	for _, a := range st.Acceptance {
		if a.AcceptedHeadCommit == "" {
			return true
		}
	}
	return false
}
