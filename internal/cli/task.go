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
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/scoring"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
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

	taskStartCmd.Flags().String("title", "", "任务标题")
	// StringArray（非 StringSlice）：cobra/pflag 的 StringSlice 默认按逗号切分，会把
	// 含逗号的命令拆坏；StringArray 每个 --accept 整条不切。验收标准是完整 "run :: expected" 串。
	taskStartCmd.Flags().StringArray("accept", nil, `验收标准（可重复 --accept）：格式 "run :: expected"（expected=输出子串）或裸 "run"（只看退出码 0）。forge task verify-acceptance 实跑回扣`)
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

	// Resolve the Claude Code session id once — used to scope the active-task-ref
	// and session records so concurrent sessions on a shared checkout stay isolated.
	sid := taskpipeline.CurrentSessionID()

	// Ensure session exists and link task to it.
	session, err := taskpipeline.EnsureSession(root, sid)
	if err == nil {
		state.SessionID = session.SessionID
	}

	// Phase 爆炸检测：同 session 下已有多个未完成 task 时提醒合并（advisory）。
	if session != nil {
		if w := phaseExplosionWarning(root, state.SessionID, ctx.TaskRef); w != "" {
			fmt.Fprintln(os.Stderr, w)
		}
	}

	// Clear check log for fresh task.
	checklog.Clear(root)
	toolusage.Clear(root)

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

	return nil
}

// runTaskAbort removes a task without scoring it. It deletes the task state file
// (.forge/tasks/<ref>.json) and clears the session-scoped active-task-ref when it
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

		// Low-score review detection
		create, mandatory := experience.ShouldReview(state.Score.Overall)

		// Upgrade to mandatory review if critical hooks were missing and score is below B.
		if hasMissingHooks && state.Score.Overall < 80 {
			create = true
			mandatory = true
		}

		if create {
			if err := experience.CreateReview(root, state.TaskRef, state.Score); err != nil {
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
					n, gerr = experience.GenerateProposalsForReview(root, state.TaskRef, lowDims)
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
					if fn, ferr := experience.GenerateFallbackProposal(root, state.TaskRef); ferr != nil {
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
					if an, aerr := experience.AutoAcceptHighConfidence(root, state.TaskRef, lowDims); aerr != nil {
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
		if review, ok := experience.PendingMandatory(root, state.TaskRef); ok {
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

	// Collect git data (non-fatal on failure)
	gitDiffStat, _ := scoring.CollectGitData(root, state.Branch, state.HeadCommit)

	// Determine hook results from gate history and check log.
	compilePassed := false
	compileChecked := false
	assertionPassed := false
	assertionChecked := false
	for _, r := range state.History {
		if r.Gate == "task-implement" {
			compileChecked = true
			compilePassed = r.Passed
		}
	}

	// Read check log for actual hook results (more reliable than gate history).
	// Scope by session so a concurrent session's check results don't contaminate
	// this task's scoring.
	// covered/total/passed 始终来自实时 CheckTestCoverage（客观，与门禁同输入同逻辑 → 必
	// 一致）。旧路径只从 checklog 读二值 passed，无法支撑 testing 维度的连续打分；实时算
	// 既准确又与门禁 verdict 一致（同 CheckTestCoverage 逻辑、同 task diff）。
	tcOK, tcMissing, tcTotal := taskpipeline.CheckTestCoverage(root, state)
	tcCovered := tcTotal - len(tcMissing)
	testCoveragePassed := tcOK
	// checked：门禁是否跑过（checklog 有 test-coverage-gate 条目）。无条目 → fallback 视为
	// checked（实时已算 covered/total/passed，评分仍可信）。
	testCoverageChecked := false
	if latestChecks, err := checklog.LatestByCheckForSession(root, state.SessionID); err == nil {
		if entry, ok := latestChecks[checklog.CheckAssertion]; ok {
			assertionChecked = entry.Checked
			assertionPassed = entry.Passed
		}
		if entry, ok := latestChecks[checklog.CheckAutoCompile]; ok {
			compileChecked = entry.Checked
			compilePassed = entry.Passed
		}
		if entry, ok := latestChecks[taskpipeline.CheckNameTestCoverage]; ok {
			testCoverageChecked = entry.Checked
		}
	}
	if !testCoverageChecked {
		testCoverageChecked = true
	}

	// Count retries: gates that appear multiple times with mixed results
	retries := 0
	gateAttempts := make(map[string][]bool)
	for _, r := range state.History {
		gateAttempts[r.Gate] = append(gateAttempts[r.Gate], r.Passed)
	}
	for _, attempts := range gateAttempts {
		hasFailure := false
		for _, passed := range attempts {
			if !passed {
				hasFailure = true
			}
		}
		if hasFailure && len(attempts) > 1 {
			retries++
		}
	}

	// Load scoring config from protocol
	var config *scoringtypes.ScoringConfig
	proto, err := protocol.Load(root)
	if err != nil || proto == nil || proto.Scoring == nil {
		config = &scoringtypes.ScoringConfig{
			Weights:    scoringtypes.DefaultWeights(),
			Thresholds: scoringtypes.DefaultThresholds(),
		}
	} else {
		config = proto.Scoring
	}

	completedAt := time.Now()
	if state.CompletedAt != nil {
		completedAt = *state.CompletedAt
	}

	// 断言密度（C）：统计本任务 changed 测试文件的断言数，供 testing 维度假测试检测。
	testAssertionCount, testFileCount := scoring.CollectAssertionDensity(root, state.Branch, state.HeadCommit)

	// 证据链来源分布（路线 Step 2）：从 checklog 聚合 deterministic/agent-claim，
	// 供 ScoreResult.Evidence 可观测（不参与打分）。ForTask 与 forge trace 同源。
	evDeterministic, evAgentClaim := 0, 0
	if ec, err := checklog.ForTask(root, state.TaskRef); err == nil {
		evDeterministic = ec.Deterministic
		evAgentClaim = ec.AgentClaim
	}

	input := &scoring.EvaluateInput{
		GateHistory: scoring.GateHistory{
			TotalGates: len(taskpipeline.DefaultGates()),
			Passed:     len(state.CompletedGates()),
			Retries:    retries,
		},
		StartedAt:             state.StartedAt,
		CompletedAt:           completedAt,
		GitDiffStat:           gitDiffStat,
		TestCoveragePassed:    testCoveragePassed,
		TestCoverageChecked:   testCoverageChecked,
		TestCoverageCovered:   tcCovered,
		TestCoverageTotal:     tcTotal,
		TestAssertionCount:    testAssertionCount,
		TestFileCount:         testFileCount,
		CompilePassed:         compilePassed,
		CompileChecked:        compileChecked,
		AssertionPassed:       assertionPassed,
		AssertionChecked:      assertionChecked,
		EvidenceDeterministic: evDeterministic,
		EvidenceAgentClaim:    evAgentClaim,
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
	conc := act.BuildConclusion(state.TaskRef, state.SessionID, state.Score, ec, pass, total, completedAt)
	if err := act.Append(root, &conc); err != nil {
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
