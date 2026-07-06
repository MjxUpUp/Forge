package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/experience"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskStartCmd)
	taskCmd.AddCommand(taskStatusCmd)
	taskCmd.AddCommand(taskGateCmd)
	taskCmd.AddCommand(taskVerifyAcceptanceCmd)
	taskCmd.AddCommand(taskCompleteCmd)
	taskCmd.AddCommand(taskAbortCmd)
	taskCmd.AddCommand(taskScoreCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskScopeCmd)
	taskScopeCmd.AddCommand(taskScopeAddCmd)
	taskScopeCmd.AddCommand(taskScopeShowCmd)

	taskStartCmd.Flags().String("title", "", "任务标题")
	// StringArray（非 StringSlice）：cobra/pflag 的 StringSlice 默认按逗号切分，会把
	// 含逗号的命令拆坏；StringArray 每个 --accept 整条不切。验收标准是完整 "run :: expected" 串。
	taskStartCmd.Flags().StringArray("accept", nil, `验收标准（可重复 --accept）：格式 "run :: expected"（expected=输出子串）或裸 "run"（只看退出码 0）。forge task verify-acceptance 实跑回扣`)
	// PlanScope：开工前声明计划改动的文件白名单（规划前置 → 可度量契约，对应 Copilot Workspace
	// plan / Terraform desired state）。支持精确路径/glob/目录前缀。advisory：实改超出声明记
	// scope-drift（checklog），不阻塞（变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract）。
	taskStartCmd.Flags().StringArray("scope", nil, `计划改动文件白名单（可重复 --scope）：精确路径 internal/cli/task.go / glob internal/cli/*.go / 目录前缀 internal/cli。开工前声明，advisory 检测 scope-drift；中途可用 forge task scope add 追加`)
	// 接续真相源 flags（continuity）：把 goal/plan/发起工具随 task start 持久化进 TaskState，
	// 供 forge task resume 跨会话/跨工具拉回。复用 --scope/--accept 的"start 持久化"模式。
	taskStartCmd.Flags().String("kind", "", "任务类型：code（默认，走 3 道门禁）| generic（不走门禁，调研/设计/纯接续任务，complete 不评分）")
	taskStartCmd.Flags().String("goal", "", "目标叙述（为什么做，可多行；比 title 一行标题更丰富，持久化供 resume 拉回）")
	taskStartCmd.Flags().String("plan-file", "", "计划正文 markdown 文件路径（读取存入 task.Plan，供 resume 拉回）")
	taskStartCmd.Flags().String("origin-tool", "", "发起工具（pi/claude-code/opencode/codex/cursor），默认从环境探测")
	taskStartCmd.Flags().String("parent", "", "父任务 ref（建立子任务→父任务关系，subtask 拆解）")
	taskStartCmd.Flags().String("ref", "", "任务引用（如 feat/add-auto-branch），默认从分支名推断")
	taskStartCmd.Flags().Bool("branch", false, "从 main/master 创建新分支并切换（ref 作为分支名）")
	taskStartCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskStatusCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskStatusCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskGateCmd.Flags().Bool("silent", false, "静默模式（仅返回退出码）")
	taskGateCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskCompleteCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskAbortCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskAbortCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskScoreCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskScoreCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskScoreCmd.Flags().Bool("history", false, "显示所有已完成任务的评分历史")
	taskListCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskListCmd.Flags().Bool("timeline", false, "按会话时间线显示所有任务")
}

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "任务级质量管道管理",
	Long: `forge task 管理任务级质量门禁。

每个开发任务走 3 道门禁：实现（task-implement）→ 验证（task-verify）→ 完成（task-complete）。
任务上下文自动从 git 分支名推断。`,
}

var taskStartCmd = &cobra.Command{
	Use:   "start [--title <title>] [--ref <ref>]",
	Short: "开始任务（自动检测分支上下文）",
	RunE:  runTaskStart,
}

var taskStatusCmd = &cobra.Command{
	Use:   "status [--json]",
	Short: "查看当前任务门禁状态",
	RunE:  runTaskStatus,
}

var taskGateCmd = &cobra.Command{
	Use:   "gate <gate-id> [--silent]",
	Short: "验证单道任务门禁",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskGate,
}

var taskVerifyAcceptanceCmd = &cobra.Command{
	Use:   "verify-acceptance",
	Short: "实跑验收标准并记 deterministic 证据（spec-as-gate）",
	Long: `forge task verify-acceptance 实跑 task start --accept 登记的每条验收标准（Run 命令），
按"退出码 0 + Expected 子串"判定，回填 Passed/Output，并记 checklog:acceptance（deterministic）。
把 dev-workflow Plan 的 "Run: <cmd>, Expected: <out>" 验收标准从 plan 文本变成不可伪造的实跑证据，
对冲 agent 自述"满足验收"但没真跑的盲区。`,
	RunE: runTaskVerifyAcceptance,
}

var taskCompleteCmd = &cobra.Command{
	Use:   "complete",
	Short: "标记任务完成（自动评分）",
	RunE:  runTaskComplete,
}

