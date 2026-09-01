package clitask

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// resolveTaskState 解析目标任务：--ref 显式指定优先，否则取 session 绑定的活跃
// 任务。错误策略沿主流形态：显式 ref 失败原样透传、活跃态失败包英文上下文。
// （2026-09 普查 P3-4：14 处序言实测仅少数纯同构——错误文案/nil 兜底/silent
// 各异属刻意 UX；语义完全一致的 raw-err 族共 3 处全部收敛——本文件 2 处 +
// task_impact 1 处，其余分化形态保留原地。）
func resolveTaskState(root, explicitRef string) (*taskpipeline.TaskState, error) {
	if explicitRef != "" {
		return taskpipeline.LoadTaskState(root, explicitRef)
	}
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return nil, fmt.Errorf("failed to load task state: %w", err)
	}
	return state, nil
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	explicitRef, _ := cmd.Flags().GetString("ref")

	root, err := projectroot.Find()
	if err != nil {
		return err
	}

	state, err := resolveTaskState(root, explicitRef)
	if err != nil {
		return err
	}
	if state == nil {
		if asJSON {
			// --json 契约：stdout 只放机器可解析内容——空场景的人类提示行同样会
			// 弄坏 jq 类消费者。
			fmt.Println(`{"task_ref": null, "active": false, "hint": "forge task start"}`)
			return nil
		}
		fmt.Println("No active task (not on a feature branch or no task started).")
		fmt.Println("Run 'forge task start' to begin a task.")
		return nil
	}

	if asJSON {
		output, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	fmt.Printf("Task: %s\n", state.TaskRef)
	fmt.Printf("Branch: %s\n", state.Branch)
	if state.Summary != "" {
		fmt.Printf("Summary: %s\n", state.Summary)
	}
	// 多仓 workspace 上下文（Step 4）：单行 fail-open——清单是全局 store，
	// 任何故障静默省略该行。
	if line := workspaceContextLine(root, state.CrossRepoImpact); line != "" {
		fmt.Println(line)
	}
	fmt.Printf("Started: %s\n", state.StartedAt.Format("2006-01-02 15:04"))
	fmt.Println(strings.Repeat("─", 40))

	gates := taskpipeline.DefaultGates()
	for _, g := range gates {
		marker := "  "
		status := "pending"

		for _, r := range state.History {
			if r.Gate == g.ID {
				if r.Passed {
					marker = "✅"
					status = "passed"
				} else {
					marker = "❌"
					status = "failed"
				}
			}
		}

		if state.CurrentGate == g.ID {
			marker = "🚦"
			status = "current"
		}

		fmt.Printf("%s %-18s %s\n", marker, g.Name, status)
	}

	fmt.Println(strings.Repeat("─", 40))

	if state.HasAcceptance() {
		fmt.Println("验收标准:")
		for i, c := range state.Acceptance {
			mark := "⏳"
			status := "未验证"
			if c.Passed {
				mark = "✅"
				status = "通过"
			} else if c.Output != "" {
				// Output 仅在 verify-acceptance 实跑后回填——区分「没跑过」(⏳)与「跑过且失败」(❌)。
				mark = "❌"
				status = "失败"
			}
			exp := c.Expected
			if exp == "" {
				exp = "(退出码 0)"
			}
			fmt.Printf("  %s [%d] %s :: %s — %s\n", mark, i+1, c.Run, exp, status)
		}
		fmt.Println(strings.Repeat("─", 40))
	}

	if len(state.PlanScope) > 0 {
		fmt.Printf("计划改动白名单（%d 条）：\n", len(state.PlanScope))
		for _, s := range state.PlanScope {
			fmt.Printf("  %s\n", s)
		}
		fmt.Println(strings.Repeat("─", 40))
	}

	if state.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", state.CompletedAt.Format("2006-01-02 15:04"))
	} else if state.CurrentGate != "" {
		fmt.Printf("Next: forge task gate %s\n", state.CurrentGate)
	}

	return nil
}

