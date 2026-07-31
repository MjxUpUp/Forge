package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
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
	taskCmd.AddCommand(taskOverrideCmd)
	taskScopeCmd.AddCommand(taskScopeAddCmd)
	taskScopeCmd.AddCommand(taskScopeShowCmd)

	taskStartCmd.Flags().String("title", "", "任务标题")
	// StringArray (not StringSlice): cobra/pflag's StringSlice splits on commas by default,
	// which would break commands containing commas; StringArray keeps each --accept intact.
	// Acceptance criteria are full 'run :: expected' strings.
	//
	// StringArray（非 StringSlice）：cobra/pflag 的 StringSlice 默认按逗号切分，会把
	// 含逗号的命令拆坏；StringArray 每个 --accept 整条不切。验收标准是完整"run :: expected"串。
	taskStartCmd.Flags().StringArray("accept", nil, `验收标准（可重复 --accept）：格式 "run :: expected"（expected=输出子串）或裸 "run"（只看退出码 0）。forge task verify-acceptance 实跑回扣`)
	// PlanScope: declare the whitelist of files planned to change before starting work
	// (planning up-front -> measurable contract). Supports exact paths/globs/directory
	// prefixes. Advisory: changes beyond the declaration are recorded as scope-drift
	// (checklog), not blocking (change-impact-analysis recall is only ~44%, scope is a
	// prediction not a contract).
	//
	// PlanScope：开工前声明计划改动的文件白名单（规划前置 → 可度量契约）。
	// 支持精确路径/glob/目录前缀。advisory：实改超出声明记
	// scope-drift（checklog），不阻塞（变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract）。
	taskStartCmd.Flags().StringArray("scope", nil, `计划改动文件白名单（可重复 --scope）：精确路径 internal/cli/task.go / glob internal/cli/*.go / 目录前缀 internal/cli。开工前声明，advisory 检测 scope-drift；中途可用 forge task scope add 追加`)
	// Continuity flags: persist goal/plan/origin-tool into TaskState at task start,
	// so forge task resume can pull them back across sessions/tools. Reuses the
	// 'start persists' pattern of --scope/--accept.
	//
	// 接续真相源 flags（continuity）：把 goal/plan/发起工具随 task start 持久化进 TaskState，
	// 供 forge task resume 跨会话/跨工具拉回。复用 --scope/--accept 的「start 持久化」模式。
	taskStartCmd.Flags().String("kind", "", "任务类型：code（默认，走 3 道门禁）| generic（不走门禁，调研/设计/纯接续任务，complete 不评分）")
	taskStartCmd.Flags().String("goal", "", "目标叙述（为什么做，可多行；比 title 一行标题更丰富，持久化供 resume 拉回）")
	taskStartCmd.Flags().String("plan-file", "", "计划正文 markdown 文件路径（读取存入 task.Plan，供 resume 拉回）")
	taskStartCmd.Flags().String("origin-tool", "", "发起工具（pi/claude-code/opencode/codex/cursor），默认从环境探测")
	taskStartCmd.Flags().String("parent", "", "父任务 ref（建立子任务→父任务关系，subtask 拆解）")
	taskStartCmd.Flags().String("ref", "", "任务引用（如 feat/add-auto-branch），默认从分支名推断")
	taskStartCmd.Flags().String("from-issue", "", "外部 issue URL（linear/github），解析为 task.ExternalOrigin 锚定外部 issue（衔接 spawn 式编排器）")
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
	taskOverrideCmd.Flags().String("ref", "", "任务 ref（默认当前活跃任务）")
	taskOverrideCmd.Flags().String("work-activity", "", "设为 disable 跳过 read-before-edit/work-activity 门禁")
	taskOverrideCmd.Flags().String("test-coverage", "", "设为 disable 跳过 test-coverage 门禁")
	taskOverrideCmd.Flags().String("acceptance-gate", "", `设为 disable 跳过 task-complete 的 acceptance pre-flight 门禁`)
	taskOverrideCmd.Flags().String("skill-decisions", "", `设为 disable 跳过 skill-decisions guardrail（改 SKILL.md 必须记决策）`)
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

// taskScopeCmd is the management entry for the PlanScope whitelist (planning
// up-front -> measurable contract).
// add: append mid-task (layered, correctable positioning — planning is not locked in once).
// show: view the declaration + live scope-drift (diff between actual-changed and declared,
// advisory).
//
// taskScopeCmd 是 PlanScope 白名单的管理入口（规划前置 → 可度量契约）。
// add：中途追加（分层、可修正的定位——规划不是一次锁死）。
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
var taskOverrideCmd = &cobra.Command{
	Use:   `override [--work-activity disable] [--test-coverage disable] [--acceptance-gate disable] [--skill-decisions disable]`,
	Short: "设置 per-task 逃生舱（优先全局 env，不污染他任务；用了降强度到 Weak）",
	RunE:  runTaskOverride,
}