var taskAbortCmd = &cobra.Command{
	Use:   "abort [--ref <ref>]",
	Short: "中止并删除任务（清理 ghost/卡住任务，不评分）",
	RunE:  runTaskAbort,
}

var taskScoreCmd = &cobra.Command{
	Use:   "score [--json] [--history]",
	Short: "查看任务质量评分",
	RunE:  runTaskScore,
}

var taskListCmd = &cobra.Command{
	Use:   "list [--json]",
	Short: "列出所有任务",
	RunE:  runTaskList,
}

// taskScopeCmd 是 PlanScope 白名单的管理入口（规划前置 → 可度量契约）。
// add：中途追加（对应 Agentless 分层、可修正的定位——规划不是一次锁死）。
// show：查看声明 + 实时 scope-drift（实改态 vs 声明态差集，advisory）。
var taskScopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "管理计划改动白名单（PlanScope，advisory scope-drift）",
}
var taskScopeAddCmd = &cobra.Command{
	Use:   "add <glob> [<glob>...]",
	Short: "追加计划改动文件到白名单（支持中途迭代）",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTaskScopeAdd,
}
var taskScopeShowCmd = &cobra.Command{
	Use:   "show",
	Short: "查看声明的白名单 + 实时 scope-drift",
	RunE:  runTaskScopeShow,
}

// phaseExplosionWarning returns a non-empty warning when the given session already
// holds too many incomplete tasks — the "phase explosion" smell where one plan is
// split into N tasks each running the full gate flow. Advisory only (non-blocking).
// Returns "" when no warning is warranted (fewer than 3, unknown session, or error).
func phaseExplosionWarning(root, sessionID, currentRef string) string {
	if sessionID == "" {
		return ""
	}
	existing, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return ""
	}
	sameSessionActive := 0
	for _, t := range existing {
		if t.SessionID == sessionID && t.CompletedAt == nil && t.TaskRef != currentRef {
			sameSessionActive++
		}
	}
	if sameSessionActive >= 3 {
		return fmt.Sprintf("[forge] WARN: Phase 爆炸风险 — session %s 已有 %d 个并行未完成 task，考虑合并为单任务", sessionID, sameSessionActive)
	}
	return ""
}

// nonGitTaskWarning is the degraded-mode notice printed at `task start` when the
// project is not a git repo. Forge is git-optional by design — gates still pass
// and `complete` still scores — but the agent needs to know which scoring
// dimensions lose fidelity so it doesn't read neutral scores as a broken
// pipeline. Mentions `abort` because a degraded task a user doesn't want to
// pursue is exactly the case abort exists for.
func nonGitTaskWarning() string {
	return "⚠️ 当前项目不是 git 仓库。forge 以降级模式运行：门禁照常通过、任务可完成评分，但以下评分维度将不可用或偏低：\n" +
		"  - 变更范围 (scope)：无 git diff，固定中性分\n" +
		"如需完整质量保障，执行 `git init`（任务流程本身可继续）。任务无法推进或临时放弃时用 `forge task abort --ref <ref>` 清理。"
}

// detectOriginTool 返回任务的发起工具（声明式真相，区别于 SessionRecord.AgentType 的目录探测弱信号）。
// explicit 非空则用之（--origin-tool）；否则从环境探测（CLAUDE_CODE_SESSION_ID → claude-code）。
// 跨工具接续时让 task 记录"谁起的头"，pi/opencode 接续时用 forge task attach 追加自己的 session+工具。
func detectOriginTool(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return "claude-code"
	}
	return ""
}

// completeGenericTask 完成 generic kind 任务（调研/设计/纯接续）：自动标 3 道门禁 passed
//（History 完整供 list/dashboard 显示，但不跑任何检查——ExecuteTaskGate 对 generic 秒过）+
// MarkComplete + 清 active-task-ref。不评分、不创建 review——generic 任务的价值在持久化的
// 接续字段，不在代码质量门禁。
func completeGenericTask(root string, state *taskpipeline.TaskState) error {
	head := taskpipeline.GetHeadCommit(root)
	for _, g := range taskpipeline.DefaultGates() {
		state.RecordGateResult(g.ID, true, head)
	}
	state.MarkComplete()
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}
	sid := taskpipeline.CurrentSessionID()
	if err := taskpipeline.ClearActiveTaskRef(root, sid); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
	}
	return nil
}

// duplicateScoreWarnings returns warning strings for completed tasks on the
// same branch that share state's HeadCommit — a re-score over an identical
// commit range. Cross-branch matches are excluded: independent feature branches
// forked from one master HEAD all record that same HeadCommit at task start,
// but their diffs live on independent branches and don't overlap, so they are
// not duplicates.
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