// runTaskScopeAdd 把 glob 追加到当前任务的 PlanScope（去重）。支持中途迭代——规划不是
// task start 一次锁死：分层定位、「重新考虑改哪些文件」
// 都印证 scope 是演进的。持久化后立即生效（后续 hook 据此 advisory 检测 drift）。
func runTaskScopeAdd(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	// --ref pins the task explicitly (task 子命令族一致性：scope-drift 拦截后 agent 按惯性
	// 带 --ref 追加，旧版 unknown flag 白跑一轮）。空则保持旧的活跃任务检测。
	explicitRef, _ := cmd.Flags().GetString("ref")
	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return fmt.Errorf("加载任务 %q 失败: %w", explicitRef, err)
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return fmt.Errorf("failed to load task state: %w", err)
		}
	}
	if state == nil {
		return fmt.Errorf("no active task. Run 'forge task start' first（或用 --ref 指定任务）")
	}
	existing := make(map[string]bool, len(state.PlanScope))
	for _, s := range state.PlanScope {
		existing[s] = true
	}
	added := 0
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" || existing[a] {
			continue
		}
		state.PlanScope = append(state.PlanScope, a)
		existing[a] = true
		added++
	}
	// 锁内 scope 写入——PlanScope 在锁内合并（§13 丢失更新）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		existing := make(map[string]bool, len(s.PlanScope))
		for _, p := range s.PlanScope {
			existing[p] = true
		}
		for _, a := range args {
			a = strings.TrimSpace(a)
			if a == "" || existing[a] {
				continue
			}
			s.PlanScope = append(s.PlanScope, a)
			existing[a] = true
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}
	fmt.Printf("PlanScope 现共 %d 条（本次新增 %d）：\n", len(state.PlanScope), added)
	for _, s := range state.PlanScope {
		fmt.Printf("  %s\n", s)
	}
	return nil
}

// runTaskOverride 设置 per-task 逃生舱（方案5 防泄漏）。优先于全局 env——一个任务逃生
// 不污染同 shell 的其他任务。用了任一逃生舱会记 CheckEscapeHatch 并把 evidence Strength
// cap 到 Weak（让逃生有代价，对冲「硬门禁 + 全局逃生 = 假硬门禁」反噬）。legitimate 用途：
// doc-only 仓库、生成代码、CI；勿用于逃避 read-before-edit/test-coverage。
func runTaskOverride(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")
	wa, _ := cmd.Flags().GetString("work-activity")
	tc, _ := cmd.Flags().GetString("test-coverage")
	ag, _ := cmd.Flags().GetString(`acceptance-gate`)
	sd, _ := cmd.Flags().GetString("skill-decisions")

	state, err := resolveTaskState(root, explicitRef)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("no active task. Run 'forge task start' first")
	}

	changed := false
	if wa != "" {
		if wa != "disable" {
			return fmt.Errorf("--work-activity 只接受 disable，got %q", wa)
		}
		state.Overrides.WorkActivity = "disable"
		changed = true
	}
	if tc != "" {
		if tc != "disable" {
			return fmt.Errorf("--test-coverage 只接受 disable，got %q", tc)
		}
		state.Overrides.TestCoverage = "disable"
		changed = true
	}
	if ag != "" {
		if ag != `disable` {
			return fmt.Errorf(`--acceptance-gate 只接受 disable，got %q`, ag)
		}
		state.Overrides.AcceptanceGate = `disable`
		changed = true
	}
	if sd != "" {
		if sd != "disable" {
			return fmt.Errorf(`--skill-decisions 只接受 disable，got %q`, sd)
		}
		state.Overrides.SkillDecisions = "disable"
		changed = true
	}
	dg, _ := cmd.Flags().GetString("doc-gate")
	if dg != "" {
		if dg != "disable" {
			return fmt.Errorf(`--doc-gate 只接受 disable，got %q`, dg)
		}
		state.Overrides.DocGate = "disable"
		changed = true
	}
	if !changed {
		fmt.Printf("当前 per-task 逃生舱：%s\n", describeOverrides(state.Overrides))
		fmt.Println(`设置：--work-activity disable / --test-coverage disable / --acceptance-gate disable / --skill-decisions disable（验证类逃生降评分强度到 Weak，重证据任务按证据缩放豁免；work-activity 不降）`)
		return nil
	}
	// 锁内逃生舱写入——只合并 Overrides 字段（§13 丢失更新）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		s.Overrides = state.Overrides
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}
	fmt.Printf("已设置 per-task 逃生舱：%s\n", describeOverrides(state.Overrides))
	fmt.Println("注意：验证类逃生舱（test-coverage/acceptance-gate/skill-decisions）会记 checklog 并把任务 evidence 强度 cap 到 Weak（重证据任务按 2026-08 证据缩放豁免）；work-activity 是节奏门禁，只审计不降强度。")
	return nil
}

