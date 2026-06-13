package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Harness/forge/internal/checklog"
	"github.com/Harness/forge/internal/experience"
	"github.com/Harness/forge/internal/protocol"
	"github.com/Harness/forge/internal/scoring"
	"github.com/Harness/forge/internal/scoringtypes"
	"github.com/Harness/forge/internal/taskcontext"
	"github.com/Harness/forge/internal/taskpipeline"
	"github.com/Harness/forge/internal/toolusage"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskStartCmd)
	taskCmd.AddCommand(taskStatusCmd)
	taskCmd.AddCommand(taskGateCmd)
	taskCmd.AddCommand(taskCompleteCmd)
	taskCmd.AddCommand(taskScoreCmd)
	taskCmd.AddCommand(taskListCmd)

	taskStartCmd.Flags().String("title", "", "任务标题")
	taskStartCmd.Flags().String("ref", "", "任务引用（如 feat/add-auto-branch），默认从分支名推断")
	taskStartCmd.Flags().Bool("branch", false, "从 main/master 创建新分支并切换（ref 作为分支名）")
	taskStartCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskStatusCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskStatusCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskGateCmd.Flags().Bool("silent", false, "静默模式（仅返回退出码）")
	taskGateCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskCompleteCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
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

每个开发任务走 5 道轻量门禁：理解 → 方案 → 实现 → 验证 → 完成。
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

var taskCompleteCmd = &cobra.Command{
	Use:   "complete",
	Short: "标记任务完成（自动评分）",
	RunE:  runTaskComplete,
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

	headCommit, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	state.RecordGateResult(gateID, result.Passed, strings.TrimSpace(string(headCommit)))

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

		// Duplicate HEAD detection: warn if another completed task shares the same commit.
		if state.HeadCommit != "" {
			allStates, listErr := taskpipeline.ListTaskStates(root)
			if listErr == nil {
				for _, s := range allStates {
					if s.TaskRef != state.TaskRef && s.HeadCommit == state.HeadCommit && s.CompletedAt != nil {
						fmt.Fprintf(os.Stderr,
							"⚠ Warning: task %q shares HEAD (%s) with completed task %q — possible duplicate scoring.\n",
							state.TaskRef, state.HeadCommit, s.TaskRef)
					}
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
				// A mandatory review MUST be resolvable (task-verify blocks on a
				// pending mandatory review). If no proposal was generated — empty low
				// dims, or a future dimension without a template — backfill a generic
				// one. AcceptProposal is no longer the only resolve path: `forge
				// experience resolve <task-ref>` is the final escape hatch.
				if mandatory && n == 0 && gerr == nil {
					if fn, ferr := experience.GenerateFallbackProposal(root, state.TaskRef); ferr != nil {
						fmt.Fprintf(os.Stderr, "Warning: fallback proposal generation failed: %v\n", ferr)
					} else {
						n = fn
					}
				}
				if n > 0 && mandatory {
					fmt.Printf("  Generated %d experience proposal(s) — run 'forge experience list' then 'forge experience accept <id>'.\n", n)
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
	} else {
		fmt.Printf("Task %s completed!\n", state.TaskRef)
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
	printToolUsageSummary(state)
	return nil
}

// scoreTask evaluates a completed task and saves the score.
func scoreTask(root string, state *taskpipeline.TaskState) error {
	if state.Score != nil {
		return nil // already scored
	}

	// Collect git data (non-fatal on failure)
	gitDiffTest, gitDiffStat, _ := scoring.CollectGitData(root, state.Branch)

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
	if latestChecks, err := checklog.LatestByCheckForSession(root, state.SessionID); err == nil {
		if entry, ok := latestChecks[checklog.CheckAssertion]; ok {
			assertionChecked = entry.Checked
			assertionPassed = entry.Passed
		}
		if entry, ok := latestChecks[checklog.CheckAutoCompile]; ok {
			compileChecked = entry.Checked
			compilePassed = entry.Passed
		}
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

	// Collect tool usage data for scoring.
	var toolCalls []toolusage.ToolCall
	if calls, err := toolusage.LoadForTask(root, state.TaskRef); err == nil {
		toolCalls = calls
	}
	apViolations := toolusage.DetectAntiPatterns(toolCalls, toolusage.DefaultAntiPatterns)
	skillHits := toolusage.DetectSkills(toolCalls)
	toolCounts := toolusage.ToolCounts(toolCalls)
	recommendedSkills := 2 // forge-quality + at least one task-related skill

	// Convert to scoring types.
	var scoringAPs []scoring.AntiPatternHit
	for _, v := range apViolations {
		scoringAPs = append(scoringAPs, scoring.AntiPatternHit{
			RuleID:     v.RuleID,
			ToolName:   v.ToolName,
			PreferTool: v.PreferTool,
			Severity:   v.Severity,
			Detail:     v.Detail,
		})
	}
	var scoringSHs []scoring.SkillHitData
	for _, h := range skillHits {
		scoringSHs = append(scoringSHs, scoring.SkillHitData{
			SkillName: h.SkillName,
			Source:    h.Source,
		})
	}

	input := &scoring.EvaluateInput{
		GateHistory: scoring.GateHistory{
			TotalGates: len(taskpipeline.DefaultGates()),
			Passed:     len(state.CompletedGates()),
			Retries:    retries,
		},
		StartedAt:         state.StartedAt,
		CompletedAt:       completedAt,
		GitDiffTest:       gitDiffTest,
		GitDiffStat:       gitDiffStat,
		CompilePassed:     compilePassed,
		CompileChecked:    compileChecked,
		AssertionPassed:   assertionPassed,
		AssertionChecked:  assertionChecked,
		ToolCalls:         len(toolCalls),
		AntiPatterns:      scoringAPs,
		SkillHits:         scoringSHs,
		RecommendedSkills: recommendedSkills,
		ToolCounts:        toolCounts,
	}

	result := scoring.Evaluate(input, config)
	result.TaskRef = state.TaskRef

	// Save tool usage summary in task state.
	state.ToolUsage = &toolusage.ToolUsageSummary{
		TotalCalls:   len(toolCalls),
		ToolCounts:   toolCounts,
		AntiPatterns: apViolations,
		SkillHits:    skillHits,
	}

	state.Score = result
	return taskpipeline.SaveTaskState(root, state)
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