func runTaskStart(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	explicitRef, _ := cmd.Flags().GetString("ref")
	title, _ := cmd.Flags().GetString("title")
	createBranch, _ := cmd.Flags().GetBool("branch")

	// --branch: create a new branch from main/master and switch to it.
	if createBranch {
		if explicitRef == "" {
			return fmt.Errorf("--branch requires --ref (e.g., --ref feat/add-auto-branch)")
		}
		if err := validateBranchRef(explicitRef); err != nil {
			return fmt.Errorf("invalid branch ref: %w", err)
		}
		detected := taskcontext.Detect(root)
		if !isMainBranch(detected.Branch) {
			return fmt.Errorf("--branch can only be used on main/master (current: %s)", detected.Branch)
		}
		if err := createAndSwitchBranch(root, explicitRef); err != nil {
			return fmt.Errorf("failed to create branch: %w", err)
		}
	}

	var ctx *taskcontext.Context
	if explicitRef != "" {
		detected := taskcontext.Detect(root)
		ctx = &taskcontext.Context{
			Source:     "explicit",
			TaskRef:    explicitRef,
			Branch:     detected.Branch,
			Summary:    title,
			DetectedAt: detected.DetectedAt,
		}
	} else {
		ctx = taskcontext.Detect(root)
		if !ctx.IsSet() {
			return fmt.Errorf("no task context detected (on main/master branch). Use --ref to specify a task reference")
		}
		if title != "" {
			ctx.Summary = title
		}
	}

	// Check if task already exists
	existing, err := taskpipeline.LoadTaskState(root, ctx.TaskRef)
	if err == nil && existing != nil {
		return fmt.Errorf("task %q already exists (started at %s). Use 'forge task status' to check progress",
			ctx.TaskRef, existing.StartedAt.Format("2006-01-02 15:04"))
	}

	state := taskpipeline.NewTaskState(ctx)

	// Record current HEAD for duplicate detection.
	state.HeadCommit = taskpipeline.GetHeadCommit(root)

	// 持久化验收标准（dev-workflow Plan 的 Run+Expected）：spec 不再随 plan 文本飘走，
	// verify-acceptance 据此实跑回扣。空则无验收标准（不影响流程）。
	if acceptRaw, _ := cmd.Flags().GetStringArray("accept"); len(acceptRaw) > 0 {
		state.Acceptance = taskpipeline.ParseAcceptance(acceptRaw)
	}

	// 持久化 PlanScope（开工前声明的计划改动白名单）：把规划前置变成可度量契约，
	// file-sentinel/task-guard 据此 advisory 检测 scope-drift。空则不检测（无声明=无偏差）。
	if scopeRaw, _ := cmd.Flags().GetStringArray("scope"); len(scopeRaw) > 0 {
		state.PlanScope = scopeRaw
	}

	// 接续真相源字段（continuity）：goal/plan/origin-tool 随 task start 持久化，使新会话
	// forge task resume 能秒级拉回完整上下文（不必 parse 靠纪律的 HANDOFF.md）。
	if kind, _ := cmd.Flags().GetString("kind"); kind != "" {
		state.Kind = kind
	}
	if goal, _ := cmd.Flags().GetString("goal"); goal != "" {
		state.Goal = goal
	}
	if planFile, _ := cmd.Flags().GetString("plan-file"); planFile != "" {
		planData, err := os.ReadFile(planFile)
		if err != nil {
			return fmt.Errorf("读取 --plan-file %q 失败: %w", planFile, err)
		}
		state.Plan = string(planData)
	}
	if parent, _ := cmd.Flags().GetString("parent"); parent != "" {
		state.ParentTaskRef = parent
	}
	originTool, _ := cmd.Flags().GetString("origin-tool")
	state.OriginTool = detectOriginTool(originTool)

	// Resolve the Claude Code session id once — used to scope the active-task-ref
	// and session records so concurrent sessions on a shared checkout stay isolated.
	sid := taskpipeline.CurrentSessionID()

	// Ensure session exists and link task to it.
	session, err := taskpipeline.EnsureSession(root, sid)
	if err == nil {
		state.SessionID = session.SessionID
	}
	// 创建方 session 锚定（多向锚定起点；接手方 forge task attach 追加自己的）。必须在
	// EnsureSession 给 state.SessionID 赋值之后——此前 SessionID 仍为空，AddSession 永不被调用，
	// 创建方 session 漏锚定：多向锚定起点丢失，直到有人主动 resume/attach 才出现首条 SessionLink。
	if state.SessionID != "" {
		state.AddSession(state.SessionID, state.OriginTool)
	}

	// Phase 爆炸检测：同 session 下已有多个未完成 task 时提醒合并（advisory）。
	if session != nil {
		if w := phaseExplosionWarning(root, state.SessionID, ctx.TaskRef); w != "" {
			fmt.Fprintln(os.Stderr, w)
		}
	}

	// Clear check log for fresh task. (Clear also prunes over-age checklog/toollog
	// archives per FORGE_LOG_RETENTION_DAYS — see store.go.)
	checklog.Clear(root)
	toolusage.Clear(root)

	// Prune completed task state files older than the retention window, keeping
	// DataDir/tasks/ bounded. Same window as the log archives so a task's metadata
	// and its logs age out together. Best-effort: error here is non-fatal.
	if days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); days > 0 {
		taskpipeline.PruneOldTasks(root, time.Now().AddDate(0, 0, -days))
	}

	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	// Mark as active task (makes hook detection unambiguous).
	// Session-scoped so concurrent sessions don't clobber each other.
	if err := taskpipeline.SetActiveTaskRef(root, sid, ctx.TaskRef); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to set active task ref: %v\n", err)
	}

	// Non-git projects degrade gracefully — gates still pass and `complete`
	// still scores — but git-backed dimensions go neutral and there is no commit
	// for the task to anchor on. This is the signal the code-knowledge-base
	// session lacked: without it, an agent that starts a task in a bare directory
	// has no idea it's in degraded mode and flounders. Stderr so --json stays clean.
	if !taskpipeline.IsGitRepo(root) {
		fmt.Fprintln(os.Stderr, nonGitTaskWarning())
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		output, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	fmt.Printf("Task started: %s\n", ctx.TaskRef)
	fmt.Printf("Branch: %s\n", ctx.Branch)
	if ctx.Summary != "" {
		fmt.Printf("Summary: %s\n", ctx.Summary)
	}
	fmt.Println()
	fmt.Println("Task gates:")
	gates := taskpipeline.DefaultGates()
	for i, g := range gates {
		auto := ""
		if g.Auto {
			auto = " [auto]"
		}
		fmt.Printf("  %d. %s (%s)%s\n", i+1, g.Name, g.ID, auto)
	}
	fmt.Println()
	fmt.Println("Run 'forge task gate <id>' to validate each gate.")

	if state.HasAcceptance() {
		fmt.Println()
		fmt.Printf("验收标准（%d 条，forge task verify-acceptance 实跑回扣）：\n", len(state.Acceptance))
		for i, c := range state.Acceptance {
			exp := c.Expected
			if exp == "" {
				exp = "(退出码 0)"
			}
			fmt.Printf("  %d. %s :: %s\n", i+1, c.Run, exp)
		}
	}

	if len(state.PlanScope) > 0 {
		fmt.Println()
		fmt.Printf("计划改动白名单（%d 条，advisory 检测 scope-drift；中途可 forge task scope add 追加）：\n", len(state.PlanScope))
		for _, s := range state.PlanScope {
			fmt.Printf("  %s\n", s)
		}
	}

	return nil
}