// phaseExplosionWarning returns a non-empty warning when the given session already
// has too many incomplete tasks — the 'phase explosion' anti-pattern (one plan split
// into N tasks each running the full gate pipeline). Advisory only (non-blocking).
// Returns '' when no warning is needed (fewer than 3, unknown session, or on error).
//
// phaseExplosionWarning 在指定 session 已有过多未完成 task 时返回非空告警——
// 即「phase 爆炸」反模式（一个 plan 拆成 N 个 task 各跑全套门禁）。仅 advisory
// （不阻塞）。无需告警时返 ""（少于 3、未知 session 或出错）。
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

// nonGitTaskWarning is the degraded-mode notice printed by 'task start' when the
// project is not a git repo. Forge is git-optional by design — gates still pass,
// 'complete' still scores — but the agent must know which scoring dimensions are
// distorted, lest it read a neutral score as a broken pipeline. Mentions 'abort':
// a degraded task the user wants to drop is exactly the scenario abort exists for.
//
// nonGitTaskWarning 是项目非 git 仓库时 `task start` 打印的降级模式提示。
// forge 设计上 git-optional——门禁照常过、`complete` 照常评分——但 agent 须
// 知道哪些评分维度失真，免得把中性分读成管道坏了。提到 `abort`：用户不想继续
// 的降级任务正是 abort 存在的场景。
func nonGitTaskWarning() string {
	return "⚠️ 当前项目不是 git 仓库。forge 以降级模式运行：门禁照常通过、任务可完成评分，但以下评分维度将不可用或偏低：\n" +
		"  - 变更范围 (scope)：无 git diff，固定中性分\n" +
		"如需完整质量保障，执行 `git init`（任务流程本身可继续）。任务无法推进或临时放弃时用 `forge task abort --ref <ref>` 清理。"
}

// detectOriginTool returns the task's origin tool (declarative truth, distinct from
// the directory-snooping weak signal of SessionRecord.AgentType). If explicit is
// non-empty it is used (--origin-tool); otherwise the environment is probed
// (CLAUDE_CODE_SESSION_ID -> claude-code). Lets the task record 'who started it'
// across tool handoffs; pi/opencode append their own session+tool via forge task attach.
//
// detectOriginTool 返回任务的发起工具（声明式真相，区别于 SessionRecord.AgentType 的目录探测弱信号）。
// explicit 非空则用之（--origin-tool）；否则从环境探测（CLAUDE_CODE_SESSION_ID → claude-code）。
// 跨工具接续时让 task 记录「谁起的头」，pi/opencode 接续时用 forge task attach 追加自己的 session+工具。
func detectOriginTool(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return "claude-code"
	}
	return ""
}

// completeGenericTask finishes a generic-kind task (research/design/pure handoff):
// auto-marks the 3 gates as passed (History stays complete for list/dashboard display,
// but no checks actually run — ExecuteTaskGate short-circuits for generic) + MarkComplete
// + clears the active-task-ref. Does not score and creates no review — the value of a
// generic task lives in its persisted continuity fields, not in code-quality gates.
//
// completeGenericTask 完成 generic kind 任务（调研/设计/纯接续）：自动标 3 道门禁 passed
// （History 完整供 list/dashboard 显示，但不跑任何检查——ExecuteTaskGate 对 generic 秒过）+
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

// duplicateScoreWarnings returns the warning strings for completed tasks on the same
// branch that share a HeadCommit with state — i.e. re-scoring over the same commit
// range. Cross-branch matches are not counted: independent feature branches pulled
// from the same master HEAD each record the same HeadCommit at task start, but their
// diffs live on separate branches and do not overlap, so they are not duplicates.
//
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