func describeOverrides(o taskpipeline.TaskOverrides) string {
	var parts []string
	if o.WorkActivity == "disable" {
		parts = append(parts, "work-activity=disable")
	}
	if o.TestCoverage == "disable" {
		parts = append(parts, "test-coverage=disable")
	}
	if o.AcceptanceGate == `disable` {
		parts = append(parts, `acceptance-gate=disable`)
	}
	if o.SkillDecisions == "disable" {
		parts = append(parts, "skill-decisions=disable")
	}
	if o.DocGate == "disable" {
		parts = append(parts, "doc-gate=disable")
	}
	if len(parts) == 0 {
		return "（无）"
	}
	return strings.Join(parts, ", ")
}

// runTaskDocReview 在 rubric 评审（doc-review skill）
// 后记录 L2 文档回检证据。门禁（CheckDocGate）是消费方：评审未记录、过期、
// 未通过或得分低于阈值时 complete 被拒。仅记录不会伪造通过——--passed fail
// 保持阻断并累加轮次（升级上限的计数）。Critical 发现落 Findings
// （Source=doc-review、Severity=critical），经 forge task finding resolve 解决。
func runTaskDocReview(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")
	passedFlag, _ := cmd.Flags().GetString("passed")
	score, _ := cmd.Flags().GetInt("score")
	round, _ := cmd.Flags().GetInt("round")
	reviewer, _ := cmd.Flags().GetString("reviewer")
	criticals, _ := cmd.Flags().GetStringSlice("critical")

	if passedFlag != "pass" && passedFlag != "fail" {
		return fmt.Errorf(`--passed 必填且只接受 pass | fail，got %q（先按 doc-review skill 评审——产出者不能当回检者）`, passedFlag)
	}
	if score < 0 || score > 100 {
		return fmt.Errorf("--score 取值 0-100（rubric 四维各 0-25），got %d", score)
	}

	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err == nil && state == nil {
			err = fmt.Errorf("no active task. Run 'forge task start' first")
		}
	}
	if err != nil {
		return fmt.Errorf("failed to load task state: %w", err)
	}

	if round <= 0 {
		// 从上一轮自动递增：第 2 轮评审不得静默重置为 1（升级上限数真实轮次）。
		round = 1
		if state.DocReview != nil && state.DocReview.Round >= round {
			round = state.DocReview.Round + 1
		}
	}

	for _, c := range criticals {
		nf := taskpipeline.Finding{
			Content:  c,
			Source:   taskpipeline.DocReviewSource,
			Severity: taskpipeline.FindingSeverityCritical,
			Evidence: fmt.Sprintf("round %d rubric=%d", round, score),
		}
		taskpipeline.EnrichFinding(root, state, &nf)
		state.AddFinding(nf)
	}

	// 轮次历史保留（循环的可观测收敛）：历史轮次留在 DocReviewHistory，得分
	// 趋势可从任务状态查询——「两轮之间 Critical 不降」是异常信号而非散文。
	// 截断保留最近 10 轮（内存卫生）。
	// 锁内 doc-review 写入——DocReview 与轮次历史在锁内合并（§13；历史滚动也
	// 必须落在锁内状态上，而非陈旧快照）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		if s.DocReview != nil && !s.DocReview.ReviewedAt.IsZero() {
			s.DocReviewHistory = append(s.DocReviewHistory, *s.DocReview)
			if len(s.DocReviewHistory) > 10 {
				s.DocReviewHistory = s.DocReviewHistory[len(s.DocReviewHistory)-10:]
			}
		}
		s.DocReview = &taskpipeline.DocReview{
			Passed:          passedFlag == "pass",
			RubricScore:     score,
			Round:           round,
			Reviewer:        reviewer,
			ReviewedAt:      time.Now(),
			HeadCommit:      taskpipeline.GetHeadCommit(root),
			DocsFingerprint: taskpipeline.DocContentFingerprint(root, s),
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}

	fmt.Printf("doc-review 已记录（round %d，score %d，verdict %s，critical +%d）。\n", round, score, passedFlag, len(criticals))
	if passedFlag == "pass" && score < taskpipeline.DocRubricThreshold {
		fmt.Printf("注意：verdict=pass 但 score %d < 阈值 %d——doc gate 仍会拦截（得分与结论矛盾，复评）。\n", score, taskpipeline.DocRubricThreshold)
	}
	if passedFlag != "pass" && round >= taskpipeline.DocReviewMaxRounds {
		fmt.Printf("已 %d 轮未过（上限 %d）——升级人工确认：用户裁定放行（forge task override --doc-gate disable）或给出下一轮修复方向。\n", round, taskpipeline.DocReviewMaxRounds)
	}
	return nil
}