// runTaskAbort removes a task without scoring it. It deletes the task state file
// (DataDir/tasks/<ref>.json) and clears the session-scoped active-task-ref when it
// points at the aborted task. This is the escape hatch for stuck or "ghost"
// tasks that can never progress through the gates — e.g. a task started in a
// non-git project, or one abandoned mid-flight. Unlike `task complete`, abort
// never scores and never creates a review, so the project's quality record is
// not polluted by an abandoned attempt.
//
// The actual code/commit changes the task made are left untouched — abort only
// reclaims the forge state. The task can be re-started later with the same ref.
func runTaskAbort(cmd *cobra.Command, args []string) error {
	explicitRef, _ := cmd.Flags().GetString("ref")
	asJSON, _ := cmd.Flags().GetBool("json")

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	// Resolve which task to abort: explicit --ref wins, else the session's
	// active task. Without either there is nothing to identify.
	taskRef := explicitRef
	if taskRef == "" {
		state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return fmt.Errorf("failed to load task state: %w", err)
		}
		if state != nil {
			taskRef = state.TaskRef
		}
	}
	if taskRef == "" {
		return fmt.Errorf("no task to abort. Specify --ref <task-ref> or run on a branch with an active task")
	}

	// Load before deleting so the report can say whether it was complete and
	// carry the branch for the user's mental model. A missing file is not fatal:
	// a stale active-task-ref may point at an already-gone task, and we still
	// want to clear that dangling pointer.
	var state *taskpipeline.TaskState
	if loaded, err := taskpipeline.LoadTaskState(root, taskRef); err == nil {
		state = loaded
	}

	// Delete the task state file. ENOENT is acceptable (already gone / stale ref).
	if err := taskpipeline.DeleteTaskState(root, taskRef); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete task state: %w", err)
	}

	// Clear the active-task-ref if it still points at the aborted task.
	// Session-scoped so concurrent sessions are not disturbed.
	sid := taskpipeline.CurrentSessionID()
	if ref := taskpipeline.ReadActiveTaskRef(root, sid); ref == taskRef {
		if err := taskpipeline.ClearActiveTaskRef(root, sid); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
		}
	}

	if asJSON {
		out := map[string]any{
			"task_ref": taskRef,
			"aborted":  true,
		}
		if state != nil {
			out["was_complete"] = state.IsComplete()
			out["branch"] = state.Branch
		}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("Task aborted: %s\n", taskRef)
	if state != nil {
		if state.IsComplete() {
			fmt.Printf("Note: task had already passed all gates; its scored state was removed.\n")
		}
		if state.Branch != "" {
			fmt.Printf("Branch: %s (left untouched — abort only removes forge state)\n", state.Branch)
		}
	}
	fmt.Println("Code changes are untouched. Re-start with: forge task start --ref " + taskRef)
	return nil
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
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
	}
	if state == nil {
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
				// Output 仅在 verify-acceptance 实跑后回填——区分"没跑过"(⏳)与"跑过且失败"(❌)。
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

func runTaskGate(cmd *cobra.Command, args []string) error {
	gateID := args[0]
	silent, _ := cmd.Flags().GetBool("silent")
	explicitRef, _ := cmd.Flags().GetString("ref")

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			if silent {
				return nil
			}
			return err
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			if silent {
				return nil
			}
			return fmt.Errorf("failed to load task state: %w", err)
		}
	}
	if state == nil {
		if silent {
			return nil // No task — silent exit (for hook compatibility)
		}
		return fmt.Errorf("no active task. Run 'forge task start' first")
	}

	// Validate gate exists
	gate := taskpipeline.GateByID(gateID)
	if gate == nil {
		return fmt.Errorf("unknown task gate: %s (valid: %s)", gateID, strings.Join(taskpipeline.GateIDs(), ", "))
	}

	result, err := taskpipeline.ExecuteTaskGate(root, gateID, state)
	if err != nil {
		return err
	}

	// Resolve HEAD from the project root (not the cwd forge was invoked from) so
	// the recorded gate commit matches the repo the task tracks. Other git calls
	// in this path use `git -C root`; this one previously omitted the dir, which
	// silently recorded the wrong commit when forge ran from a subdirectory.
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = root
	headCommit, _ := headCmd.Output()
	state.RecordGateResult(gateID, result.Passed, strings.TrimSpace(string(headCommit)))

	// Token 成本熔断（advisory）：task 累计估算 token 超阈值则警示。让 token 计量不止于
	// forge trace 可观测，而是 task gate 推进时的成本上限信号（loop engineering 成本治理）。
	if w, _ := toolusage.TaskTokenBreaker(root, state.TaskRef); w != "" {
		fmt.Fprintf(os.Stderr, "⚠️ [breaker] %s\n", w)
	}

	if state.IsComplete() && result.Passed {
		state.MarkComplete()
	}

	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}

	if !silent {
		if result.Passed {
			fmt.Printf("  ✅ %s — passed\n", gate.Name)
		} else {
			fmt.Printf("  ❌ %s — failed: %s\n", gate.Name, result.Message)
		}
	}

	if !result.Passed {
		return fmt.Errorf("task gate %s failed", gateID)
	}

	return nil
}

