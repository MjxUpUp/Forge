package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// duplicateScoreWarnings 返同分支已完成 task 中与 state 共享 HeadCommit 的告警串——
// 即在相同 commit 范围上的重新评分。跨分支匹配不计：从同一 master HEAD 拉出的
// 独立 feature 分支都在 task start 时记录同样的 HeadCommit，但它们的 diff 在独立分支
// 上不重叠，不算重复。
func duplicateScoreWarnings(state *taskpipeline.TaskState, allStates []*taskpipeline.TaskState) []string {
	if state.HeadCommit == "" {
		return nil
	}
	var warnings []string
	for _, s := range allStates {
		if s.TaskRef == state.TaskRef || s.Branch != state.Branch || s.HeadCommit != state.HeadCommit || s.CompletedAt == nil {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("task %q shares HEAD (%s) with completed task %q — possible duplicate scoring.",
			state.TaskRef, state.HeadCommit, s.TaskRef))
	}
	return warnings
}

func runTaskComplete(cmd *cobra.Command, args []string) error {
	explicitRef, _ := cmd.Flags().GetString("ref")

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return err
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return fmt.Errorf("failed to load task state: %w", err)
		}
		// 兜底：旧 gate 行为（2026-08-18 死锁修复前，最后一道 gate 即 MarkComplete，任务对
		// ActiveTaskState 失活）留下的存量状态。经 branch context load，让它们仍能 finalize。
		if state == nil {
			ctx := taskcontext.Detect(root)
			if ctx.IsSet() {
				state, _ = taskpipeline.LoadTaskState(root, ctx.TaskRef)
			}
		}
	}
	if state == nil {
		return fmt.Errorf("no active task. Run forge task start first")
	}
	return runTaskCompleteAt(root, state)
}