func runTaskStart(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	explicitRef, _ := cmd.Flags().GetString("ref")
	title, _ := cmd.Flags().GetString("title")
	createBranch, _ := cmd.Flags().GetBool("branch")

	// --branch: create a new branch from main/master and switch to it.
	//
	// --branch：从 main/master 创建新分支并切过去。
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

	// Check whether the task already exists.
	//
	// 检查 task 是否已存在
	existing, err := taskpipeline.LoadTaskState(root, ctx.TaskRef)
	if err == nil && existing != nil {
		return fmt.Errorf("task %q already exists (started at %s). Use 'forge task status' to check progress",
			ctx.TaskRef, existing.StartedAt.Format("2006-01-02 15:04"))
	}

	state := taskpipeline.NewTaskState(ctx)

	// Record current HEAD for duplicate detection.
	//
	// 记录当前 HEAD 用于重复检测。
	state.HeadCommit = taskpipeline.GetHeadCommit(root)

	// Persist acceptance criteria (dev-workflow Plan's Run+Expected): the spec no
	// longer drifts with plan text; verify-acceptance replays them against actual runs.
	// Empty means no acceptance criteria (does not affect flow).
	//
	// 持久化验收标准（dev-workflow Plan 的 Run+Expected）：spec 不再随 plan 文本飘走，
	// verify-acceptance 据此实跑回扣。空则无验收标准（不影响流程）。
	if acceptRaw, _ := cmd.Flags().GetStringArray("accept"); len(acceptRaw) > 0 {
		state.Acceptance = taskpipeline.ParseAcceptance(acceptRaw)
	}

	// Persist PlanScope (the planned-change whitelist declared before starting work):
	// turns up-front planning into a measurable contract; file-sentinel/task-guard use
	// it to advisory-detect scope-drift. Empty means no detection (no declaration = no drift).
	//
	// 持久化 PlanScope（开工前声明的计划改动白名单）：把规划前置变成可度量契约，
	// file-sentinel/task-guard 据此 advisory 检测 scope-drift。空则不检测（无声明=无偏差）。
	if scopeRaw, _ := cmd.Flags().GetStringArray("scope"); len(scopeRaw) > 0 {
		state.PlanScope = scopeRaw
	}

	// Continuity fields: goal/plan/origin-tool are persisted at task start so a fresh
	// session's 'forge task resume' can pull the full context back in seconds (no need
	// to parse a discipline-dependent HANDOFF.md).
	//
	// 接续真相源字段（continuity）：goal/plan/origin-tool 随 task start 持久化，使新会话
	// forge task resume 能秒级拉回完整上下文（不必 parse 靠纪律的 HANDOFF.md）。
	if kind, _ := cmd.Flags().GetString("kind"); kind != "" {
		state.Kind = kind
	}
	if goal, _ := cmd.Flags().GetString("goal"); goal != "" {
		state.Goal = goal
	}
	// planAcceptanceAdded: the net count of entries actually added from --plan-file
	// after extraction (net of duplicates deduped against explicit --accept), used only
	// to annotate the source in the success output below. Uses the len delta after merge
	// rather than len(extracted) before — the latter would count entries discarded by
	// dedup when --accept coexists, misleading users into thinking they entered state.
	//
	// planAcceptanceAdded：--plan-file 提取后实际新增入库的条数（净增，扣除与显式 --accept
	// 去重的部分），仅供下方成功输出标注来源。用 merge 后的 len 差值而非提取前的 len(extracted)——
	// 后者在 --accept 共存时会数进去被去重丢弃的条目，误导用户以为它们进了 state。
	planAcceptanceAdded := 0
	if planFile, _ := cmd.Flags().GetString("plan-file"); planFile != "" {
		planData, err := os.ReadFile(planFile)
		if err != nil {
			return fmt.Errorf("读取 --plan-file %q 失败: %w", planFile, err)
		}
		state.Plan = string(planData)
		// Auto-extract acceptance criteria from Plan markdown (Run:/Expected: blocks),
		// closing the gap of hand-copying plan's Run/Expected into --accept (dogfood:
		// self-disciplined hand-copying always leaks; uncopied criteria leave the
		// acceptance advisory silent). Explicit --accept wins; plan extraction fills in
		// by Run-dedup (MergeAcceptance).
		//
		// 从 Plan markdown 自动提取验收标准（Run:/Expected: 块），消除把 plan 的 Run/Expected
		// 手抄到 --accept 的断口（dogfood：靠自觉手抄必漏；没抄时 acceptance advisory 零信号）。
		// 显式 --accept 优先，plan 提取按 Run 去重补充（MergeAcceptance）。
		if extracted := taskpipeline.ParseAcceptanceFromPlan(state.Plan); len(extracted) > 0 {
			baseBefore := len(state.Acceptance)
			state.Acceptance = taskpipeline.MergeAcceptance(state.Acceptance, extracted)
			planAcceptanceAdded = len(state.Acceptance) - baseBefore
		}
	}
	if parent, _ := cmd.Flags().GetString("parent"); parent != "" {
		state.ParentTaskRef = parent
	}
	originTool, _ := cmd.Flags().GetString("origin-tool")
	state.OriginTool = detectOriginTool(originTool)

	// External issue origin: extends the task's source from branch to external issue
	// (linear/github), bridging spawn-style orchestrators (Symphony-like) — when the
	// orchestrator spawns a run, the task is naturally linked to the issue, not via branch inference.
	//
	// 外部 issue origin：把 task 的来源从 branch 扩展到外部 issue（linear/github），
	// 衔接 spawn 式编排器（Symphony 类）——编排器拉起 run 时 task 天然关联 issue，不靠 branch 推断。
	if fromIssue, _ := cmd.Flags().GetString("from-issue"); fromIssue != "" {
		state.ExternalOrigin = taskpipeline.ParseExternalOriginURL(fromIssue)
	}

	// Take a Claude Code session id once — used to scope active-task-ref and session
	// records so concurrent sessions on a shared checkout stay isolated.
	//
	// 取一次 Claude Code session id——用于 scope active-task-ref 与 session record，
	// 让共享 checkout 上的并发 session 保持隔离。
	sid := taskpipeline.CurrentSessionID()

	// Ensure the session exists and link the task to it.
	//
	// 确保 session 存在并把 task 链上去。
	session, err := taskpipeline.EnsureSession(root, sid)
	if err == nil {
		state.SessionID = session.SessionID
	}
	// Creator session anchoring (the starting point of multi-directional anchoring;
	// successors append their own via forge task attach). Must run after EnsureSession
	// assigns state.SessionID — before that, SessionID is still empty, AddSession is
	// never called, and the creator session is left unanchored: the multi-directional
	// starting point is lost until someone actively resumes/attaches and the first
	// SessionLink finally appears.
	//
	// 创建方 session 锚定（多向锚定起点；接手方 forge task attach 追加自己的）。必须在
	// EnsureSession 给 state.SessionID 赋值之后——此前 SessionID 仍为空，AddSession 永不被调用，
	// 创建方 session 漏锚定：多向锚定起点丢失，直到有人主动 resume/attach 才出现首条 SessionLink。
	if state.SessionID != "" {
		state.AddSession(state.SessionID, state.OriginTool)
	}

	// Phase-explosion detection: warn about merging when the same session already has
	// many incomplete tasks (advisory).
	//
	// Phase 爆炸检测：同 session 下已有多个未完成 task 时提醒合并（advisory）。
	if session != nil {
		if w := phaseExplosionWarning(root, state.SessionID, ctx.TaskRef); w != "" {
			fmt.Fprintln(os.Stderr, w)
		}
	}

	// Clear checklog for the new task. (Clear also prunes expired checklog/toollog
	// archives per FORGE_LOG_RETENTION_DAYS — see store.go.)
	//
	// 为新 task 清 checklog。（Clear 也会按 FORGE_LOG_RETENTION_DAYS 清理超期 checklog/toollog
	// 归档——见 store.go。）Clear 失败只 stderr 告警（不阻断 task start），但必须留痕——
	// 静默失败会让新任务继承上一任务的证据链。
	if cerr := checklog.Clear(root); cerr != nil {
		fmt.Fprintf(os.Stderr, "warn: 清空 checklog 失败（新任务可能混入上一任务证据）: %v\n", cerr)
	}
	if cerr := toolusage.Clear(root); cerr != nil {
		fmt.Fprintf(os.Stderr, "warn: 清空 toollog 失败（新任务可能混入上一任务工具记录）: %v\n", cerr)
	}

	// Prune completed task-state files beyond the retention window to keep
	// DataDir/tasks/ bounded. Same window as log archival, so task metadata is
	// retired in lockstep with its logs. Best-effort: errors here are not fatal.
	//
	// 清理超过 retention 窗口的已完成 task state 文件，保持 DataDir/tasks/ 有界。
	// 与 log 归档同窗口，让 task 元数据与其 log 同步淘汰。best-effort：此处错误不致命。
	if days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); days > 0 {
		taskpipeline.PruneOldTasks(root, time.Now().AddDate(0, 0, -days))
	}

	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	// Mark as active task (so hook detection is unambiguous).
	// Session-scoped — concurrent sessions do not clobber each other.
	//
	// 标记为 active task（让 hook 检测无歧义）。
	// session-scoped，并发 session 不会互相覆盖。
	if err := taskpipeline.SetActiveTaskRef(root, sid, ctx.TaskRef); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to set active task ref: %v\n", err)
	}

	// Graceful degradation for non-git projects — gates still pass, 'complete' still
	// scores — but git-dependent dimensions collapse to neutral and the task has no
	// commit to anchor on. This is the missing signal for code-knowledge-base sessions:
	// without it, an agent starting a task in a bare directory does not know it is in
	// degraded mode and flails blindly. stderr output keeps --json stdout clean.
	//
	// 非 git 项目优雅降级——门禁照常过、`complete` 照常评分——但依赖 git 的维度归中性，
	// 且 task 无 commit 可锚。这是 code-knowledge-base session 缺失的信号：没有它，
	// agent 在裸目录里启动 task 时不知自己在降级模式而盲目挣扎。stderr 输出保持 --json 干净。
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
		src := ""
		if planAcceptanceAdded > 0 {
			src = fmt.Sprintf(`，其中 %d 条从 --plan-file 自动提取`, planAcceptanceAdded)
		}
		fmt.Printf("验收标准（%d 条%s，forge task verify-acceptance 实跑回扣）：\n", len(state.Acceptance), src)
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