// runTaskVerifyAcceptance 实跑任务登记的每条验收标准（task start --accept），按
// "退出码 0 + Expected 子串" 判定，回填 Passed/Output 到 TaskState 并记一条
// checklog:acceptance（deterministic——forge 自己跑命令看结果，不可伪造）。这是把
// dev-workflow Plan 的 "Run: <cmd>, Expected: <out>" 变成不可伪造实跑证据的入口，
// 对冲 agent 自述"满足验收"却没真跑的盲区（spec-as-gate）。失败不阻塞会话，仅返回 error
// 让调用方/脚本感知退出码；Passed 字段如实落盘 + checklog，forge trace 可见。
func runTaskVerifyAcceptance(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	return runTaskVerifyAcceptanceAt(root)
}

// runTaskVerifyAcceptanceAt 是 runTaskVerifyAcceptance 的 root 注入核心，独立出来便于
// 在临时项目上单测（不经 findProjectRoot / cobra）。实跑任务登记的每条验收标准
// （task start --accept），按 "退出码 0 + Expected 子串" 判定，回填 Passed/Output 到
// TaskState 并记一条 checklog:acceptance（deterministic——forge 自己跑命令看结果，不可伪造）。
// 这是把 dev-workflow Plan 的 "Run: <cmd>, Expected: <out>" 变成不可伪造实跑证据的入口，
// 对冲 agent 自述"满足验收"却没真跑的盲区（spec-as-gate）。
func runTaskVerifyAcceptanceAt(root string) error {
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return fmt.Errorf("failed to load task state: %w", err)
	}
	if state == nil {
		return fmt.Errorf("no active task. Run 'forge task start' first")
	}
	if !state.HasAcceptance() {
		fmt.Println("本任务未登记验收标准（forge task start --accept \"run :: expected\"）。")
		return nil
	}

	taskpipeline.VerifyAcceptance(root, state)
	allPassed := state.AllAcceptancePassed()

	checklog.Record(root, &checklog.Entry{
		Check:   taskpipeline.CheckNameAcceptance,
		Passed:  allPassed,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  formatAcceptanceDetail(state.Acceptance),
	})

	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}

	fmt.Println("验收标准实跑结果：")
	for i, c := range state.Acceptance {
		mark := "✅"
		if !c.Passed {
			mark = "❌"
		}
		exp := c.Expected
		if exp == "" {
			exp = "(退出码 0)"
		}
		fmt.Printf("  %s [%d] %s :: %s\n", mark, i+1, c.Run, exp)
		if !c.Passed && c.Output != "" {
			for _, line := range splitLines(c.Output) {
				fmt.Printf("     %s\n", line)
			}
		}
	}
	fmt.Println(strings.Repeat("─", 40))
	if allPassed {
		fmt.Printf("✅ 全部通过 — 真实结果已记为 deterministic 证据（checklog: %s）\n", taskpipeline.CheckNameAcceptance)
		return nil
	}
	fmt.Printf("❌ 存在未通过项 — 失败结果已记入 checklog（%s）\n", taskpipeline.CheckNameAcceptance)
	return fmt.Errorf("acceptance verification failed")
}

