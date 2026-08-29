package taskpipeline

// executor_check_common.go — ExecuteTaskGate 拆分（refactor/executor-pipeline 第一步）：
// 全 gate 共享的前置检查（DependsOn 门禁 / 前置 gate 链）与收尾的 agent-claim 落盘。
// 代码体自 executor.go 的 ExecuteTaskGate 原样提取，行为等价——仅变量引用改为参数名。
//
// executor_check_common.go — ExecuteTaskGate decomposition (refactor/executor-pipeline
// step 1): the checks shared by every gate (the DependsOn gate / the prerequisite-gate
// chain) and the tail agent-claim audit. Bodies were extracted verbatim from
// ExecuteTaskGate in executor.go — behavior-equivalent; only variable references
// became parameter names.

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// checkDependsOnGate is the DependsOn gate (design phase 3): a task with unsatisfied upstream
// dependencies cannot pass task-verify or task-complete — the worker could not have
// verified/completed work whose inputs were never delivered. Checked only at verify/complete:
// implement may proceed while an upstream is still in flight (writing code against an expected
// interface is fine; blocking there would needlessly serialize the graph). A missing upstream ref
// counts as pending (not delivered) so an aborted or typo'd dependency cannot be silently
// bypassed. generic tasks already returned earlier in ExecuteTaskGate.
//
// checkDependsOnGate 是 DependsOn 门禁（设计阶段3）：上游依赖未满足的 task 不能过
// task-verify/task-complete——工作方无法验收/完成一个输入从未交付的工作。仅在 verify/complete
// 查：implement 可在上游进行中时推进（针对预期接口写代码无妨，那时阻塞只会无谓串行化依赖图）。
// 缺失的上游 ref 计为 pending（未交付），故 abort/拼错的依赖无法被静默绕过。generic task 已在
// ExecuteTaskGate 上方 return。
func checkDependsOnGate(root string, gateID string, state *TaskState) error {
	if len(state.DependsOn) > 0 && state.CompletedAt == nil &&
		(gateID == `task-verify` || gateID == `task-complete`) {
		pending := PendingDependencies(root, state.DependsOn)
		if len(pending) > 0 {
			// BLOCKED 必落盘（与 skill-decisions / test-coverage 阻断前落盘一致）：否则任务反复卡在
			// 依赖门禁的停滞信号对 score/dashboard/forge trace 不可见，与「从未尝试」无法区分。
			//
			// BLOCKED must hit disk (matching skill-decisions / test-coverage recording before a
			// block): otherwise the signal that a task repeatedly stalls at the dependency gate is
			// invisible to score/dashboard/forge trace, indistinguishable from "never attempted".
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameDependencyGate,
				Passed:  false,
				Checked: true,
				TaskRef: state.TaskRef,
				// 显式标 LevelBlocked：Detail 不以 "BLOCKED: " 起头（是「%s 拒绝：…」），DeriveLevel 不会判
				// 为 blocked，导致这条 HARD 阻断在 score/dashboard/forge trace 里被分桶成普通告警——与「它确实
				// 是硬阻断」不符。caller-set Level 恒优先于 DeriveLevel（store.go Record）。
				Level:  checklog.LevelBlocked,
				Detail: fmt.Sprintf(`%s 拒绝：上游依赖未交付或不存在（%s）`, gateID, strings.Join(pending, `, `)),
			})
			return GateBlocked(`%s 拒绝（HARD stop）：上游 task 未交付或不存在（%s，可能是未创建/已 abort/拼错；key:ref 跨仓依赖还会因目标仓未交付或数据不可读而阻塞）；forge task mine --blocked 查看详情，或先推进上游交付`, gateID, strings.Join(pending, `, `))
		}
	}
	return nil
}

// checkPrerequisiteGates verifies that all earlier gates have passed.
//
// checkPrerequisiteGates 校验前置：所有更早的 gate 必须已通过。
func checkPrerequisiteGates(gateID string, state *TaskState) error {
	gates := DefaultGates()
	for _, g := range gates {
		if g.ID == gateID {
			break
		}
		if !state.gatePassed(g.ID) {
			return GateBlocked("prerequisite gate %q has not passed — complete earlier gates first (HARD stop, not a reminder)", g.ID)
		}
	}
	return nil
}

// recordAgentClaimAudit is the evidence-chain agent-claim data source: an agent advancing a
// non-auto gate 'declares' that stage complete (task-verify = verification claim,
// task-complete = completion claim). Complementary to deterministic hook/gate execution
// checks — EvidenceChain buckets by Source, and the ratio = how much deterministic evidence
// backs a completion claim, surfacing the LLM-judge blind spot of 'agent skips prerequisites
// and still declares done'. Recorded only when the gate actually passes and the task is not
// yet complete (re-checking a completed task does not re-declare).
//
// recordAgentClaimAudit 是证据链 agent-claim 数据源：agent 推进一个非自动 gate 即「声明」该
// 阶段完成（task-verify=验证声明，task-complete=完成声明）。与 deterministic 的 hook/gate
// 实跑检查互补——EvidenceChain 据 Source 分桶，ratio=完成声明背后有多少 deterministic
// 证据支撑，照出「agent 跳过前置就声明完成」的 LLM-judge 盲区。仅在 gate 实际通过且任务
// 未完成时记录（重检 completed 任务不重复声明）。
func recordAgentClaimAudit(root string, gateID string, state *TaskState) {
	switch gateID {
	case "task-verify":
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckTaskVerify,
			Passed:  true,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  `agent-claim: 通过 task-verify gate（agent 自述验证完成）`,
		})
	case "task-complete":
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckTaskComplete,
			Passed:  true,
			Checked: true,
			TaskRef: state.TaskRef,
			Detail:  `agent-claim: 通过 task-complete gate（agent 自述任务完成）`,
		})
	}
}