// runTaskAbort removes the task but does not score it. Deletes the task-state file
// (DataDir/tasks/<ref>.json) and, when the session-scoped active-task-ref points at
// the aborted task, clears that too. This is the escape hatch for stuck or
// un-passable ghost tasks — e.g. a task started in a non-git project, or one abandoned
// midway. Unlike 'task complete', abort never scores and never creates a review; the
// project's quality record is not polluted by abandoned attempts.
//
// The code/commit changes the task actually made are left alone — abort only reclaims
// forge state. The same ref can be re-started later.
//
// runTaskAbort 移除任务但不评分。删除 task state 文件（DataDir/tasks/<ref>.json），
// 当 session-scoped active-task-ref 指向被 abort 的 task 时也清掉。这是给卡死或
// 永远过不了门禁的 ghost task 的逃生舱——如在非 git 项目启动的 task，或半途放弃的
// task。与 `task complete` 不同，abort 绝不评分、绝不创建 review，项目质量记录不被
// 放弃的尝试污染。
//
// task 实际做的 code/commit 改动不动——abort 只回收 forge state。后续可用同 ref 重新 start。
func runTaskAbort(cmd *cobra.Command, args []string) error {
	explicitRef, _ := cmd.Flags().GetString("ref")
	asJSON, _ := cmd.Flags().GetBool("json")

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	// Resolve the task to abort: explicit --ref wins, else fall back to the session's
	// active task. If neither, there is nothing to identify.
	//
	// 解析要 abort 的 task：显式 --ref 优先，否则取 session 的 active task。
	// 两者皆无则无可识别。
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

	// Load before delete so the report can say whether the task was complete and
	// retain the branch for the user's mental model. Missing file is not fatal: a stale
	// active-task-ref may point to a task that no longer exists; the dangling pointer
	// still needs clearing.
	//
	// 删除前 load，让报告能说明 task 是否完成、并保留 branch 给用户心智模型。
	// 文件缺失不致命：stale active-task-ref 可能指向已不存在的 task，仍需清掉悬空指针。
	var state *taskpipeline.TaskState
	if loaded, err := taskpipeline.LoadTaskState(root, taskRef); err == nil {
		state = loaded
	}

	// Delete the task-state file. ENOENT is acceptable (already deleted / stale ref).
	//
	// 删除 task state 文件。ENOENT 可接受（已删除 / stale ref）。
	if err := taskpipeline.DeleteTaskState(root, taskRef); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete task state: %w", err)
	}

	// If the active-task-ref still points at the aborted task, clear it.
	// Session-scoped — concurrent sessions are not disturbed.
	//
	// 若 active-task-ref 仍指向被 abort 的 task 则清掉。
	// session-scoped，并发 session 不受干扰。
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
				// Output is back-filled only after verify-acceptance actually runs —
				// distinguishes 'never run' (⏳) from 'ran and failed' (❌).
				//
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

	// Validate the gate exists.
	//
	// 校验 gate 存在
	gate := taskpipeline.GateByID(gateID)
	if gate == nil {
		return fmt.Errorf("unknown task gate: %s (valid: %s)", gateID, strings.Join(taskpipeline.GateIDs(), ", "))
	}

	result, err := taskpipeline.ExecuteTaskGate(root, gateID, state)
	if err != nil {
		return err
	}

	// Resolve HEAD from the project root (not forge's cwd at call time) so the recorded
	// gate commit matches the repo the task tracks. Every other git call in this path
	// uses 'git -C root'; this one previously omitted the directory parameter, so forge
	// run from a subdirectory would silently record the wrong commit.
	//
	// 从项目根（不是 forge 调用时的 cwd）解析 HEAD，让记录的 gate commit 与 task 跟踪的
	// repo 一致。本路径其他 git 调用都用 `git -C root`；此前这一处漏了目录参数，forge
	// 从子目录跑时会静默记错 commit。
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = root
	headCommit, _ := headCmd.Output()
	state.RecordGateResult(gateID, result.Passed, strings.TrimSpace(string(headCommit)))

	// Token cost circuit-breaker (advisory): warn when the task's cumulative estimated
	// tokens exceed the threshold. Makes token accounting more than forge-trace
	// observability — it becomes a cost-ceiling signal as task gates advance
	// (loop-engineering cost governance).
	//
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
			fmt.Printf("  ❌ %s — BLOCKED: %s\n", gate.Name, result.Message)
		}
	}

	if !result.Passed {
		return fmt.Errorf("task gate %s failed", gateID)
	}

	return nil
}