// formatAcceptanceDetail 生成 checklog:acceptance 的 Detail 摘要——"PASS/FAIL — k/n 通过"，
// 让 forge trace 不展开每条也能一眼看出验收整体结果。
func formatAcceptanceDetail(cs []taskpipeline.AcceptanceCriterion) string {
	passed := 0
	for _, c := range cs {
		if c.Passed {
			passed++
		}
	}
	word := `FAIL`
	if passed == len(cs) {
		word = `PASS`
	}
	return fmt.Sprintf("%s — %d/%d 验收标准通过", word, passed, len(cs))
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
		// Fallback: task completed all gates but MarkComplete was called,
		// so ActiveTaskState returns nil. Load via branch context.
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

	// Auto-score the task
	if err := scoreTask(root, state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: scoring failed: %v\n", err)
	}

	if state.Score != nil {
		fmt.Printf("Task %s completed! Score: %.0f (%s)\n", state.TaskRef, state.Score.Overall, state.Score.Grade)

		// Duplicate HEAD detection: warn if another completed task on the SAME
		// branch shares the same HeadCommit (a re-score over an identical commit
		// range). Scoped to the same branch to avoid false positives — every
		// feature branch forked from one master HEAD records that same HeadCommit
		// at task start, but their diffs live on independent branches and don't
		// overlap, so cross-branch matches are not duplicates.
		if state.HeadCommit != "" {
			allStates, listErr := taskpipeline.ListTaskStates(root)
			if listErr == nil {
				for _, w := range duplicateScoreWarnings(state, allStates) {
					fmt.Fprintf(os.Stderr, "⚠ Warning: %s\n", w)
				}
			}
		}

		// Missing hooks check: warn if critical quality hooks never ran.
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

		// experience store lives in the user-level DataDir (~/.forge/projects/<key>/).
		// When there is no DataDir (non-git or ProjectFor failure) proj==nil: the
		// review WRITE block below is skipped (guarded by `if create && proj != nil`),
		// and the PendingMandatory READ tolerates nil (returns no pending review).
		// Net effect degrades like act/health — never blocks complete on a project
		// with no DataDir. Non-git projects have no review mechanism anyway.
		proj, perr := forgedata.ProjectFor(root)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "Warning: experience review skipped (project not resolved): %v\n", perr)
		}

		// Low-score review detection
		create, mandatory := experience.ShouldReview(state.Score.Overall)

		// Upgrade to mandatory review if critical hooks were missing and score is below B.
		if hasMissingHooks && state.Score.Overall < 80 {
			create = true
			mandatory = true
		}

		if create && proj != nil {
			if err := experience.CreateReview(proj, state.TaskRef, state.Score); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: review creation failed: %v\n", err)
			} else {
				lowDims := experience.LowDimensions(state.Score)

				// Generate one seed proposal per low-scoring dimension so the review
				// is resolvable via `forge experience accept`. Skip the ListProposals
				// dedup scan when there are no low dims (a mandatory review upgraded
				// due to missing hooks may have none) — the fallback below covers it.
				var n int
				var gerr error
				if len(lowDims) > 0 {
					n, gerr = experience.GenerateProposalsForReview(proj, state.TaskRef, lowDims)
					if gerr != nil {
						fmt.Fprintf(os.Stderr, "Warning: experience proposal generation failed: %v\n", gerr)
					}
				}
				// A mandatory review MUST be resolvable (forge task complete blocks
				// on a pending mandatory review — see the PendingMandatory check
				// below). If no proposal was generated — empty low dims, or a future
				// dimension without a template — backfill a generic one so `forge
				// experience accept` always has a target. AcceptProposal is no longer
				// the only resolve path: `forge experience resolve <task-ref>` is the
				// final escape hatch.
				if mandatory && n == 0 && gerr == nil {
					if fn, ferr := experience.GenerateFallbackProposal(proj, state.TaskRef); ferr != nil {
						fmt.Fprintf(os.Stderr, "Warning: fallback proposal generation failed: %v\n", ferr)
					} else {
						n = fn
					}
				}
				// Auto-accept high-confidence proposals (severe low scores <60) into
				// the global knowledge store. Without this the experience loop was
				// empty in real heavy-use projects: knowledge only grew via manual
				// `forge experience accept`, which no one ran, so ~/.forge/knowledge/
				// never accumulated. Resolving the review here also unblocks the
				// mandatory-review gate below when the low score is unambiguous;
				// borderline (60-69) proposals still wait for a human.
				var autoN int
				if n > 0 {
					if an, aerr := experience.AutoAcceptHighConfidence(proj, state.TaskRef, lowDims); aerr != nil {
						fmt.Fprintf(os.Stderr, "Warning: high-confidence auto-accept failed: %v\n", aerr)
					} else {
						autoN = an
					}
				}
				if n > 0 && mandatory {
					pending := n - autoN
					if autoN > 0 {
						fmt.Printf("  %d high-confidence proposal(s) auto-accepted into knowledge (score<60).\n", autoN)
					}
					if pending > 0 {
						fmt.Printf("  %d borderline proposal(s) pending — run 'forge experience list' then 'forge experience accept <id>'.\n", pending)
					}
				}

				var dimParts []string
				for _, d := range lowDims {
					dimParts = append(dimParts, fmt.Sprintf("%s (%d)", d.Dimension, d.Score))
				}

				if mandatory {
					fmt.Printf("⚠ Task %s scored %s (%.0f) — mandatory review required.\n", state.TaskRef, state.Score.Grade, state.Score.Overall)
					if len(dimParts) > 0 {
						fmt.Printf("  Low dimensions: %s\n", strings.Join(dimParts, ", "))
					}
					fmt.Println("AI agent: analyze root causes and propose experience rules.")
				} else {
					fmt.Printf("💡 Task %s scored %s (%.0f) — review suggested (optional).\n", state.TaskRef, state.Score.Grade, state.Score.Overall)
					if len(dimParts) > 0 {
						fmt.Printf("  Low dimensions: %s\n", strings.Join(dimParts, ", "))
					}
				}
			}
		}

		// Enforce mandatory review resolution. Previously the task-verify Stop
		// hook blocked session end on a pending mandatory review; that hook is
		// now advisory, so the force moved HERE — to task completion. Failing
		// here is a task-level block: we return BEFORE ClearActiveTaskRef, so
		// the active task ref survives and the session is NOT trapped in a
		// stop-retry loop — resolve the review, then re-run complete.
		if review, ok := experience.PendingMandatory(proj, state.TaskRef); ok {
			fmt.Fprintf(os.Stderr, "\n❌ Task %s cannot complete: mandatory review (score %.0f/%s) is still pending.\n",
				state.TaskRef, review.Score, review.Grade)
			fmt.Fprintf(os.Stderr, "   Resolve it first:\n")
			fmt.Fprintf(os.Stderr, "     forge experience list\n")
			fmt.Fprintf(os.Stderr, "     forge experience accept <id>   # or: forge experience resolve %s\n", state.TaskRef)
			fmt.Fprintf(os.Stderr, "   Then re-run: forge task complete --ref %s\n", state.TaskRef)
			return fmt.Errorf("task %s blocked by pending mandatory review (resolve it to complete)", state.TaskRef)
		}
	} else {
		fmt.Printf("Task %s completed!\n", state.TaskRef)
	}

	// Act 反馈臂（PDCA Act）：构建证据驱动结论落盘，喂给 session-retrospective。必须在
	// PendingMandatory 阻塞检查之后调用——否则被挂起强制评审阻塞时 operator 重跑 complete
	// 会重复追加结论（每次重跑一行）。即使评分失败也建（证据强度不依赖分数）；Nudge 时打印
	// 一行回顾指令（stderr，stdout --json 保持干净）。
	if d := appendConclusion(root, state); d != "" {
		fmt.Fprintln(os.Stderr, d)
	}

	// Clear active task ref — task is done (session-scoped)
	if err := taskpipeline.ClearActiveTaskRef(root, taskpipeline.CurrentSessionID()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
	}

	return nil
}

