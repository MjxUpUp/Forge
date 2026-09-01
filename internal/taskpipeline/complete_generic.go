package taskpipeline

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/worktree"
)

// CompleteGeneric completes a generic-kind task (research/design/pure handoff):
// auto-passes the 3 gates, marks complete, clears the active-task ref, and
// handles L1 binding per checkout shape.
//
// CompleteGeneric 完成 generic kind 任务（调研/设计/纯接续）：自动标 3 道门禁 passed
// （History 完整供 list/dashboard 显示，但不跑任何检查——ExecuteTaskGate 对 generic 秒过）+
// MarkComplete + 清 active-task-ref。不评分、不创建 review——generic 任务的价值在持久化的
// 接续字段，不在代码质量门禁。（2026-09 普查 A1：自 cli/task_gate.go 下沉；harness 提交
// 钩子留在 cli 包装层——会话语义不属于执行器。）
func CompleteGeneric(root string, state *TaskState) error {
	// Design §5 orchestration completion trigger: a generic task that is an orchestrator (has child
	// tasks pointing at it via ParentTaskRef) is "可 complete" when all children are delivered or
	// terminal (failed/canceled). "不全 delivered 不强制 complete" → this is an ADVISORY warn to
	// stderr, never a block: the orchestrator may legitimately synthesize partial results, but the
	// pending children are surfaced so completing with unfinished work is a deliberate choice, not
	// a silent one. A generic task with NO children (ordinary research/design/handoff) is unaffected.
	// ListTaskState failure is non-fatal — completion must not be gated on the readiness probe.
	//
	// 设计 §5 编排完成触发：作为编排器的 generic 任务（经 ParentTaskRef 有子任务指向它）在全子任务
	// delivered 或终态（failed/canceled）时「可 complete」。「不全 delivered 不强制 complete」→ 此处是
	// 到 stderr 的 advisory 告警，绝不阻断：编排器可合理地综合部分成果，但上浮未交付子任务使「带未完成
	// 工作完成」成为显式选择而非静默。无子任务的 generic 任务（普通调研/设计/接续）不受影响。
	// ListTaskState 失败非致命——完成不应被就绪探测门禁住。
	if states, err := ListTaskStates(root); err == nil {
		if _, pending := OrchestrationReady(states, state.TaskRef); len(pending) > 0 {
			fmt.Fprintf(os.Stderr, `⚠ 编排任务 %s 尚有 %d 个子任务未交付/终态: %s；仍可 complete（设计：不强制），建议先综合子任务成果`, state.TaskRef, len(pending), strings.Join(pending, `, `))
			fmt.Fprintln(os.Stderr)
		}
	}
	head := GetHeadCommit(root)
	// 锁内完成写入：对调用前快照裸 SaveTaskState 会回滚并发写者（本文件已
	// 记录的 §13 丢失更新类）。
	if err := MutateTaskState(root, state.TaskRef, func(s *TaskState) error {
		for _, g := range DefaultGates() {
			s.RecordGateResult(g.ID, true, head)
		}
		s.MarkComplete()
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}
	sid := CurrentSessionID()
	if err := ClearActiveTaskRef(root, sid); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
	}
	// L1 绑定处理分形态：主检出任务 complete 即解绑（新窗口不再解析到已完结
	// 任务）；worktree 任务【保留】绑定——finish 收尾依赖它（合并 + 清理 + 解绑）。
	// complete 时解绑 worktree 绑定会让 finish 判定「无本目录绑定」而永不清理
	// （review B2 关联缺陷的完整修复）。ActiveTaskState 对已完成任务本就跳过绑定
	// 命中，保留不产生误挂。best-effort。
	if IsMainCheckout(root) {
		if err := worktree.Clear(root, state.TaskRef); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clear workspace binding: %v\n", err)
		}
	}
	return nil
}