// runTaskScopeShow 打印声明的 PlanScope + 实时 scope-drift（实改源码 vs 声明的差集）。
// drift 全程 advisory：变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract，
// 偏差是常态信号——这里只是把它从隐性变成可度量、可回顾，绝不阻塞。
func runTaskScopeShow(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return fmt.Errorf("failed to load task state: %w", err)
	}
	if state == nil {
		return fmt.Errorf("no active task. Run 'forge task start' first")
	}

	fmt.Printf("Task: %s\n", state.TaskRef)
	if len(state.PlanScope) == 0 {
		fmt.Println("PlanScope: 空（未声明计划改动白名单——无声明则不检测 scope-drift）")
		fmt.Println("声明: forge task start --scope <glob>  或中途追加: forge task scope add <glob>")
		return nil
	}
	fmt.Printf("PlanScope（%d 条，声明态 / desired state）：\n", len(state.PlanScope))
	for _, s := range state.PlanScope {
		fmt.Printf("  %s\n", s)
	}
	fmt.Println(strings.Repeat("─", 40))

	changed := taskpipeline.ChangedFiles(root, state)
	drift := taskpipeline.ScopeDrift(changed, state.PlanScope)
	if len(drift) == 0 {
		fmt.Println("scope-drift: 无（实改源码均在声明内 ✅）")
		return nil
	}
	fmt.Printf("scope-drift（advisory，%d 个源码文件超出声明——实改态 vs 声明态差集）：\n", len(drift))
	for _, f := range drift {
		fmt.Printf("  ⚠ %s\n", f)
	}
	fmt.Println("(advisory：不阻塞。偏差供 review 参考，用 forge task scope add <glob> 收编)")
	return nil
}

