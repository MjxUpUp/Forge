package clitask

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/impact"
	"github.com/MjxUpUp/Forge/internal/projectroot"
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

	root, err := projectroot.Find()
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
// acceptance pre-flight → MarkComplete → 采集完成事实 → 反馈 → 清 ref。MarkComplete 只在
// pre-flight 通过之后发生——pre-flight 失败必须保持任务 active，verify-acceptance
// （默认认 active；--ref 可显式指定）才能刷新过期快照、complete 才能重试。（修复前
// 最后一道 gate 就标记完成，gate 后的 commit 一移动 HEAD → pre-flight 永久失败且
// 无复活路径。）
func runTaskCompleteAt(root string, state *taskpipeline.TaskState) error {
	// 幂等重复完成守卫：已 finalize 的任务（完成且已采集事实；或已完成 generic
	// 任务）不得重跑完成副作用（重复采集、重复 HEAD 告警、第二条 Act 结论）。
	if state.CompletedAt != nil && (state.Evidence != nil || state.IsGeneric()) {
		fmt.Printf("Task %s already completed（已完成，幂等跳过）。\n", state.TaskRef)
		return nil
	}

	// generic kind（调研/设计/纯接续任务）：跳过门禁 IsComplete 检查与完成事实采集。这类任务的价值在
	// 持久化的 plan/决策/阻塞（接续真相源），不在代码质量门禁。自动把门禁标 passed（保持
	// History 完整供 list/dashboard 显示）+ MarkComplete + 清 active-task-ref，不采集事实、不创建 review。
	if state.IsGeneric() {
		if err := completeGenericTask(root, state); err != nil {
			return err
		}
		fmt.Printf("Task %s completed (generic, 接续任务不产完成事实)。\n", state.TaskRef)
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

	// acceptance registration pre-flight（oracle-pipeline L1，考卷层级制）：非 generic
	// 交付任务必须至少登记一条验收标准——零标准时整个 deterministic 验收核心对空集
	// 平凡通过（2026-09-07 实证 ratio 0.08 完成事故）。考卷梯子由强到弱：spec 产物
	// 提取（可经 human 档业务签字）＞ start 登记 ＞ conventions 兜底 ＞ 事后补登
	// （manual，验收单如实披露）。逃生与 freshness 检查共用 acceptance-gate 舱。
	if ok, reasons := taskpipeline.CheckAcceptanceRegistered(root, state); !ok {
		return fmt.Errorf(`acceptance registration 未通过（零验收标准——考卷缺位）: %s。登记出口：forge task accept "run :: expected"（本任务补登，层级 manual）；或先 forge task verify-acceptance（有 conventions 档案时自动登记默认套件）。逃生（落 checklog 审计，降 evidence 强度）: forge task override --acceptance-gate disable 或 FORGE_ACCEPTANCE_GATE=disable`,
			strings.Join(reasons, `; `))
	}

	// acceptance pre-flight（proof-of-work consumer）：task 声明了验收标准时，complete 前
	// deterministic 校验每条都 fresh（AcceptedHeadCommit==HEAD）且 Passed。给 AcceptedHeadCommit
	// 补消费方——MCP 拆除后该字段只写不读成孤儿，本检查把它从声明层变 affordance gate。
	// 对应 Emergence World Proof of Work：声称「验收过」必须有可验证 consumer。
	if ok, reasons := taskpipeline.CheckAcceptanceFresh(root, state); !ok {
		return fmt.Errorf(`acceptance pre-flight 未通过（验收未实跑/快照过期/未通过）: %s；逃生（落 checklog 审计，降 evidence 强度到 Weak；重证据任务按证据缩放豁免）: forge task override --acceptance-gate disable 或 FORGE_ACCEPTANCE_GATE=disable`,
			strings.Join(reasons, `; `))
	}

	// 先失败证明「已声明」pre-flight（B2）：行为类改动连 --accept-red 都没登记时，
	// 红先绿机制根本没有输入——CheckRedFirst 只能核对已登记项是否闭合，管不了缺席。
	// 豁免走 task start --red-first-waived "<理由>"，理由留在状态里供评审看。
	changed, cerr := taskpipeline.TaskChangedFilesStrict(root, state)
	if cerr != nil {
		return fmt.Errorf("红先绿已声明检查无法进行：%w。恢复路径：确认 git 可用且仓库完整（git status）后重试", cerr)
	}
	if err := CheckRedFirstDeclared(state, changed); err != nil {
		return err
	}

	// 红先绿 pre-flight：--accept-red 登记的判据必须开工时真失败过、且同一命令现在转绿。
	// 这是「测试先行」唯一的机械证据——没有它，红先绿只是交付单上的一句话。
	if err := CheckRedFirst(root, state); err != nil {
		return err
	}

	// held-out pre-flight（focus-batches §2a）：登记了保留集的任务在完成边界复跑
	// 双套件——防"验收后改码"staleness 的最强形态就是 complete 时再跑一次测试。
	// 可见全过而保留集挂 = test-generalization gap（SpecBench 形态）→ 拒绝完成。
	if ok, reasons := taskpipeline.CheckHeldoutFresh(root, state); !ok {
		return fmt.Errorf(`held-out gap 未通过：%s——可见验收过了但保留集没过（测试泛化缺口，修真实行为而非保留集）。逃生（落 checklog 审计）: FORGE_HELDOUT=disable`,
			strings.Join(reasons, `; `))
	}

	// 回环耗尽 pre-flight（artifact-chain-workflow.md「回边语义」节）：审查回环
	// 轮次预算耗尽或已解决 finding 复活 = 机器侧迭代到此为止——升级人工，complete
	// 被拦。终止权外置：耗尽后唯一出口是人（--reset-loop / abort）。
	if loopReasons := taskpipeline.CheckLoopExhausted(root, state); len(loopReasons) > 0 {
		return fmt.Errorf(`回环耗尽 pre-flight 未通过: %s；人工裁决出口: forge task finding --reset-loop --note "<裁决>"（审计行落地）后重试，或 forge task abort`,
			strings.Join(loopReasons, `; `))
	}

	// self-report pre-flight（focus-batches §1b，方向 B）：checklist 已勾选项声称
	// 执行过的验证类命令 vs toollog 实测 Bash 集。测试类声称任务全程零匹配 =
	// inaccurate self-reporting 形态（arXiv 2605.29442）→ 拒绝完成；非测试类
	// 差集只留 advisory 痕（checklog warn）。toollog 缺失（宿主遥测未接）跳过——
	// 区分"无法验证"与"验证通过"。
	if sr := taskpipeline.CheckSelfReport(root, state); sr.Blocked {
		return fmt.Errorf(`self-report consistency 未通过：checklist 声称的验证命令在任务全程 Bash 记录中零证据（%s）——虚报进度的形态。修复：真实跑通声称的命令后重新 complete，或修正 checklist 描述。逃生（落 checklog 审计）: FORGE_SELF_REPORT=disable`,
			strings.Join(sr.UnmatchedTests, `; `))
	}

	// 影响面披露（forge-ruthless）：complete 时按 diff 算一次影响面，把「改了文档 /
	// 源码无测试」摆到交付单上。这是披露不是门禁——不阻断完成，只让 reviewer 看见。
	if rep, err := impact.Collect(root); err == nil {
		if len(rep.Docs) > 0 || len(rep.SourcesWithoutTest) > 0 {
			fmt.Printf("影响面：%s\n", impact.SummaryLine(rep))
		}
	}

	// review 硬前置 + 审查快照一致性（与 task gate task-complete 同一函数）：
	// 审后改码必须复审——complete 自己也要复核，不能只靠 gate 记录。
	if err := taskpipeline.CheckCompleteReviewPrereqs(root, state); err != nil {
		return err
	}

	// MarkComplete 恰在此处（pre-flight 之后）：完成标记属于 `forge task complete` 的整个
	// 动作而非某道 gate（dogfood 2026-08-18 死锁修复的另一面——见 runTaskGate 的对应注释）。
	// firstComplete 门控（review m1）：仅首次完成时标记——完成事实落盘失败留下的
	// CompletedAt!=nil、Evidence==nil 中间态重试时，补采事实是预期恢复，但 CompletedAt
	// 不得被重置（污染 duration 度量）、Act 结论不得二次追加（act.Append 无 TaskRef 去重）。
	// 先落盘再采集事实，采集失败也不丢完成状态。
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

	// 事实清单（不评分）：判据实跑、红先绿、评审轮次、逃生舱都直接数。
	ev := taskpipeline.BuildTaskEvidence(root, state)
	if err := taskpipeline.MergeOrPersistTaskState(root, state, func(s *taskpipeline.TaskState) error {
		s.Evidence = ev
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save evidence: %v\n", err)
	}
	state.Evidence = ev
	completed := fmt.Sprintf("Task %s completed! 判据 %d 条（通过 %d）· 红先绿 %d/%d 条 · 评审 %d 轮",
		state.TaskRef, ev.Criteria, ev.CriteriaPassed, ev.RedFirstClosed, ev.RedFirstTrials, ev.ReviewRounds)
	if ev.ReviewScore != nil {
		completed += fmt.Sprintf("（score %d）", *ev.ReviewScore)
	}
	if ev.Escapes > 0 {
		completed += fmt.Sprintf(" · 逃生 %d 次", ev.Escapes)
	}
	fmt.Printf("%s → 详见 forge task report\n", completed)

	// 重复 HEAD 检测：同一分支上另一个已完成 task 与之共享 HeadCommit（在相同 commit
	// 范围上重复完成）时告警。仅限同分支避免假阳性。
	if state.HeadCommit != "" {
		allStates, listErr := taskpipeline.ListTaskStates(root)
		if listErr == nil {
			for _, w := range duplicateScoreWarnings(state, allStates) {
				fmt.Fprintf(os.Stderr, "⚠ Warning: %s\n", w)
			}
		}
	}

	// 设计 B：complete 输出末尾的 next 行——任务完结后的下一步（通常是合并分支/开新任务）
	// 顺手可见；ActiveTaskState 对已完成任务返回 nil，NextHint 走「无活跃任务」分支。
	if hint := taskpipeline.NextHint(root, nil); hint.Next != "" {
		fmt.Printf("→ next: %s（%s）\n", hint.Next, hint.Reason)
	}

	// 逃生舱库存行（mechanism-hardening P1-2）：本任务用了几次逃生舱、哪些 gate
	// ——库存进任务报告（Fowler 库存观：可见是治理的第一步）。读侧零额外 IO
	//（checklog 已按任务过滤的 LoadForTask）。
	if escRows, eerr := checklog.LoadForTask(root, state.TaskRef); eerr == nil {
		gates := map[string]int{}
		for _, er := range escRows {
			if er.Check == checklog.CheckEscapeHatch {
				g := checklog.EscapeGateOf(&er)
				if g == "" {
					g = "(legacy)"
				}
				gates[g]++
			}
		}
		if len(gates) > 0 {
			parts := make([]string, 0, len(gates))
			for g, n := range gates {
				parts = append(parts, fmt.Sprintf("%s×%d", g, n))
			}
			sort.Strings(parts)
			total := 0
			for _, n := range gates {
				total += n
			}
			fmt.Fprintf(os.Stderr, "🚪 逃生舱使用：%d 次（%s）——已留 checklog 审计、evidence 封顶 Weak\n", total, strings.Join(parts, ", "))
		}
	}

	// Act 反馈臂（PDCA Act）：构建证据驱动结论落盘，喂给 session-retrospective。
	// 证据强度只由 checklog 事实决定；Nudge 时打印一行回顾指令（stderr，
	// stdout --json 保持干净）。仅 firstComplete（review m1）——重试不得为
	// 同一任务追加第二条结论。无 Nudge（证据强）时 sedimentReminder 仍打印
	// 一句轻提醒：干净任务同样产出可复用教训（2026-08-18 case-split/CI 清扫
	// 都是 A 但都沉淀了多条），此前沉淀评估全靠用户记得问。结论落盘失败时
	// （ok=false）跳过提醒——刚警告完「结论落盘失败」又提醒「评估沉淀载体」
	// 不协调（沉淀的事实源正是落盘失败的结论，code-review 发现）。
	if firstComplete {
		if d, ok := AppendConclusion(root, state); ok {
			fmt.Fprintln(os.Stderr, SedimentReminder(d))
		}
	}

	// 清所有仍指向本任务的锚点——当前会话与其他会话的指针、legacy 全局、workspace 绑定
	// （E.3，按内容匹配）：多会话任务只清自己会让其他会话（及无 session 的 CLI 调用经
	// workspace 绑定）继续把 hook 行归到已完成任务；按内容匹配也避免了旧的按路径清除
	// 在「当前会话指针指向别的任务」（异会话 complete --ref）时误清无关任务。
	if err := taskpipeline.ClearActiveTaskRefsForTask(root, state.TaskRef); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear task anchors: %v\n", err)
	}
	// dogfood 2.3：post-complete grace sentinel，让 file-sentinel 不把自然的后续
	// git commit 误判为「无 active task + 源码写入」而 quarantine。此前流程迫使 agent
	// 开个 chore/*-commit task 纯粹为绕这个坑（DevWorkbench：3 个这种 task，~600 次调用）。
	// grace 窗口有界（默认 5min，执法点在 file-sentinel bash hook 的 300s 字面量）；
	// 窗口外恢复 quarantine 策略——
	// 一个"complete"的 session 持续写源码 30+ 分钟已不再 complete，应开新 task。
	if err := taskpipeline.MarkCompleteGrace(root, taskpipeline.CurrentSessionID()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to mark complete grace: %v\n", err)
	}

	return nil
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

// appendConclusion thin-wrapper：Act 结论构建+落盘下沉到 taskpipeline.AppendConclusion
// （单一真相源）。cli 与 MCP forge_task_complete 共用同一 Act 反馈臂。stderr 警告由本 wrapper
// 保留（CLI 交互语义），taskpipeline 层只返结构化结果。
func AppendConclusion(root string, state *taskpipeline.TaskState) (string, bool) {
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
// SedimentReminder renders the review-sediment reminder line for a directive.
//
// SedimentReminder 渲染 directive 的评审沉淀提醒行（act 域测试经
// clitask.AppendConclusion 链路消费）。
func SedimentReminder(directive string) string {
	if directive != "" {
		return directive
	}
	return `ADVISORY: 若本次任务产出过非显然教训（排查链长的坑、会重复踩的模式、差点进主干的缺陷），评估沉淀载体（→ session-retrospective）；常识/一次性细节/代码已记录的不沉淀。`
}
