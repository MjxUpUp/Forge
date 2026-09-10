package taskpipeline

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// next.go —— nextDecision 纯决策函数：给定分支、脏树、活跃任务状态，返回恰好一条下一步
// 命令 + 理由（vNext P1-2 单命令语义）。从 internal/cli/next.go 下沉（设计 B：gate/status/
// complete 的输出点也要挂 next 行，clitask 不能反向 import cli——决策逻辑进 taskpipeline，
// 两处消费同一真相源）。`forge next` CLI 与 clitask 的三个输出点都经 NextDecision。
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
// 门禁顺序与真实链严格一致：implement →（验收未实跑则 verify-acceptance）→ gate task-verify
// → review pass → gate task-complete → task complete。每条 Next 恰一条命令（无 && 复合）。
func NextDecision(branch string, dirty bool, st *TaskState) NextResult {
	gates := map[string]bool{}
	for _, g := range nextGateHistory(st) {
		gates[g] = true
	}
	state := map[string]any{
		"branch":        branch,
		"dirty":         dirty,
		"active_task":   nextTaskRef(st),
		"gates_passed":  nextGateHistory(st),
		"review_passed": nextReviewPassed(st),
	}

	// 无活跃任务（ActiveTaskState 对已完成任务返回 nil——完成态经此分支）：归属问题优先。
	if st == nil {
		if dirty {
			return NextResult{
				Next:   `forge task start --ref <ref> --branch --title <title>`,
				Reason: "工作区有未归属变更而无活跃任务——先建任务收编（刻意的一次性小改可改走 forge task wild \"<说明>\" 申报）",
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
	case nextAcceptancePending(st):
		return NextResult{Next: "forge task verify-acceptance", Reason: "验收标准尚未实跑回扣——先实跑（AcceptedHeadCommit 为空的标准待跑）", State: state}
	case !gates[GateVerify]:
		return NextResult{Next: "forge task gate task-verify", Reason: "验收已实跑——过验证门", State: state}
	case !nextReviewPassed(st):
		return NextResult{Next: "forge review pass", Reason: "验证已过而审查未过——派只读子代理审查当前 diff 后标记（task-complete 门禁硬前置）", State: state}
	case !gates[GateComplete]:
		return NextResult{Next: "forge task gate task-complete", Reason: "实现/验证/审查齐备——过第三道门（forge task complete 要求三门禁全过）", State: state}
	default:
		return NextResult{Next: "forge task complete", Reason: "三门禁与审查齐备——完结并评分（此后手工合并分支）", State: state}
	}
}

// NextHint derives the hint for a task context and records one next-hint checklog row (design B:
// B1 adoption is measured as suggested-command-executed-within-10min from these rows).
//
// NextHint 推导任务上下文的下一步并落一条 next-hint checklog 行（设计 B：B1 采纳率 =
// next-hint 行的建议命令在 10 分钟内被执行的比例）。branch/dirty 经 git 实算；记录失败只打
// stderr 不影响调用方输出。
func NextHint(root string, st *TaskState) NextResult {
	branch := ""
	if out, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	dirty := false
	if out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output(); err == nil {
		dirty = strings.TrimSpace(string(out)) != ""
	}
	res := NextDecision(branch, dirty, st)
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
		dirty = strings.TrimSpace(string(out)) != ""
	}
	return branch, dirty
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