// runTaskCompleteAt 是 runTaskComplete 的 root 注入核心（独立可测，与
// runTaskVerifyAcceptanceAt 同款范式）。task 解析之后的一切都在这里。顺序契约
// （dogfood 2026-08-18 死锁修复）：重复完成守卫 → generic 路径 → IsComplete →
// acceptance pre-flight → MarkComplete → 评分 → 反馈 → 清 ref。MarkComplete 只在
// pre-flight 通过之后发生——pre-flight 失败必须保持任务 active，verify-acceptance
// （默认认 active；--ref 可显式指定）才能刷新过期快照、complete 才能重试。（修复前
// 最后一道 gate 就标记完成，gate 后的 commit 一移动 HEAD → pre-flight 永久失败且
// 无复活路径。）
func runTaskCompleteAt(root string, state *taskpipeline.TaskState) error {
	// 幂等重复完成守卫：已 finalize 的任务（完成且已评分；或从不评分的已完成 generic
	// 任务）不得重跑完成副作用（重复评分、重复 HEAD 告警、第二条 Act 结论）。
	if state.CompletedAt != nil && (state.Score != nil || state.IsGeneric()) {
		fmt.Printf("Task %s already completed（已完成，幂等跳过）。\n", state.TaskRef)
		return nil
	}

	// generic kind（调研/设计/纯接续任务）：跳过门禁 IsComplete 检查和评分。这类任务的价值在
	// 持久化的 plan/决策/阻塞（接续真相源），不在代码质量门禁。自动把 3 道门禁标 passed（保持
	// History 完整供 list/dashboard 显示）+ MarkComplete + 清 active-task-ref，不评分不创建 review。
	if state.IsGeneric() {
		if err := completeGenericTask(root, state); err != nil {
			return err
		}
		fmt.Printf("Task %s completed (generic, 接续任务不评分)。\n", state.TaskRef)
		return nil
	}

	if !state.IsComplete() {
		return fmt.Errorf("task not complete. Missing gates: %s", missingGates(state))
	}

	// 完整性门（state-integrity-signing）：验签失败 = 状态文件在 forge 之外被改
	// 过——其上手改的 ReviewPassed/DocReview 不得完成（2026-08-29 功能探针封的
	// 是写入侧，这里是消费侧）。
	if state.IntegrityBroken() {
		return fmt.Errorf(`task complete 拒绝：任务状态文件完整性校验失败（在 forge 之外被修改——手改的 review/doc 证据不被采信）。恢复路径：forge task abort 后重新走门禁`)
	}

	// acceptance pre-flight（proof-of-work consumer）：task 声明了验收标准时，complete 前
	// deterministic 校验每条都 fresh（AcceptedHeadCommit==HEAD）且 Passed。给 AcceptedHeadCommit
	// 补消费方——MCP 拆除后该字段只写不读成孤儿，本检查把它从声明层变 affordance gate。
	// 对应 Emergence World Proof of Work：声称「验收过」必须有可验证 consumer。
	if ok, reasons := taskpipeline.CheckAcceptanceFresh(root, state); !ok {
		return fmt.Errorf(`acceptance pre-flight 未通过（验收未实跑/快照过期/未通过）: %s；逃生（落 checklog 审计，降 evidence 强度到 Weak；重证据任务按证据缩放豁免）: forge task override --acceptance-gate disable 或 FORGE_ACCEPTANCE_GATE=disable`,
			strings.Join(reasons, `; `))
	}

	// doc pre-flight（输出→回检循环的流程节点）：任务变更了 markdown 产物时，
	// complete 前 L1 确定性 lint 全过 + L2 回检证据（DocReview fresh/Passed/
	// ≥75 分）+ 零未决 Critical。无文档产物放行；逃生舱与 acceptance 对称。
	// 设计：docs/design/output-readability-gates.md（飞书《AI 产物可读性差调研
	// 设计》落地方案二）。
	if ok, reasons := taskpipeline.CheckDocGate(root, state); !ok {
		return fmt.Errorf(`doc gate 未通过（文档产物未过 L1 lint / L2 回检）: %s；流程：forge docs lint <paths> 修 L1 → 按 doc-review skill 评审（产出者不能自检）→ forge task doc-review 记录证据。逃生（落 checklog 审计，降 evidence 强度到 Weak）: forge task override --doc-gate disable 或 FORGE_DOC_GATE=disable`,
			strings.Join(reasons, `; `))
	}

	// MarkComplete 恰在此处（pre-flight 之后）：完成标记属于 `forge task complete` 的整个
	// 动作而非某道 gate（dogfood 2026-08-18 死锁修复的另一面——见 runTaskGate 的对应注释）。
	// firstComplete 门控（review m1）：仅首次完成时标记——评分失败留下的 CompletedAt!=nil、
	// Score==nil 中间态重试时，再评分是预期恢复，但 CompletedAt 不得被重置（污染
	// duration 度量）、Act 结论不得二次追加（act.Append 无 TaskRef 去重）。
	// 先落盘再评分，评分失败也不丢完成状态。
	firstComplete := state.CompletedAt == nil
	if firstComplete {
		state.MarkComplete()
	}
	// 锁内完成写入——把 CompletedAt 合并进锁内状态（§13：对 pre-flight 前的快照
	// 裸保存会回滚并发写者）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		if s.CompletedAt == nil {
			s.MarkComplete()
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}

	// 自动评分 task
	if err := scoreTask(root, state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: scoring failed: %v\n", err)
	}

	if state.Score != nil {
		fmt.Printf("Task %s completed! Score: %.0f (%s)\n", state.TaskRef, state.Score.Overall, state.Score.Grade)

		// 重复 HEAD 检测：同一分支上另一个已完成 task 与之共享 HeadCommit（在相同 commit
		// 范围上重评分）时告警。仅限同分支避免假阳性——每个从同一 master HEAD 拉出的
		// feature 分支都在 task start 时记同样的 HeadCommit，但它们的 diff 在独立分支上不重叠，
		// 故跨分支匹配不算重复。
		if state.HeadCommit != "" {
			allStates, listErr := taskpipeline.ListTaskStates(root)
			if listErr == nil {
				for _, w := range duplicateScoreWarnings(state, allStates) {
					fmt.Fprintf(os.Stderr, "⚠ Warning: %s\n", w)
				}
			}
		}

		// 缺失 hook 检查：关键质量 hook 从未跑过时告警。
		missingHooks := checkMissingHooks(root, state)
		hasMissingHooks := len(missingHooks) > 0
		if hasMissingHooks {
			fmt.Fprintf(os.Stderr, "\n⚠ WARNING: Critical quality hooks were NOT executed during this task:\n")
			for _, h := range missingHooks {
				fmt.Fprintf(os.Stderr, "  - %s\n", h)
			}
			fmt.Fprintf(os.Stderr, "  The score (%s, %.0f) may not reflect actual code quality.\n",
				state.Score.Grade, state.Score.Overall)
			fmt.Fprintf(os.Stderr, "  Ensure the AI agent ran all required hooks during implementation.\n\n")
		}

	} else {
		fmt.Printf("Task %s completed!\n", state.TaskRef)
	}

	// Act 反馈臂（PDCA Act）：构建证据驱动结论落盘，喂给 session-retrospective。
	// 即使评分失败也建（证据强度不依赖分数）；Nudge 时打印一行回顾指令（stderr，
	// stdout --json 保持干净）。仅 firstComplete（review m1）——评分失败重试不得为
	// 同一任务追加第二条结论。无 Nudge（证据强+高分）时 sedimentReminder 仍打印
	// 一句轻提醒：干净任务同样产出可复用教训（2026-08-18 case-split/CI 清扫
	// 都是 A 但都沉淀了多条），此前沉淀评估全靠用户记得问。结论落盘失败时
	// （ok=false）跳过提醒——刚警告完「结论落盘失败」又提醒「评估沉淀载体」
	// 不协调（沉淀的事实源正是落盘失败的结论，code-review 发现）。
	if firstComplete {
		if d, ok := appendConclusion(root, state); ok {
			fmt.Fprintln(os.Stderr, sedimentReminder(d))
		}
	}

	// 清 active task ref——task 完成（session-scoped）
	if err := taskpipeline.ClearActiveTaskRef(root, taskpipeline.CurrentSessionID()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
	}
	// dogfood 2.3：post-complete grace sentinel，让 file-sentinel 不把自然的后续
	// git commit 误判为「无 active task + 源码写入」而 quarantine。此前流程迫使 agent
	// 开个 chore/*-commit task 纯粹为绕这个坑（DevWorkbench：3 个这种 task，~600 次调用）。
	// grace 窗口有界（默认 5min，见 completeGraceWindow）；窗口外恢复 quarantine 策略——
	// 一个"complete"的 session 持续写源码 30+ 分钟已不再 complete，应开新 task。
	if err := taskpipeline.MarkCompleteGrace(root, taskpipeline.CurrentSessionID()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to mark complete grace: %v\n", err)
	}

	return nil
}