func runTaskScore(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	showHistory, _ := cmd.Flags().GetBool("history")
	explicitRef, _ := cmd.Flags().GetString("ref")

	root, err := projectroot.Find()
	if err != nil {
		return err
	}

	// 显示所有已评分 task 的历史
	if showHistory {
		states, err := taskpipeline.ListTaskStates(root)
		if err != nil {
			return err
		}
		var scored []*taskpipeline.TaskState
		for _, s := range states {
			if s.Score != nil {
				scored = append(scored, s)
			}
		}
		if len(scored) == 0 {
			fmt.Println("No scored tasks yet.")
			return nil
		}
		if asJSON {
			output, _ := json.MarshalIndent(scored, "", "  ")
			fmt.Println(string(output))
			return nil
		}
		fmt.Println("Task Score History:")
		fmt.Println(strings.Repeat("─", 60))
		for _, s := range scored {
			fmt.Printf("  %s — %.0f (%s) — %s\n",
				s.TaskRef, s.Score.Overall, s.Score.Grade,
				s.Score.ScoredAt.Format("2006-01-02 15:04"))
		}
		return nil
	}

	// 加载单个 task
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
		// 兜底：完成的 task 不再 active 但仍可评分。
		if state == nil {
			ctx := taskcontext.Detect(root)
			if ctx.IsSet() {
				state, _ = taskpipeline.LoadTaskState(root, ctx.TaskRef)
			}
		}
	}
	if state == nil {
		return fmt.Errorf("no active task")
	}

	// 未评分则评分
	if state.Score == nil {
		if !state.IsComplete() {
			return fmt.Errorf("task not complete. Complete it first with 'forge task complete'")
		}
		if err := scoreTask(root, state); err != nil {
			return fmt.Errorf("scoring failed: %w", err)
		}
	}

	if asJSON {
		output, _ := json.MarshalIndent(state.Score, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	fmt.Printf("Task: %s\n", state.TaskRef)
	fmt.Println(strings.Repeat("─", 60))
	for _, d := range state.Score.Dimensions {
		fmt.Printf("  %-15s %3d  %s\n", d.Dimension, d.Score, d.Detail)
	}
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  Overall: %.0f (%s)\n", state.Score.Overall, state.Score.Grade)
	// 审查-返工循环度量（检验审查流程是否收敛的结果指标）。仅信息展示——不进加权总分。
	if ev := state.Score.Evidence; ev != nil && (ev.ReviewPasses > 0 || ev.CompleteRejections > 0) {
		fmt.Printf("  返工轮次: review pass %d 次 / task-complete 被拒 %d 次\n", ev.ReviewPasses, ev.CompleteRejections)
	}
	return nil
}

func runTaskList(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}

	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return err
	}
	if len(states) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	timeline, _ := cmd.Flags().GetBool("timeline")

	if asJSON {
		output, _ := json.MarshalIndent(states, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	if timeline {
		return runTaskTimeline(root, states)
	}

	fmt.Println("Tasks:")
	fmt.Println(strings.Repeat("─", 60))
	for _, s := range states {
		status := "active"
		if s.CompletedAt != nil {
			status = "completed"
		}
		score := ""
		if s.Score != nil {
			score = fmt.Sprintf(" — %.0f (%s)", s.Score.Overall, s.Score.Grade)
		}
		fmt.Printf("  %-25s %s%s\n", s.TaskRef, status, score)
	}
	return nil
}

// runTaskTimeline 按 session 分组 task 并展示 ASCII timeline。
func runTaskTimeline(root string, states []*taskpipeline.TaskState) error {
	sessions, err := taskpipeline.LoadSessions(root)
	if err != nil {
		// 加载不出 session 时回退到简单 flat list。
		fmt.Println("Task Timeline (session data unavailable):")
		fmt.Println(strings.Repeat("─", 60))
		for _, s := range states {
			printTaskLine(s)
		}
		return nil
	}

	// 建 session → tasks 索引
	sessionTasks := make(map[string][]*taskpipeline.TaskState)
	var orphanTasks []*taskpipeline.TaskState

	for _, s := range states {
		if s.SessionID == "" {
			orphanTasks = append(orphanTasks, s)
		} else {
			sessionTasks[s.SessionID] = append(sessionTasks[s.SessionID], s)
		}
	}

	fmt.Println("Task Timeline:")
	fmt.Println(strings.Repeat("─", 70))

	// 按时间顺序打印 session
	for _, sess := range sessions {
		tasks, ok := sessionTasks[sess.SessionID]
		if !ok || len(tasks) == 0 {
			continue
		}

		// session 头部
		endTime := ""
		latest := findLatestTaskTime(tasks)
		if !latest.IsZero() {
			endTime = fmt.Sprintf(" - %s", latest.Format("15:04"))
		} else {
			endTime = " - ..."
		}
		agentStr := ""
		if sess.AgentType != "" {
			agentStr = fmt.Sprintf(" [%s]", sess.AgentType)
		}
		fmt.Printf("\nSession %s%s\n", sess.SessionID, agentStr)
		fmt.Printf("  %s%s\n", sess.StartedAt.Format("01-02 15:04"), endTime)

		// session 内按开始时间排序 task
		sortTasksByTime(tasks)

		for j, t := range tasks {
			prefix := "  ├──"
			if j == len(tasks)-1 {
				prefix = "  └──"
			}
			printTaskTreeLine(prefix, t)
		}
	}

	// 打印 orphan task（无 session 关联）
	if len(orphanTasks) > 0 {
		fmt.Printf("\n(no session data)\n")
		sortTasksByTime(orphanTasks)
		for j, t := range orphanTasks {
			prefix := "  ├──"
			if j == len(orphanTasks)-1 {
				prefix = "  └──"
			}
			printTaskTreeLine(prefix, t)
		}
	}

	if len(sessionTasks) == 0 && len(orphanTasks) == 0 {
		fmt.Println("No tasks to display.")
	}
	fmt.Println()
	return nil
}

// printTaskLine 以 flat 格式打印单个 task。
func printTaskLine(s *taskpipeline.TaskState) {
	status := "active"
	if s.CompletedAt != nil {
		status = "completed"
	}
	score := ""
	if s.Score != nil {
		score = fmt.Sprintf(" %.0f (%s)", s.Score.Overall, s.Score.Grade)
	}
	startTime := s.StartedAt.Format("01-02 15:04")
	fmt.Printf("  %s  %-25s %s%s\n", startTime, s.TaskRef, status, score)
}

// printTaskTreeLine 以 tree 格式打印单个 task。
func printTaskTreeLine(prefix string, s *taskpipeline.TaskState) {
	startTime := s.StartedAt.Format("15:04")
	status := "✅"
	if s.CompletedAt == nil {
		status = "🔄"
	}
	score := ""
	if s.Score != nil {
		score = fmt.Sprintf(" %.0f(%s)", s.Score.Overall, s.Score.Grade)
		if s.Score.Overall < 70 {
			score += " ⚠"
		}
	}
	summary := ""
	if s.Summary != "" && s.Summary != s.TaskRef {
		summary = fmt.Sprintf(" — %s", s.Summary)
	}
	fmt.Printf("%s %s %-25s %s%s%s\n", prefix, startTime, s.TaskRef, status, score, summary)
}

// findLatestTaskTime 返回一组 task 中最近的时间。
func findLatestTaskTime(tasks []*taskpipeline.TaskState) time.Time {
	var latest time.Time
	for _, t := range tasks {
		if t.CompletedAt != nil && t.CompletedAt.After(latest) {
			latest = *t.CompletedAt
		}
		if t.StartedAt.After(latest) {
			latest = t.StartedAt
		}
	}
	return latest
}

// sortTasksByTime 按开始时间排序 task（最旧在前）。
func sortTasksByTime(tasks []*taskpipeline.TaskState) {
	slices.SortFunc(tasks, func(a, b *taskpipeline.TaskState) int {
		return a.StartedAt.Compare(b.StartedAt)
	})
}