// runTaskVerifyAcceptance actually runs every acceptance criterion registered for the
// task (task start --accept), judges each by 'exit code 0 + Expected substring',
// back-fills Passed/Output onto TaskState, and records a checklog:acceptance entry
// (deterministic — forge itself runs the command and observes the result, cannot be
// faked). This is the entry point that turns dev-workflow Plan's 'Run: <cmd>,
// Expected: <out>' into unfakable run-time evidence, hedging against the blind spot of
// an agent claiming 'satisfies acceptance' without actually running it (spec-as-gate).
// Failure does not block the session — it only returns an error so callers/scripts can
// read the exit code; the Passed field is persisted truthfully + checklog, visible in forge trace.
//
// runTaskVerifyAcceptance 实跑任务登记的每条验收标准（task start --accept），按
// 「退出码 0 + Expected 子串」判定，回填 Passed/Output 到 TaskState 并记一条
// checklog:acceptance（deterministic——forge 自己跑命令看结果，不可伪造）。这是把
// dev-workflow Plan 的"Run: <cmd>, Expected: <out>"变成不可伪造实跑证据的入口，
// 对冲 agent 自述「满足验收」却没真跑的盲区（spec-as-gate）。失败不阻塞会话，仅返回 error
// 让调用方/脚本感知退出码；Passed 字段如实落盘 + checklog，forge trace 可见。
func runTaskVerifyAcceptance(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	return runTaskVerifyAcceptanceAt(root)
}