// checkMissingHooks 返本任务期间从未跑过的关键质量 hook 名（基于 checklog 条目与 gate 历史）。
func checkMissingHooks(root string, state *taskpipeline.TaskState) []string {
	var missing []string

	latestChecks, err := checklog.LatestByCheckForSessionSince(root, state.SessionID, state.StartedAt)
	if err != nil || latestChecks == nil {
		// 读不到 checklog——除非 gate 历史显示跑过，否则假设所有 hook 都缺失。
		compileRan := false
		for _, r := range state.History {
			if r.Gate == "task-implement" && r.Passed {
				compileRan = true
				break
			}
		}
		if !compileRan {
			missing = append(missing, "auto-compile")
		}
		missing = append(missing, "assertion-check")
		return missing
	}

	if _, ok := latestChecks[checklog.CheckAssertion]; !ok {
		missing = append(missing, "assertion-check")
	}
	if _, ok := latestChecks[checklog.CheckAutoCompile]; !ok {
		// 检查编译是否经 task-implement gate 跑过。
		compileRan := false
		for _, r := range state.History {
			if r.Gate == "task-implement" && r.Passed {
				compileRan = true
				break
			}
		}
		if !compileRan {
			missing = append(missing, "auto-compile")
		}
	}

	return missing
}

func missingGates(state *taskpipeline.TaskState) string {
	var missing []string
	completed := state.CompletedGates()
	completedMap := make(map[string]bool)
	for _, id := range completed {
		completedMap[id] = true
	}
	for _, g := range taskpipeline.DefaultGates() {
		if !completedMap[g.ID] {
			missing = append(missing, g.ID)
		}
	}
	return strings.Join(missing, ", ")
}

// scoreTask 评估已完成的 task 并保存评分。
// scoreTask thin-wrapper：评分下沉到 taskpipeline.ScoreTask（单一真相源）。cli runTaskComplete
// /runTaskScore 与测试透明复用——MCP forge_task_complete 走同一 taskpipeline.ScoreTask。
func scoreTask(root string, state *taskpipeline.TaskState) error {
	return taskpipeline.ScoreTask(root, state)
}

// appendConclusion thin-wrapper：Act 结论构建+落盘下沉到 taskpipeline.AppendConclusion
// （单一真相源）。cli 与 MCP forge_task_complete 共用同一 Act 反馈臂。stderr 警告由本 wrapper
// 保留（CLI 交互语义），taskpipeline 层只返结构化结果。
func appendConclusion(root string, state *taskpipeline.TaskState) (string, bool) {
	_, directive, err := taskpipeline.AppendConclusion(root, state)
	if err != nil {
		fmt.Fprintln(os.Stderr, `Warning:`, err)
		return ``, false
	}
	return directive, true
}

// sedimentReminder 决定 task complete 打印的一行：有 RetrospectiveNudge 时原样
// 返回 Act directive（其结尾已带沉淀行动入口）；否则返回一句轻提醒——干净的
// 完成同样可能产出可复用教训。确定性、宿主无关——不同于模型侧 trigger，不会
// 被死 Stop 通道或遗忘的 prompt 丢掉。「什么值得沉淀」的判断委托给
// session-retrospective 自己的不沉淀清单（常识/一次性细节/代码已记录的），
// 让这行提醒保持噪声有界。
func sedimentReminder(directive string) string {
	if directive != "" {
		return directive
	}
	return `ADVISORY: 若本次任务产出过非显然教训（排查链长的坑、会重复踩的模式、差点进主干的缺陷），评估沉淀载体（→ session-retrospective）；常识/一次性细节/代码已记录的不沉淀。`
}