// checkMissingHooks returns the names of critical quality hooks that never
// executed during this task (based on checklog entries and gate history).
func checkMissingHooks(root string, state *taskpipeline.TaskState) []string {
	var missing []string

	latestChecks, err := checklog.LatestByCheckForSession(root, state.SessionID)
	if err != nil || latestChecks == nil {
		// Can't read checklog — assume all hooks are missing unless gate history shows otherwise.
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
		// Check if compilation was run via task-implement gate instead.
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

// runTaskScopeAdd 把 glob 追加到当前任务的 PlanScope（去重）。支持中途迭代——规划不是
// task start 一次锁死：Agentless 的分层定位、Copilot Workspace 的"重新考虑改哪些文件"
// 都印证 scope 是演进的。持久化后立即生效（后续 hook 据此 advisory 检测 drift）。
func runTaskScopeAdd(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
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
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}
	fmt.Printf("PlanScope 现共 %d 条（本次新增 %d）：\n", len(state.PlanScope), added)
	for _, s := range state.PlanScope {
		fmt.Printf("  %s\n", s)
	}
	return nil
}

// runTaskScopeShow 打印声明的 PlanScope + 实时 scope-drift（实改源码 vs 声明的差集）。
// drift 全程 advisory：变更影响分析召回率仅 ~44%（PASTE），scope 是 prediction 非 contract，
// 偏差是常态信号——这里只是把它从隐性变成可度量、可回顾，绝不阻塞。
func runTaskScopeShow(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
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

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	// Show history of all scored tasks
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

	// Load single task
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
		// Fallback: completed tasks are not active but can still be scored.
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

	// Score if not yet scored
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
	return nil
}

// scoreTask evaluates a completed task and saves the score.
func scoreTask(root string, state *taskpipeline.TaskState) error {
	if state.Score != nil {
		return nil // already scored
	}

	input, config, err := buildEvaluateInput(root, state)
	if err != nil {
		return fmt.Errorf(`build evaluate input: %w`, err)
	}

	result := scoring.Evaluate(input, config)
	result.TaskRef = state.TaskRef

	state.Score = result
	return taskpipeline.SaveTaskState(root, state)
}

// appendConclusion 构建 + 落盘一个完成任务的 Act 结论（证据驱动），返回 Directive
// （无 RetrospectiveNudge 时为空串，调用方据非空决定是否打印）。聚合 checklog.ForTask
// 的证据链 + state.Acceptance 的通过率 + state.Score，调 act.BuildConclusion——解耦于
// taskpipeline（act 包不依赖它，本处从 state 提取原始值传入）。
func appendConclusion(root string, state *taskpipeline.TaskState) string {
	ec, _ := checklog.ForTask(root, state.TaskRef)
	pass, total := 0, len(state.Acceptance)
	for _, c := range state.Acceptance {
		if c.Passed {
			pass++
		}
	}
	completedAt := time.Now()
	if state.CompletedAt != nil {
		completedAt = *state.CompletedAt
	}
	conc := act.BuildConclusion(state.TaskRef, state.SessionID, state.Score, ec, pass, total, completedAt, phaseKeys(state.DesignPhases))
	proj, perr := forgedata.ProjectFor(root)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "Warning: act conclusion append skipped (project not resolved): %v\n", perr)
		return conc.Directive()
	}
	if err := act.Append(proj, &conc); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: act conclusion append failed: %v\n", err)
	}
	return conc.Directive()
}