// runTaskVerifyAcceptanceAt is the root-injected core of runTaskVerifyAcceptance,
// split out so it can be unit-tested on a temp project (skipping findProjectRoot /
// cobra). It performs the same actually-run-and-judge acceptance loop and the same
// deterministic checklog:acceptance recording as runTaskVerifyAcceptance — see that
// godoc for the spec-as-gate rationale (forge runs each command itself, so the
// evidence cannot be faked; agents cannot merely assert 'satisfies acceptance').
//
// runTaskVerifyAcceptanceAt 是 runTaskVerifyAcceptance 的 root 注入核心，独立出来便于
// 在临时项目上单测（不经 findProjectRoot / cobra）。实跑任务登记的每条验收标准
// （task start --accept），按「退出码 0 + Expected 子串」判定，回填 Passed/Output 到
// TaskState 并记一条 checklog:acceptance（deterministic——forge 自己跑命令看结果，不可伪造）。
// 这是把 dev-workflow Plan 的"Run: <cmd>, Expected: <out>"变成不可伪造实跑证据的入口，
// 对冲 agent 自述「满足验收」却没真跑的盲区（spec-as-gate）。
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

	if recErr := checklog.Record(root, &checklog.Entry{
		Check:   taskpipeline.CheckNameAcceptance,
		Passed:  allPassed,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  formatAcceptanceDetail(state.Acceptance),
	}); recErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ checklog 记录失败（验收证据未落盘）: %v\n", recErr)
	}

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

// formatAcceptanceDetail builds the Detail summary for checklog:acceptance —
// 'PASS/FAIL — k/n passed', so forge trace can show the overall acceptance result at
// a glance without expanding each criterion.
//
// formatAcceptanceDetail 生成 checklog:acceptance 的 Detail 摘要——「PASS/FAIL — k/n 通过」，
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
		// Fallback: the task finished all gates but already called MarkComplete, so
		// ActiveTaskState returns nil. Load via branch context.
		//
		// 兜底：task 完成全部 gate 但已调 MarkComplete，
		// 故 ActiveTaskState 返 nil。经 branch context load。
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

	// generic kind (research/design/pure-handoff tasks): skip the gate IsComplete check
	// and scoring. The value of these tasks is in the persisted plan/decisions/blockers
	// (continuity truth source), not in code-quality gates. Auto-mark the 3 gates as
	// passed (keeping History complete for list/dashboard display) + MarkComplete + clear
	// active-task-ref; no scoring and no review creation.
	//
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

	// acceptance pre-flight (proof-of-work consumer): when the task declared acceptance
	// criteria, before complete, deterministically verify each is fresh
	// (AcceptedHeadCommit==HEAD) and Passed. Gives AcceptedHeadCommit a consumer — after
	// the MCP teardown this field was write-only and orphaned; this check turns it from
	// a declaration into an affordance gate. Corresponds to Emergence World Proof of
	// Work: a claim of 'acceptance passed' must have a verifiable consumer.
	//
	// acceptance pre-flight（proof-of-work consumer）：task 声明了验收标准时，complete 前
	// deterministic 校验每条都 fresh（AcceptedHeadCommit==HEAD）且 Passed。给 AcceptedHeadCommit
	// 补消费方——MCP 拆除后该字段只写不读成孤儿，本检查把它从声明层变 affordance gate。
	// 对应 Emergence World Proof of Work：声称「验收过」必须有可验证 consumer。
	if ok, reasons := taskpipeline.CheckAcceptanceFresh(root, state); !ok {
		return fmt.Errorf(`acceptance pre-flight 未通过（验收未实跑/快照过期/未通过）: %s；逃生（落 checklog 审计，降 evidence 强度到 Weak）: forge task override --acceptance-gate disable 或 FORGE_ACCEPTANCE_GATE=disable`,
			strings.Join(reasons, `; `))
	}

	// Auto-score the task.
	//
	// 自动评分 task
	if err := scoreTask(root, state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: scoring failed: %v\n", err)
	}

	if state.Score != nil {
		fmt.Printf("Task %s completed! Score: %.0f (%s)\n", state.TaskRef, state.Score.Overall, state.Score.Grade)

		// Duplicate-HEAD detection: warn when another completed task on the same branch
		// shares a HeadCommit (re-scoring over the same commit range) with this one.
		// Same-branch only to avoid false positives — every feature branch pulled from the
		// same master HEAD records the same HeadCommit at task start, but their diffs live
		// on separate branches and do not overlap, so cross-branch matches are not duplicates.
		//
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

		// Missing-hook check: warn when a critical quality hook never ran during this task.
		//
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

	// Act feedback arm (PDCA Act): build the evidence-driven conclusion, persist it, and
	// feed it to session-retrospective. Built even when scoring fails (evidence strength
	// does not depend on the score); on Nudge, prints a one-line retrospective directive
	// (stderr, so --json stdout stays clean).
	//
	// Act 反馈臂（PDCA Act）：构建证据驱动结论落盘，喂给 session-retrospective。
	// 即使评分失败也建（证据强度不依赖分数）；Nudge 时打印一行回顾指令（stderr，
	// stdout --json 保持干净）。
	if d := appendConclusion(root, state); d != "" {
		fmt.Fprintln(os.Stderr, d)
	}

	// Clear the active task ref — task is complete (session-scoped).
	//
	// 清 active task ref——task 完成（session-scoped）
	if err := taskpipeline.ClearActiveTaskRef(root, taskpipeline.CurrentSessionID()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear active task ref: %v\n", err)
	}
	// dogfood 2.3: post-complete grace sentinel so file-sentinel does not mistake the
	// natural follow-up git commit for 'no active task + source write' and quarantine it.
	// The prior flow forced agents to open a chore/*-commit task purely to work around
	// this pit (DevWorkbench: 3 such tasks, ~600 invocations). The grace window is
	// bounded (default 5min, see completeGraceWindow); outside the window, quarantine
	// policy resumes — a 'complete' session still writing source 30+ minutes later is no
	// longer actually complete and should start a new task.
	//
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

// checkMissingHooks returns the names of critical quality hooks that never ran during
// this task (based on checklog entries and gate history).
//
// checkMissingHooks 返本任务期间从未跑过的关键质量 hook 名（基于 checklog 条目与 gate 历史）。
func checkMissingHooks(root string, state *taskpipeline.TaskState) []string {
	var missing []string

	latestChecks, err := checklog.LatestByCheckForSession(root, state.SessionID)
	if err != nil || latestChecks == nil {
		// Cannot read checklog — assume every hook is missing unless gate history shows
		// it ran.
		//
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
		// Check whether compile ran via the task-implement gate.
		//
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

// runTaskScopeAdd appends globs to the current task's PlanScope (deduped). Supports
// mid-task iteration — planning is not locked in once at task start: layered
// positioning and 'reconsidering which files to change' both confirm scope is
// evolutionary. Takes effect immediately after persistence (later hooks detect drift
// advisory based on it).
//
// runTaskScopeAdd 把 glob 追加到当前任务的 PlanScope（去重）。支持中途迭代——规划不是
// task start 一次锁死：分层定位、「重新考虑改哪些文件」
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

// runTaskOverride sets per-task escape hatches (option 5, leak-proof). Takes priority
// over global env — one task's escape does not pollute other tasks in the same shell.
// Using any escape hatch records CheckEscapeHatch and caps evidence Strength at Weak
// (gives escape a cost, hedging the 'hard gate + global escape = fake-hard-gate'
// backlash). Legitimate uses: doc-only repos, generated code, CI; do not use it to
// evade read-before-edit/test-coverage.
//
// runTaskOverride 设置 per-task 逃生舱（方案5 防泄漏）。优先于全局 env——一个任务逃生
// 不污染同 shell 的其他任务。用了任一逃生舱会记 CheckEscapeHatch 并把 evidence Strength
// cap 到 Weak（让逃生有代价，对冲「硬门禁 + 全局逃生 = 假硬门禁」反噬）。legitimate 用途：
// doc-only 仓库、生成代码、CI；勿用于逃避 read-before-edit/test-coverage。
func runTaskOverride(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	explicitRef, _ := cmd.Flags().GetString("ref")
	wa, _ := cmd.Flags().GetString("work-activity")
	tc, _ := cmd.Flags().GetString("test-coverage")
	ag, _ := cmd.Flags().GetString(`acceptance-gate`)
	sd, _ := cmd.Flags().GetString("skill-decisions")

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
	if !changed {
		fmt.Printf("当前 per-task 逃生舱：%s\n", describeOverrides(state.Overrides))
		fmt.Println(`设置：--work-activity disable / --test-coverage disable / --acceptance-gate disable / --skill-decisions disable（用了降评分强度到 Weak）`)
		return nil
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}
	fmt.Printf("已设置 per-task 逃生舱：%s\n", describeOverrides(state.Overrides))
	fmt.Println("注意：用了逃生舱会记 checklog 并把任务 evidence 强度 cap 到 Weak（让逃生有代价）。")
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
	if len(parts) == 0 {
		return "（无）"
	}
	return strings.Join(parts, ", ")
}

// runTaskScopeShow prints the declared PlanScope + live scope-drift (the diff between
// actually-changed source and declared). drift is advisory end-to-end: change-impact
// recall is only ~44%, scope is a prediction not a contract, and deviation is the
// steady-state signal — this just makes it measurable and reviewable instead of
// implicit, never blocking.
//
// runTaskScopeShow 打印声明的 PlanScope + 实时 scope-drift（实改源码 vs 声明的差集）。
// drift 全程 advisory：变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract，
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

	// Show the history of all scored tasks.
	//
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

	// Load a single task.
	//
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
		// Fallback: a completed task is no longer active but can still be scored.
		//
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

	// Score if not yet scored.
	//
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
	return nil
}

// scoreTask evaluates a completed task and persists its score.
// scoreTask thin-wrapper: scoring is sunk into taskpipeline.ScoreTask (single source
// of truth). cli runTaskComplete/runTaskScore and tests reuse it transparently —
// MCP forge_task_complete goes through the same taskpipeline.ScoreTask.
//
// scoreTask 评估已完成的 task 并保存评分。
// scoreTask thin-wrapper：评分下沉到 taskpipeline.ScoreTask（单一真相源）。cli runTaskComplete
// /runTaskScore 与测试透明复用——MCP forge_task_complete 走同一 taskpipeline.ScoreTask。
func scoreTask(root string, state *taskpipeline.TaskState) error {
	return taskpipeline.ScoreTask(root, state)
}

// appendConclusion thin-wrapper: Act conclusion build + persist is sunk into
// taskpipeline.AppendConclusion (single source of truth). cli and MCP
// forge_task_complete share the same Act feedback arm. The stderr warning is retained
// by this wrapper (CLI interaction semantics); the taskpipeline layer returns only
// structured results.
//
// appendConclusion thin-wrapper：Act 结论构建+落盘下沉到 taskpipeline.AppendConclusion
// （单一真相源）。cli 与 MCP forge_task_complete 共用同一 Act 反馈臂。stderr 警告由本 wrapper
// 保留（CLI 交互语义），taskpipeline 层只返结构化结果。
func appendConclusion(root string, state *taskpipeline.TaskState) string {
	_, directive, err := taskpipeline.AppendConclusion(root, state)
	if err != nil {
		fmt.Fprintln(os.Stderr, `Warning:`, err)
	}
	return directive
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

// runTaskTimeline groups tasks by session and renders an ASCII timeline.
//
// runTaskTimeline 按 session 分组 task 并展示 ASCII timeline。
func runTaskTimeline(root string, states []*taskpipeline.TaskState) error {
	sessions, err := taskpipeline.LoadSessions(root)
	if err != nil {
		// Fall back to a simple flat list when sessions cannot be loaded.
		//
		// 加载不出 session 时回退到简单 flat list。
		fmt.Println("Task Timeline (session data unavailable):")
		fmt.Println(strings.Repeat("─", 60))
		for _, s := range states {
			printTaskLine(s)
		}
		return nil
	}

	// Build session -> tasks index.
	//
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

	// Print sessions in chronological order.
	//
	// 按时间顺序打印 session
	for _, sess := range sessions {
		tasks, ok := sessionTasks[sess.SessionID]
		if !ok || len(tasks) == 0 {
			continue
		}

		// Session header.
		//
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

		// Sort tasks within the session by start time.
		//
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

	// Print orphan tasks (no session association).
	//
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

// printTaskLine prints a single task in flat format.
//
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

// printTaskTreeLine prints a single task in tree format.
//
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

// findLatestTaskTime returns the latest time among a group of tasks.
//
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

// sortTasksByTime sorts tasks by start time (oldest first).
//
// sortTasksByTime 按开始时间排序 task（最旧在前）。
func sortTasksByTime(tasks []*taskpipeline.TaskState) {
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if tasks[i].StartedAt.After(tasks[j].StartedAt) {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}
}

// validateBranchRef ensures ref is a valid conventional branch name.
//
// validateBranchRef 确保 ref 是合法 conventional 分支名。
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

// isMainBranch checks whether the branch name is main or master.
//
// isMainBranch 检查分支名是否为 main/master。
func isMainBranch(branch string) bool {
	lower := strings.ToLower(branch)
	return lower == "main" || lower == "master"
}

// createAndSwitchBranch creates a new git branch and switches to it.
//
// createAndSwitchBranch 创建新 git 分支并切过去。
func createAndSwitchBranch(root, name string) error {
	cmd := exec.Command("git", "checkout", "-b", name)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