func runTaskList(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
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

// runTaskTimeline groups tasks by session and displays an ASCII timeline.
func runTaskTimeline(root string, states []*taskpipeline.TaskState) error {
	sessions, err := taskpipeline.LoadSessions(root)
	if err != nil {
		// Fall back to simple flat list if sessions can't be loaded.
		fmt.Println("Task Timeline (session data unavailable):")
		fmt.Println(strings.Repeat("─", 60))
		for _, s := range states {
			printTaskLine(s)
		}
		return nil
	}

	// Build session -> tasks index
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

	// Print sessions in chronological order
	for _, sess := range sessions {
		tasks, ok := sessionTasks[sess.SessionID]
		if !ok || len(tasks) == 0 {
			continue
		}

		// Session header
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

		// Sort tasks by start time within session
		sortTasksByTime(tasks)

		for j, t := range tasks {
			prefix := "  ├──"
			if j == len(tasks)-1 {
				prefix = "  └──"
			}
			printTaskTreeLine(prefix, t)
		}
	}

	// Print orphan tasks (no session association)
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

// printTaskLine prints a single task in flat format.
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

// printTaskTreeLine prints a single task in tree format.
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

// findLatestTaskTime returns the most recent time from a set of tasks.
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

// sortTasksByTime sorts tasks by start time (oldest first).
func sortTasksByTime(tasks []*taskpipeline.TaskState) {
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if tasks[i].StartedAt.After(tasks[j].StartedAt) {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}
}

// validateBranchRef ensures the ref is a valid conventional branch name.
func validateBranchRef(ref string) error {
	validPrefixes := []string{
		"feat/", "feature/", "fix/", "bugfix/", "hotfix/",
		"refactor/", "test/", "chore/", "docs/", "ci/",
		"perf/", "build/", "style/",
	}
	for _, p := range validPrefixes {
		if strings.HasPrefix(ref, p) && len(ref) > len(p) {
			return nil
		}
	}
	return fmt.Errorf("must start with a conventional prefix (feat/, fix/, refactor/, test/, chore/, docs/, ci/, perf/, build/, style/)")
}

// isMainBranch checks if a branch name is a main/master branch.
func isMainBranch(branch string) bool {
	lower := strings.ToLower(branch)
	return lower == "main" || lower == "master"
}

// createAndSwitchBranch creates a new git branch and switches to it.
func createAndSwitchBranch(root, name string) error {
	cmd := exec.Command("git", "checkout", "-b", name)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// phaseKeys converts taskpipeline.DesignPhase slice to string slice for act.Conclusion.
func phaseKeys(phases []taskpipeline.DesignPhase) []string {
	if len(phases) == 0 {
		return nil
	}
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = string(p)
	}
	return out
}
