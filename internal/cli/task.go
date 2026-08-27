package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hostcap"
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
	taskCmd.AddCommand(taskDocReviewCmd)
	taskCmd.AddCommand(taskImpactCmd)
	taskScopeCmd.AddCommand(taskScopeAddCmd)
	taskScopeCmd.AddCommand(taskScopeShowCmd)

	taskStartCmd.Flags().String("title", "", "任务标题")
	// StringArray (not StringSlice): cobra/pflag's StringSlice splits on commas by default,
	// which would break commands containing commas; StringArray keeps each --accept intact.
	// Acceptance criteria are full 'run :: expected' strings.
	//
	// StringArray（非 StringSlice）：cobra/pflag 的 StringSlice 默认按逗号切分，会把
	// 含逗号的命令拆坏；StringArray 每个 --accept 整条不切。验收标准是完整"run :: expected"串。
	taskStartCmd.Flags().StringArray("accept", nil, `验收标准（可重复 --accept）：格式 "run :: expected"（expected=输出子串）或裸 "run"（只看退出码 0）。forge task verify-acceptance 实跑回扣。run 为 go test 且带 expected 而未加 -v 时自动补 -v（否则无 PASS 行永不匹配）`)
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
	taskStartCmd.Flags().String(`parent`, ``, `父任务 ref（建立子任务→父任务关系，subtask 拆解）`)
	// Delegation flags（多 agent 任务分派）：创建时即把任务交给指定 agent（offered 起步），
	// 编排器 fan-out；--depends-on 串依赖图（fan-in 顺序）。worker 侧用 forge task assign/
	// claim/deliver 推进 offered→claimed→delivered 生命周期。
	taskStartCmd.Flags().String(`assignee`, ``, `分派给指定 agent（如 kimi/reasonix/cursor），任务创建即 offered；建议配合 --role 说明角色`)
	taskStartCmd.Flags().String(`role`, ``, `分派角色（如 frontend/backend/testing），随 --assignee 记入 Assignment.Role`)
	taskStartCmd.Flags().StringArray(`depends-on`, nil, `依赖的上游 task ref（可重复 --depends-on）：本任务等待它们 delivered 后再开工；支持 key:ref 跨仓依赖（key 须为本 repo 所属 workspace 的成员）`)
	// Per-task zombie TTL override (design §3/§9 --ttl): a delegation that should expire on its own
	// clock — faster than the global 7d default (short-fuse), or slower (long runner) — sets this.
	// Zero (no flag) keeps the global constant, fully backward compatible. health.effectiveTTL reads it.
	//
	// Per-task 僵尸 TTL 覆盖（设计 §3/§9 --ttl）：需按自己的时钟失联的分派——比全局 7d 默认更快
	//（短时效）或更慢（长跑任务）——设此项。零（不带 flag）保持全局常量，完全向后兼容。
	// health.effectiveTTL 读取它。
	taskStartCmd.Flags().Duration(`ttl`, 0, `每任务僵尸 TTL，覆盖全局 7d 默认（如 24h/30m/72h）；0=用全局（向后兼容）。offered/claimed/input-required 超此时长无活动即标僵尸`)
	taskStartCmd.Flags().String(`ref`, ``, `任务引用（如 feat/add-auto-branch），默认从分支名推断`)
	taskStartCmd.Flags().String("from-issue", "", "外部 issue URL（linear/github），解析为 task.ExternalOrigin 锚定外部 issue（衔接 spawn 式编排器）")
	taskStartCmd.Flags().Bool("branch", false, "从 main/master 创建新分支并切换（ref 作为分支名）")
	taskStartCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskStatusCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskStatusCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskGateCmd.Flags().Bool("silent", false, "静默模式（仅返回退出码）")
	taskVerifyAcceptanceCmd.Flags().Bool(`trust-foreign`, false, `受信外来验收命令（task import/.forge migrate 带入）：确认已人工审阅命令清单后执行，首次受信运行清除外来标记`)
	taskVerifyAcceptanceCmd.Flags().String("ref", "", "指定任务引用（不依赖活跃任务检测）")
	taskGateCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskCompleteCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskAbortCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskAbortCmd.Flags().Bool("json", false, "JSON 格式输出")
	taskAbortCmd.Flags().Bool(`cascade`, false, `一并 abort 所有依赖此任务的 task（传递闭包，清除死链）`)
	taskAbortCmd.Flags().Bool(`detach-deps`, false, `从依赖此任务的 task 的 DependsOn 移除该边（解除依赖，保留依赖方任务）`)
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
	taskOverrideCmd.Flags().String("doc-gate", "", `设为 disable 跳过 task-complete 的 doc pre-flight（输出→回检门禁；轮次上限后的放行须人工确认后走这里）`)
	taskScopeAddCmd.Flags().String("ref", "", "指定任务引用（不依赖活跃任务检测）")
	taskImpactCmd.Flags().String(`level`, ``, `跨仓影响级别：none（纯本仓）| multi（波及其他 repo）——必填`)
	taskImpactCmd.Flags().StringArray(`repo`, nil, `受影响的项目 key（可重复 --repo；仅 level=multi 携带，none 下忽略）`)
	taskImpactCmd.Flags().String(`note`, ``, `影响说明（自由文本，供 review 阅读）`)
	taskImpactCmd.Flags().String(`ref`, ``, `任务 ref（默认当前活跃任务）`)
	taskDocReviewCmd.Flags().String("ref", "", "任务 ref（默认当前活跃任务）")
	taskDocReviewCmd.Flags().String("passed", "", "评审结论：pass | fail（必填）")
	taskDocReviewCmd.Flags().Int("score", 0, "rubric 四维总分 0-100（rubric-docs.md）")
	taskDocReviewCmd.Flags().Int("round", 0, "本轮次编号（从 1 递增；≥3 轮未过升级人工确认）")
	taskDocReviewCmd.Flags().String("reviewer", "", "评审者标识（子代理/session id——产出者不能当回检者）")
	taskDocReviewCmd.Flags().StringSlice("critical", nil, "Critical 发现内容（可重复；未决将阻断 doc gate，须 forge task finding resolve 解决）")
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
	Use:   "verify-acceptance [--ref <ref>] [--trust-foreign]",
	Short: "实跑验收标准并记 deterministic 证据（spec-as-gate）",
	Long: `forge task verify-acceptance 实跑 task start --accept 登记的每条验收标准（Run 命令），
按"退出码 0 + Expected 子串"判定，回填 Passed/Output，并记 checklog:acceptance（deterministic）。
把 dev-workflow Plan 的 "Run: <cmd>, Expected: <out>" 验收标准从 plan 文本变成不可伪造的实跑证据，
对冲 agent 自述"满足验收"但没真跑的盲区。

验收命令若来自 task import / .forge migrate（外来标记），首次执行前须人工审阅命令清单并加
--trust-foreign 显式受信——外来命令串是可任意执行的载荷，不审阅就跑等于把执行权交给 bundle 作者。`,
	RunE: runTaskVerifyAcceptance,
}

var taskCompleteCmd = &cobra.Command{
	Use:   "complete",
	Short: "标记任务完成（自动评分）",
	RunE:  runTaskComplete,
}

var taskAbortCmd = &cobra.Command{
	Use:   "abort [--ref <ref>] [--cascade|--detach-deps]",
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
	Use:   "add <glob> [<glob>...] [--ref <ref>]",
	Short: "追加计划改动文件到白名单（支持中途迭代；--ref 指定任务）",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTaskScopeAdd,
}
var taskScopeShowCmd = &cobra.Command{
	Use:   "show",
	Short: "查看声明的白名单 + 实时 scope-drift",
	RunE:  runTaskScopeShow,
}
var taskOverrideCmd = &cobra.Command{
	Use:   `override [--work-activity disable] [--test-coverage disable] [--acceptance-gate disable] [--skill-decisions disable] [--doc-gate disable]`,
	Short: "设置 per-task 逃生舱（优先全局 env，不污染他任务；验证类降 evidence 强度到 Weak（重证据按证据缩放豁免），work-activity 是节奏门禁不降强度）",
	RunE:  runTaskOverride,
}

var taskDocReviewCmd = &cobra.Command{
	Use:   "doc-review --passed <pass|fail> --score <N> [--round <R>] [--reviewer <id>] [--critical <发现>]",
	Short: "记录 L2 文档回检证据（rubric-docs.md 评审后落档；doc gate 消费）",
	RunE:  runTaskDocReview,
}

// phaseExplosionWarning returns a non-empty warning when the given session already
// has too many incomplete tasks — the 'phase explosion' anti-pattern (one plan split
// into N tasks each running the full gate pipeline). Advisory only (non-blocking).
// Returns ” when no warning is needed (fewer than 3, unknown session, or on error).
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
// the directory-snooping weak signal of SessionRecord.AgentType). Probe order:
// explicit (--origin-tool) > FORGE_AGENT (injected by runHook from the resolved
// --agent flag, so hook-spawned forge processes on kimi/windsurf know their host) >
// CLAUDE_CODE_SESSION_ID (claude-code). Lets the task record 'who started it'
// across tool handoffs; other hosts append their own session+tool via forge task attach.
//
// detectOriginTool 返回任务的发起工具（声明式真相，区别于 SessionRecord.AgentType 的目录探测弱信号）。
// 探测顺序：explicit（--origin-tool）> FORGE_AGENT（runHook 把解析出的 --agent 值注入，
// 使 kimi/windsurf 上 hook 派生的 forge 进程知道自己的 host）> CLAUDE_CODE_SESSION_ID
// （claude-code）。跨工具接续时让 task 记录「谁起的头」，其他 host 接续时用
// forge task attach 追加自己的 session+工具。
func detectOriginTool(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if agent := os.Getenv("FORGE_AGENT"); agent != "" {
		return agent
	}
	// Host-injected shell env (today only claude-code's CLAUDE_CODE_SESSION_ID) —
	// registry-driven, see hostcap.Host.ShellSessionEnv.
	//
	// 宿主注入的 shell env（目前仅 claude-code 的 CLAUDE_CODE_SESSION_ID）——
	// 注册表驱动，见 hostcap.Host.ShellSessionEnv。
	if host, _ := hostcap.ProbeShellIdentity(); host != "" {
		return host
	}
	return ""
}

// resolveOriginTool is detectOriginTool plus the LAST attribution fallback: the
// last-session pointer written by the hook dispatcher. A forge command run
// inside a kimi/codex/cursor/... Bash tool carries no identity env (their
// shells are bare), so without this every such task/session anchor was
// unattributed — 9 of 20 tasks in this repo had an empty OriginTool (2026-08
// audit). The pointer is freshness-gated (taskpipeline.RecentHookSession), so
// stale agent activity never mislabels a human's manual terminal run.
//
// resolveOriginTool 是 detectOriginTool 加最终归因回落：hook 分发器写入的
// last-session 指针。在 kimi/codex/cursor/... 的 Bash 工具里跑的 forge 命令
// 不带任何身份 env（它们的 shell 是裸的），故没有本函数这些任务/会话锚定全部
// 无归属——本仓 20 个任务里 9 个 OriginTool 为空（2026-08 审计）。指针有新鲜
// 度门控（taskpipeline.RecentHookSession），过期活动不会错标人类的手动终端
// 操作。
func resolveOriginTool(root, explicit string) string {
	if tool := detectOriginTool(explicit); tool != "" {
		return tool
	}
	if _, agent, ok := taskpipeline.RecentHookSession(root); ok {
		return agent
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
	if states, err := taskpipeline.ListTaskStates(root); err == nil {
		if _, pending := taskpipeline.OrchestrationReady(states, state.TaskRef); len(pending) > 0 {
			fmt.Fprintf(os.Stderr, `⚠ 编排任务 %s 尚有 %d 个子任务未交付/终态: %s；仍可 complete（设计：不强制），建议先综合子任务成果`, state.TaskRef, len(pending), strings.Join(pending, `, `))
			fmt.Fprintln(os.Stderr)
		}
	}
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

	// Node lease (sync-convergence.md §4): starting work claims the task for this
	// machine — advisory-only in v1, fail-open (identity problems never block start).
	//
	// 节点租约（sync-convergence.md §4）：开工即为本机认领任务——v1 仅 advisory，
	// fail-open（身份问题绝不阻塞开工）。
	taskpipeline.ClaimLeaseForCurrentNode(state)

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
	// Per-task TTL override (design §3/§9 --ttl): persists into state.TTL so health.effectiveTTL
	// surfaces this task as stale on its own clock, independent of the global 7d constant. Zero
	// (no --ttl) leaves state.TTL at its zero-value fallback — no behavior change for legacy tasks.
	//
	// Per-task TTL 覆盖（设计 §3/§9 --ttl）：持久化进 state.TTL，使 health.effectiveTTL 按本任务自己
	// 的时钟标失联，独立于全局 7d 常量。零（无 --ttl）留 state.TTL 于零值回落——legacy 任务无行为变化。
	if ttl, _ := cmd.Flags().GetDuration("ttl"); ttl > 0 {
		state.TTL = ttl
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
	// go test ergonomics (usage-log fix): a `go test` Run without -v prints no PASS lines,
	// so an Expected like "PASS" can never match and the agent only finds out at
	// verify-acceptance time — the recorded failure mode was abort + restart the task.
	// Auto-insert -v (output-only; exit-code semantics unchanged) and announce it — never
	// silently rewrite a registered command. Runs with empty Expected (exit-code-only)
	// are left untouched: they don't need verbose output.
	//
	// go test 人体工学（usage 日志修复）：`go test` 不带 -v 时输出没有 PASS 行，Expected
	// 写 "PASS" 永不匹配，agent 到 verify-acceptance 才发现——真实失败模式是 abort 重开
	// 任务。自动补 -v（只影响输出、退出码语义不变）并明示——绝不静默改写登记的命令。
	// Expected 为空（只看退出码）的命令不动：它们不需要 verbose 输出。
	if adjusted := taskpipeline.EnsureGoTestVerbose(state.Acceptance); len(adjusted) > 0 {
		fmt.Printf("ℹ️ 验收命令自动补 -v（go test 无 -v 时输出无 PASS 行，Expected 子串永不匹配）：%s\n", strings.Join(adjusted, ", "))
	}
	if parent, _ := cmd.Flags().GetString("parent"); parent != "" {
		state.ParentTaskRef = parent
	}
	originTool, _ := cmd.Flags().GetString("origin-tool")
	state.OriginTool = resolveOriginTool(root, originTool)

	// External issue origin: extends the task's source from branch to external issue
	// (linear/github), bridging spawn-style orchestrators (Symphony-like) — when the
	// orchestrator spawns a run, the task is naturally linked to the issue, not via branch inference.
	//
	// 外部 issue origin：把 task 的来源从 branch 扩展到外部 issue（linear/github），
	// 衔接 spawn 式编排器（Symphony 类）——编排器拉起 run 时 task 天然关联 issue，不靠 branch 推断。
	if fromIssue, _ := cmd.Flags().GetString(`from-issue`); fromIssue != `` {
		state.ExternalOrigin = taskpipeline.ParseExternalOriginURL(fromIssue)
	}
	// Delegation: optionally hand this task to a specific agent at creation time. --assignee
	// drives AssignTo (no-assignment → offered); the orchestrator creates the task already
	// offered and the worker claims it. --depends-on persists the upstream dependency graph
	// (fan-in ordering) with cycle detection (DAG enforced via AddDependency); the task-verify /
	// task-complete gates block until every upstream is delivered (phase 3). Runs after OriginTool
	// is set so OfferedBy records who actually offered the task.
	//
	// 分派：创建时可选把本任务交给指定 agent。--assignee 驱动 AssignTo（无→offered）；
	// 编排器创建即 offered，工作方认领。--depends-on 持久化上游依赖图（fan-in 顺序）并做环检测
	// （经 AddDependency 强制 DAG）；task-verify/task-complete 门禁在上游全部交付前阻断（阶段3）。
	// 须在 OriginTool 设置之后运行，使 OfferedBy 记录真正的发起方。
	if assignee, _ := cmd.Flags().GetString(`assignee`); assignee != `` {
		warnIfUnknownAgent(cmd.ErrOrStderr(), assignee)
		role, _ := cmd.Flags().GetString(`role`)
		if err := state.AssignTo(assignee, role, state.OriginTool); err != nil {
			return fmt.Errorf(`分派失败: %w`, err)
		}
	}
	if deps, _ := cmd.Flags().GetStringArray(`depends-on`); len(deps) > 0 {
		// AddDependency rejects a self-reference and any ref whose transitive deps lead back to
		// this task (a cycle would deadlock the ring). lookup loads each ref's state for the DFS;
		// a missing ref is tolerated here (the edge is recorded; the gate later treats missing as
		// not-delivered), so a forward reference to a task created moments later is allowed.
		//
		// Cross-repo (key:ref, depref.go): membership/existence is validated first
		// (fail-open — manifest trouble only warns). The lookup deliberately returns nil for
		// key:ref so the cycle DFS never crosses into another repo's graph (a real-time
		// cross-repo DFS would need a global graph lock across DataDirs; cross-repo cycles are
		// doctor-detected instead). Same-repo refs keep the exact pre-workspace DFS behavior.
		//
		// AddDependency 拒绝自引用及任何传递依赖指回本 task 的 ref（环会死锁环上 task）。lookup 为
		// DFS 载入各 ref 的 state；缺失 ref 此处容忍（边已记；门禁后把缺失当未交付），故对稍后创建
		// 的 task 的前向引用是允许的。
		//
		// 跨仓（key:ref，见 depref.go）：先做成员资格/存在性校验（fail-open——清单故障只警告）。
		// lookup 刻意对 key:ref 返回 nil，使环 DFS 绝不跨入他仓图（实时跨仓 DFS 需要跨 DataDir
		// 的全局图锁；跨仓环改由 doctor 检出）。本仓 ref 保持 workspace 之前的 DFS 行为不变。
		if err := validateDependsOnRefs(root, state.TaskRef, deps, cmd.ErrOrStderr()); err != nil {
			return err
		}
		lookup := func(ref string) *taskpipeline.TaskState {
			if key, _ := taskpipeline.SplitDepRef(ref); key != `` {
				return nil // 跨仓 ref 不做实时 DFS（见上注释）
			}
			st, err := taskpipeline.LoadTaskState(root, ref)
			if err != nil || st == nil {
				return nil
			}
			return st
		}
		if err := state.AddDependency(deps, lookup); err != nil {
			return fmt.Errorf(`依赖设置失败: %w`, err)
		}
	}

	// Take the session id once — used to scope active-task-ref and session
	// records so concurrent sessions on a shared checkout stay isolated. Env
	// probes cover claude-code (shell env) and hook-spawned wrappers
	// (FORGE_SESSION_ID); a forge invocation inside any OTHER host's Bash tool
	// has neither, so fall back to the freshness-gated last-session pointer —
	// this binds the task to the real host session's scoped record and makes
	// the active-task-ref write/read keys match the hook side (which reads
	// scoped by the stdin session id), instead of collapsing onto the legacy
	// global file where concurrent sessions clobber each other.
	//
	// 取一次 session id——用于 scope active-task-ref 与 session record，让共享
	// checkout 上的并发 session 保持隔离。env 探测覆盖 claude-code（shell
	// env）与 hook 派生的 wrapper（FORGE_SESSION_ID）；在任何其他宿主的 Bash
	// 工具里发起的 forge 调用两者皆无，故回落到有新鲜度门控的 last-session
	// 指针——这把任务绑到真实宿主会话的 scoped 记录上，并使 active-task-ref
	// 的写读键与 hook 侧（按 stdin session id 读 scoped）一致，而非挤到并发
	// session 互相覆盖的 legacy 全局文件。
	sid := taskpipeline.CurrentSessionID()
	if sid == "" {
		if pointerSID, _, ok := taskpipeline.RecentHookSession(root); ok {
			sid = pointerSID
		}
	}

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

	// Append a task-started boundary event instead of Clearing the logs
	// (multi-task-concurrency design §5, L2 event-sourcing). The old Clear was a
	// destructive truncation with three production failure faces: a task B start
	// wiped in-flight task A's evidence chain (concurrent tasks share the project
	// DataDir by design), a crash between tool call and state write left the audit
	// chain severed, and cross-machine merges lost whatever had been cleared.
	// Reads were already TaskRef-scoped (checklog.LoadForTask /
	// LatestByCheckForSession, toolusage.LoadForTask), so segmentation by boundary
	// event preserves everything Clear protected against — inheritance of a
	// previous task's evidence — without destroying anything. Retention pruning
	// (the useful half of Clear) stays, non-destructively.
	//
	// 追加 task-started 边界事件，取代清空日志（multi-task-concurrency 设计 §5，L2 事件
	// 化）。旧 Clear 是破坏性截断，三个生产事故面：任务 B 开工抹掉在途任务 A 的证据链
	//（并发任务按设计共享项目 DataDir）；工具调用与状态写之间的崩溃断掉审计链；被清
	// 掉的内容跨机合并即丢失。读取侧本就按 TaskRef 过滤（checklog.LoadForTask /
	// LatestByCheckForSession、toolusage.LoadForTask），按边界事件分段既保住 Clear 防的
	// 那件事——新任务继承上一任务的证据——又不破坏任何东西。retention 清理（Clear 有
	// 用的那半）保留，非破坏性。
	recordAuditErr := func(err error, what string) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s 失败: %v\n", what, err)
		}
	}
	recordAuditErr(checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckTaskStarted,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelAdvisory,
		TaskRef: ctx.TaskRef,
		Detail:  fmt.Sprintf("task started: %s (branch %s)", state.Summary, state.Branch),
	}), "记录 task-started 边界事件")
	checklog.Prune(root)
	toolusage.Prune(root)

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

	cascade, _ := cmd.Flags().GetBool(`cascade`)
	detachDeps, _ := cmd.Flags().GetBool(`detach-deps`)
	// The two flags are the two non-default branches of the design §4 "three-way" abort of a task
	// others depend on. Mutually exclusive: --cascade deletes the dependents, --detach-deps keeps
	// them but removes the now-dangling edge. Default = neither: abort only + warn, the milestone
	// "废弃链被提示" behavior. The agent-neutral CLI surface replaces the design's interactive
	// three-way prompt (an agent driving forge cannot answer an interactive stdin prompt reliably).
	//
	// 两个 flag 是设计§4 abort 被依赖任务「三选一」的两个非默认分支，互斥：--cascade 删依赖方，
	// --detach-deps 留依赖方但摘掉悬空边。默认两者皆不传：仅 abort + warn，里程碑「废弃链被提示」行为。
	// agent-neutral CLI 表层替代了设计的交互式三选一 prompt（驱动 forge 的 agent 无法可靠回答交互 stdin）。
	if cascade && detachDeps {
		return fmt.Errorf(`--cascade 与 --detach-deps 互斥：--cascade 一并 abort 依赖方，--detach-deps 仅摘依赖边保留依赖方`)
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

	// Reverse-dependency scan (design phase 3 + §4 three-way): aborting a task that others
	// DependsOn leaves those dependents blocked forever (their gate reports the ref missing/not-
	// delivered and never unblocks). By default we do NOT cascade-abort — a dependent may still be
	// valuable with its upstream re-pointed — but surface the dangling edge. --cascade aborts the
	// whole transitive closure; --detach-deps removes just the edge. Computed before delete so the
	// just-aborted state is still scannable.
	//
	// KNOWN LIMITATION (multi-repo workspace Option B): the scan reads only THIS repo's tasks —
	// a dependent living in ANOTHER member repo (its DependsOn pointing here via key:ref) is
	// invisible to abort: not warned, not cascaded, not detached; its gate reports the aborted
	// key:ref pending forever until someone removes the edge in that repo. Cross-repo cleanup is
	// deliberately out of scope (the reverse index would need a cross-DataDir scan + remote
	// mutation); a one-line note below surfaces the blind spot when this repo is a multi-repo
	// workspace member.
	//
	// 反向依赖扫描（设计阶段3 + §4 三选一）：abort 一个被其他 task DependsOn 的 task，会让依赖方永远阻塞
	// （门禁报该 ref 缺失/未交付且永不放行）。默认不级联 abort——依赖方在上游重指后可能仍有价值——但暴露
	// 悬空边。--cascade abort 整个传递闭包；--detach-deps 仅摘边。delete 前算，使刚 abort 的 state 仍可扫。
	//
	// 已知限制（多仓 workspace Option B）：扫描只读本仓 task——位于另一个成员仓的依赖方（其
	// DependsOn 经 key:ref 指向本仓）对 abort 不可见：不警告、不级联、不摘边；其门禁会把被
	// abort 的 key:ref 永久计 pending，直到有人到那个仓摘掉该边。跨仓清理刻意不做（反向索引
	// 需要跨 DataDir 扫描 + 远端改写）；下方在本 repo 属于多仓 workspace 时打一行提示暴露该
	// 盲区。
	dependentsMap := map[string][]string{}
	allStates, listErr := taskpipeline.ListTaskStates(root)
	if listErr == nil {
		for _, t := range allStates {
			if t == nil {
				continue
			}
			for _, d := range t.DependsOn {
				dependentsMap[d] = append(dependentsMap[d], t.TaskRef)
			}
		}
	}
	// H1: --cascade/--detach-deps 是明确的「处理依赖」意图（非默认 warn 流程）。ListTaskStates 失败时
	// 若静默跳过，空的 dependentsMap 会让级联/解绑降级为 no-op，而主 abort 仍成功——用户误以为依赖已处理，
	// 实际停滞的依赖任务原样留存（JSON 还省略 cascaded 字段）。故扫描失败时对这两个 flag 明确报错，让用户
	// 知道未执行并重试；默认 warn 流程不受影响（仍尽力 abort 主任务）。
	if listErr != nil && (cascade || detachDeps) {
		return fmt.Errorf(`扫描反向依赖失败，未执行 --cascade/--detach-deps：%v（请重试）`, listErr)
	}
	var dependents []string
	for _, dep := range dependentsMap[taskRef] {
		dependents = append(dependents, dep)
	}
	// cascade closure: transitive dependents (direct + indirect), breadth-first over the reverse
	// map. A dependent whose upstream is gone can never pass its gate, so cascade clears the chain.
	// cascaded = the attempted set (BFS closure, drives the delete loop); cascadedDone = those actually
	// deleted. A delete can fail on permission / Windows file lock — that dependent emits an INLINE
	// per-item stderr Warning inside the loop (NOT by re-reading `cascaded` later) and is excluded from
	// cascadedDone, so it must NOT be reported as aborted in JSON, or a JSON consumer believes a live
	// task is gone.
	//
	// cascade 闭包：传递依赖方（直接 + 间接），对反向 map 广度优先。依赖方上游已没，门禁永远过不了，
	// 故 cascade 清除死链。cascaded = 试图集（BFS 闭包，驱动删除循环）；cascadedDone = 实际删除成功的。
	// 删除可能因权限/Windows 文件锁失败——该依赖方在循环内发一条内联 per-item stderr Warning（并非后续回读
	// `cascaded`），且不计入 cascadedDone，故绝不能报进 JSON 的已 abort，否则 JSON 消费者会以为一个仍在
	// 盘上的任务已没了。
	var cascaded, cascadedDone []string
	if cascade {
		visited := map[string]bool{}
		queue := append([]string{}, dependents...)
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if visited[cur] {
				continue
			}
			visited[cur] = true
			cascaded = append(cascaded, cur)
			queue = append(queue, dependentsMap[cur]...)
		}
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

	// --cascade: abort every transitive dependent and clear its active-task-ref. Done after the
	// primary delete; each DeleteTaskState tolerates ENOENT.
	//
	// --cascade：abort 每个传递依赖方并清其 active-task-ref。在主 delete 之后；各 DeleteTaskState 容忍 ENOENT。
	//
	// M3 已知限制（follow-up）：DeleteTaskState 是 os.Remove 无任务锁，与并发的 MutateTaskState 存在 TOCTOU——
	// 若另一 forge 进程正持有某级联目标的任务锁、已 load 准备 save，我们的 remove 后其 save 会重建文件，
	// 级联「成功」但目标复现。主 abort 同样如此；--cascade 把窗口放大 N 倍。彻底修复需 DeleteTaskState 走
	// LockTask+remove+unlock，本任务不扩范围。
	for _, depRef := range cascaded {
		if err := taskpipeline.DeleteTaskState(root, depRef); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, `Warning: failed to cascade-abort %s: %v`+"\n", depRef, err)
			continue // 删除失败：留 stderr Warning，不计入 cascadedDone（JSON 只报实际已删）
		}
		cascadedDone = append(cascadedDone, depRef)
		if ref := taskpipeline.ReadActiveTaskRef(root, sid); ref == depRef {
			// 清依赖方的 active-task-ref 失败时记 stderr 而非吞掉：该 ref 仍指向一个已删任务，会让
			// 下一次 `forge task status` / resume 锚定到幽灵。删除本身已成功，故不计入 cascade 失败，
			// 只作为非致命告警暴露出来。
			if err := taskpipeline.ClearActiveTaskRef(root, sid); err != nil {
				fmt.Fprintf(os.Stderr, `Warning: cascade-aborted %s but failed to clear its active task ref: %v`+"\n", depRef, err)
			}
		}
	}

	// --detach-deps: remove the now-dangling edge from each direct dependent, leaving the dependent
	// task alive (it may still be valuable, just no longer waiting on this aborted upstream).
	//
	// --detach-deps：从每个直接依赖方摘掉这条悬空边，保留依赖方任务（它可能仍有价值，只是不再等这个已
	// abort 的上游）。
	var detached []string
	if detachDeps {
		for _, depRef := range dependents {
			err := taskpipeline.MutateTaskState(root, depRef, func(s *taskpipeline.TaskState) error {
				var kept []string
				for _, d := range s.DependsOn {
					if d != taskRef {
						kept = append(kept, d)
					}
				}
				s.DependsOn = kept
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, `Warning: failed to detach dep edge on %s: %v`+"\n", depRef, err)
				continue
			}
			detached = append(detached, depRef)
		}
	}

	// M1: dependents/cascaded/detached 的原始顺序随 ListTaskStates 的 ReadDir（跨平台 FS 顺序不定），
	// 排序后输出稳定——JSON 消费者不因遍历顺序间歇失败，stderr warn 也确定。级联最终状态与顺序无关。
	slices.Sort(dependents)
	// cascaded（试图集）此处不排序：它仅作删除循环的迭代源（task.go:823，循环在排序之前），删除失败者
	// 已在循环内发内联 per-item stderr Warning，排序后既不入 JSON（JSON 只取 cascadedDone）也不入任何汇总行。
	slices.Sort(cascadedDone)
	slices.Sort(detached)

	if asJSON {
		out := map[string]any{
			"task_ref": taskRef,
			"aborted":  true,
		}
		if state != nil {
			out["was_complete"] = state.IsComplete()
			out["branch"] = state.Branch
		}
		if len(dependents) > 0 {
			out[`dependents_blocked`] = dependents
		}
		// JSON 的 `cascaded` 只报实际删除成功的（cascadedDone）——失败的已走 stderr Warning，
		// 报进 JSON 会让编排 agent 误以为一个仍在盘上的任务已 abort。
		//
		// JSON `cascaded` reports only successful deletions (cascadedDone) — failures already went
		// to stderr Warning; reporting them in JSON would make an orchestrator agent believe a task
		// still on disk was aborted.
		if len(cascadedDone) > 0 {
			out[`cascaded`] = cascadedDone
		}
		if len(detached) > 0 {
			out[`detached`] = detached
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
	// Summary line reports ONLY successful deletes (cascadedDone), mirroring the JSON path — a failed
	// delete already got its own "Warning: failed to cascade-abort X" above; listing it again here as
	// "aborted" would make a task still on disk look finished (the very leak cascaded/cascadedDone split fixes).
	//
	// 汇总行只报成功删除（cascadedDone），与 JSON 路径一致——失败的删除上方已有独立的"Warning: failed to
	// cascade-abort X"；此处再当"已 abort"列出会让一个仍在盘上的任务看起来已完成（正是 cascaded/cascadedDone
	// 拆分要堵的泄露）。
	if len(cascadedDone) > 0 {
		fmt.Fprintf(os.Stderr, `Cascade-aborted %d dependent(s): %s`+"\n", len(cascadedDone), strings.Join(cascadedDone, `, `))
	}
	if len(detached) > 0 {
		fmt.Fprintf(os.Stderr, `Detached dep edge on %d dependent(s): %s`+"\n", len(detached), strings.Join(detached, `, `))
	}
	if len(dependents) > 0 && !cascade && !detachDeps {
		fmt.Fprintf(os.Stderr, `Warning: %d task(s) depend on this one (%s); their gate will now block on a missing upstream. Re-run with --cascade to abort them too, or --detach-deps to unlink them.`+"\n", len(dependents), strings.Join(dependents, `, `))
	}
	// Cross-repo blind spot (see the scan's KNOWN LIMITATION comment above): only surfaced as a
	// note when this repo is a multi-repo workspace member — the scan itself stays same-repo.
	//
	// 跨仓盲区（见上方扫描处的 KNOWN LIMITATION 注释）：仅当本 repo 属于多仓 workspace 时
	// 提示一句——扫描本身仍只覆盖本仓。
	if multiRepoMembership(root) {
		fmt.Fprintf(os.Stderr, "Note: 跨仓依赖方（他仓 task 以 key:ref 依赖 %s）不在本次扫描内；若存在，其门禁会把本 ref 永久计 pending——需到对应 repo 摘掉依赖边（forge workspace doctor 可检跨仓环）\n", taskRef)
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
	// Multi-repo workspace context (Step 4): one fail-open line — the workspace
	// manifest is a global store, so any trouble silently omits the line.
	//
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

	// Assignment advisory (P2 of the 2026-08-18 脱节修复): gating a task that is offered to
	// ANOTHER agent and never claimed is the pipeline/assignment drift precursor — warn but
	// never block (orchestrator proxying is legitimate).
	//
	// 分派 advisory（2026-08-18 脱节修复的 P2）：给「分派给另一个 agent 且从未认领」的任务
	// 过门禁正是管线/分派脱节的前兆——提醒但绝不阻断（编排器代跑合法）。
	adviseUnclaimedAssignment(root, gateID, result.Passed, state)

	// Node-lease advisory (sync-convergence.md §4): gating a task another machine
	// holds an active lease on is the dual-machine collision precursor — warn, never
	// block (TTL leases are UX, not correctness).
	//
	// 节点租约 advisory（sync-convergence.md §4）：给他机持有活跃租约的任务过门禁
	// 是双机互踩的前兆——提醒但绝不阻断（TTL 租约管 UX 不管正确性）。
	if ls := taskpipeline.LeaseStatusForCurrentNode(state); ls.ForeignActive {
		fmt.Fprintf(os.Stderr, "⚠️ [lease] %s\n", ls.Message)
	}

	// 刻意不在此处 MarkComplete（dogfood 2026-08-18 死锁修复）：曾经「最后一道 gate 通过
	// 即 MarkComplete」，而 ActiveTaskState 对 CompletedAt!=nil 返回 nil —— 紧随其后的
	// `forge task complete` acceptance pre-flight 恰要求快照新鲜（时为 AcceptedHeadCommit==HEAD；
	// 2026-08-25 起为任务 HeadCommit 锚定的源码内容指纹，见 acceptance.go），
	// 刷新只能由 verify-acceptance（默认认 active task，可用 --ref 显式指定）完成。门一过
	// 任务即失活 → 验收刷新死锁（本次 v2 任务实测踩中：review 修复 commit 移动 HEAD 后
	// complete 永久 BLOCKED，且无任何 CLI 路径可复活）。完成 = `forge task complete` 的
	// 整个动作（pre-flight → MarkComplete → 评分 → 反馈 → 清 ref）；门禁全过只是它的
	// 前置条件，不是完成本身。
	//
	// Deliberately NO MarkComplete here (dogfood 2026-08-18 deadlock fix): the last gate
	// used to mark the task complete on pass, but ActiveTaskState returns nil once
	// CompletedAt is set — and the immediately following `forge task complete` acceptance
	// pre-flight demands snapshot freshness (then AcceptedHeadCommit==HEAD; since
	// 2026-08-25 a source-content fingerprint anchored at the task's HeadCommit, see
	// acceptance.go), refreshable ONLY by
	// verify-acceptance (active task by default; --ref pins explicitly). Gate pass
	// deactivated the task → acceptance refresh deadlocked (hit in production by the v2
	// task: a review-fix commit moved HEAD, complete BLOCKED forever, no CLI path could
	// revive it). Completion is `forge task complete`'s whole action (pre-flight →
	// MarkComplete → scoring → feedback → clear ref); all gates passed is its
	// prerequisite, not completion itself.
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
	trustForeign, _ := cmd.Flags().GetBool(`trust-foreign`)
	explicitRef, _ := cmd.Flags().GetString("ref")
	return runTaskVerifyAcceptanceAt(root, explicitRef, trustForeign)
}

// stdinIsHumanTerminal reports whether stdin is attached to a human terminal (char device) —
// the discriminator the --trust-foreign gate uses to keep an LLM agent (whose Bash-spawned
// stdin is a pipe) from self-trusting foreign acceptance commands by simply following the
// refusal text's own instruction. Var (not func) so tests can inject both sides.
//
// KNOWN LIMITATION (review 2026-08-16): mintty (Git Bash's default terminal) hands native
// processes a named pipe instead of a char device, so a real human is refused there too. No
// in-place escape hatch is offered on purpose — any env/flag bypass is equally settable by an
// injected agent, re-opening the self-trust hole this gate exists to close. The refusal message
// detects TERM_PROGRAM=mintty and tells the human to re-run from a ConPTY terminal (Windows
// Terminal / PowerShell) instead.
//
// stdinIsHumanTerminal 报告 stdin 是否挂在真人终端（char device）上——--trust-foreign 受信门
// 用来阻止 LLM agent（其 Bash 生成的 stdin 是管道）照拒绝文案自己的指引自我受信外来验收
// 命令的判别器。用变量（非函数）以便测试注入两侧。
//
// 已知局限（2026-08-16 复审）：mintty（Git Bash 默认终端）给原生进程的 stdin 是命名管道而非
// char device，真人在该终端下同样会被拒。刻意不提供就地逃生舱——任何 env/flag 旁路被注入的
// agent 同样设得了，等于重新打开本门要堵的自我受信洞。拒绝信息会探测 TERM_PROGRAM=mintty
// 并指引用户改用 ConPTY 终端（Windows Terminal / PowerShell）重跑。
var stdinIsHumanTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// runTaskVerifyAcceptanceAt is the root-injected core of runTaskVerifyAcceptance,
// split out so it can be unit-tested on a temp project (skipping findProjectRoot /
// cobra). It performs the same actually-run-and-judge acceptance loop and the same
// deterministic checklog:acceptance recording as runTaskVerifyAcceptance — see that
// godoc for the spec-as-gate rationale (forge runs each command itself, so the
// evidence cannot be faked; agents cannot merely assert 'satisfies acceptance').
// explicitRef pins the task directly (gate-family --ref parity); empty keeps the
// legacy active-task detection.
//
// runTaskVerifyAcceptanceAt 是 runTaskVerifyAcceptance 的 root 注入核心，独立出来便于
// 在临时项目上单测（不经 findProjectRoot / cobra）。实跑任务登记的每条验收标准
// （task start --accept），按「退出码 0 + Expected 子串」判定，回填 Passed/Output 到
// TaskState 并记一条 checklog:acceptance（deterministic——forge 自己跑命令看结果，不可伪造）。
// 这是把 dev-workflow Plan 的"Run: <cmd>, Expected: <out>"变成不可伪造实跑证据的入口，
// 对冲 agent 自述「满足验收」却没真跑的盲区（spec-as-gate）。explicitRef 直接指定任务
// （门禁族 --ref 一致性）；空串保持旧的活跃任务检测。
func runTaskVerifyAcceptanceAt(root, explicitRef string, trustForeign bool) error {
	var state *taskpipeline.TaskState
	var err error
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
	if !state.HasAcceptance() {
		fmt.Println("本任务未登记验收标准（forge task start --accept \"run :: expected\"）。")
		return nil
	}

	// Foreign-acceptance trust gate: Run commands that entered this TaskState via task import or
	// .forge migrate are attacker-authorable executable strings (a cloned malicious repo / a hostile
	// bundle). verify-acceptance executes them with full env — running unreviewed foreign commands is
	// arbitrary command execution steered by the very BLOCKED guidance this tool emits. So the first
	// execution demands an explicit, review-based opt-in: without --trust-foreign, print the command
	// list for human review and refuse (the marker is already on disk, so an untrusted refusal is
	// durable — nothing to persist in that branch); with it, clear the marker once under the
	// per-task lock (subsequent re-runs are local-verified evidence, not foreign payloads).
	//
	// --trust-foreign additionally requires a HUMAN terminal (stdin is a char device): the threat
	// model of this very fix is hostile content steering an LLM agent, and an injected agent can
	// simply follow the refusal text's own instruction and add the flag — the char-device check is
	// the one discriminator between a human shell and an agent's piped stdin.
	//
	// 外来验收受信门：经 task import 或 .forge migrate 进入本 TaskState 的 Run 命令是攻击者可
	// 书写的可执行字符串（clone 恶意仓库 / 敌意 bundle）。verify-acceptance 以完整环境执行它们
	// ——未审阅就跑外来命令等于被本工具自己的 BLOCKED 指引导向的任意命令执行。故首次执行要求
	// 显式的、基于审阅的受信：无 --trust-foreign 时打印命令清单供人工审阅并拒绝（标记已在盘上，
	// 未受信的拒绝天然持久——该分支无须落盘）；带 flag 则在 per-task 锁内一次性清除标记（之后
	// 的重跑是本机验证证据，非外来载荷）。
	//
	// --trust-foreign 额外要求真人终端（stdin 是 char device）：本修复的威胁模型正是恶意内容
	// 操纵 LLM agent，而被注入的 agent 大可直接照拒绝文案的指引加上 flag——char device 检查是
	// 区分人类 shell 与 agent 管道 stdin 的唯一判别器。
	if state.AcceptanceForeign && !trustForeign {
		fmt.Println("⚠ 本任务的验收命令来自外来源（task import / .forge migrate），未审阅前拒绝执行。命令清单：")
		for i, c := range state.Acceptance {
			fmt.Printf("  [%d] %s\n", i+1, c.Run)
		}
		fmt.Println("人工审阅以上命令后，在真人终端中加 --trust-foreign 重新执行；确认无害前不要受信。")
		return fmt.Errorf("acceptance commands are foreign-marked; review them then re-run with --trust-foreign in a human terminal")
	}
	trusted := state.AcceptanceForeign && trustForeign
	if trusted && !stdinIsHumanTerminal() {
		// mintty (Git Bash's default terminal) hands native processes a named pipe instead of a
		// char device — a real human is refused here too, with no in-place escape hatch: an
		// env/flag bypass an agent could also set would re-open the self-trust hole this gate
		// exists to close. The actionable path is switching to a ConPTY terminal.
		//
		// mintty（Git Bash 默认终端）给原生进程的 stdin 是命名管道而非 char device——真人
		// 在此同样被拒，且不提供就地逃生舱：agent 也能设置的 env/flag 旁路会重新打开本门
		// 要堵的自我受信洞。可行动路径是换 ConPTY 终端。
		if os.Getenv(`TERM_PROGRAM`) == `mintty` {
			return fmt.Errorf("--trust-foreign 须真人在终端运行：Git Bash/mintty 的 stdin 是命名管道（非 char device），无法与 agent 管道区分——请改用 Windows Terminal / PowerShell 等 ConPTY 终端执行本命令")
		}
		return fmt.Errorf("--trust-foreign 是人工审阅决策：须由真人在终端中运行（当前 stdin 非终端——agent/管道环境不得自我受信外来命令）")
	}
	// NOTE: the foreign marker is cleared only AFTER the run, together with the result merge below —
	// clearing it before the run would let a crash mid-run leave unmarked foreign commands runnable
	// without trust on the next attempt; fail-closed instead (a crash leaves the marker on and
	// re-demands --trust-foreign).
	//
	// 注意：外来标记改为实跑之后、随下方结果合并一并清除——先清后跑会让跑到一半崩溃的下次
	// 尝试在无受信的情况下执行已去标记的外来命令；改为 fail-closed（崩溃后标记仍在，重跑
	// 重新要求 --trust-foreign）。

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

	// Merge acceptance RESULTS onto the freshest on-disk state under the per-task lock (design
	// §13): a bare SaveTaskState of the pre-run snapshot would clobber concurrent resume/decide
	// continuity writes — a lost-update on exactly the data import exists to preserve. MutateTaskState
	// reloads inside the lock; results are matched onto the fresh acceptance spec by the
	// (Run, Expected) pair so a concurrently edited spec is never stamped with another command's
	// outcome, and the foreign marker (trusted branch) flips here too — after the run,
	// fail-closed (see NOTE above).
	//
	// 在 per-task 锁内把验收「结果」合并到最新盘上状态（设计§13）：裸 SaveTaskState 回写实跑前
	// 快照会覆盖并发 resume/decide 的接续写入——丢的恰是 import 要保的数据。MutateTaskState
	// 锁内重载；结果按 (Run, Expected) 二元组匹配到最新 acceptance spec 上，并发改过的 spec
	// 不会被盖上另一条命令的结果；外来标记（受信分支）也在此翻——实跑之后、fail-closed（见上 NOTE）。
	if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		taskpipeline.MergeAcceptanceResults(s, state.Acceptance, trusted)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save acceptance results: %w", err)
	}
	if trusted {
		fmt.Println("已受信外来验收命令（--trust-foreign）——验收实跑完成、外来标记已清除，后续重跑按本机证据处理。")
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
		// Fallback: legacy states completed by the OLD gate behavior (the last gate used
		// to MarkComplete before the 2026-08-18 deadlock fix, deactivating the task for
		// ActiveTaskState). Load via branch context so those tasks can still finalize.
		//
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

// runTaskCompleteAt is the root-injected core of runTaskComplete (split out for
// unit-testability, same pattern as runTaskVerifyAcceptanceAt). Everything after task
// resolution lives here. Ordering contract (dogfood 2026-08-18 deadlock fix):
// double-complete guard → generic path → IsComplete → acceptance pre-flight →
// MarkComplete → scoring → feedback → clear ref. MarkComplete happens ONLY after the
// pre-flight passes — a failing pre-flight must leave the task active so
// verify-acceptance (active-only) can refresh the stale snapshots and complete can be
// retried. (Before the fix, the LAST GATE marked completion, so a post-gate commit
// moved HEAD → pre-flight failed forever with no revival path.)
//
// runTaskCompleteAt 是 runTaskComplete 的 root 注入核心（独立可测，与
// runTaskVerifyAcceptanceAt 同款范式）。task 解析之后的一切都在这里。顺序契约
// （dogfood 2026-08-18 死锁修复）：重复完成守卫 → generic 路径 → IsComplete →
// acceptance pre-flight → MarkComplete → 评分 → 反馈 → 清 ref。MarkComplete 只在
// pre-flight 通过之后发生——pre-flight 失败必须保持任务 active，verify-acceptance
// （默认认 active；--ref 可显式指定）才能刷新过期快照、complete 才能重试。（修复前
// 最后一道 gate 就标记完成，gate 后的 commit 一移动 HEAD → pre-flight 永久失败且
// 无复活路径。）
func runTaskCompleteAt(root string, state *taskpipeline.TaskState) error {
	// Idempotent double-complete guard: a task already finalized (completed + scored,
	// or a completed generic task which never scores) must not re-run the completion
	// side effects (re-scoring, duplicate-HEAD warnings, a second Act conclusion).
	//
	// 幂等重复完成守卫：已 finalize 的任务（完成且已评分；或从不评分的已完成 generic
	// 任务）不得重跑完成副作用（重复评分、重复 HEAD 告警、第二条 Act 结论）。
	if state.CompletedAt != nil && (state.Score != nil || state.IsGeneric()) {
		fmt.Printf("Task %s already completed（已完成，幂等跳过）。\n", state.TaskRef)
		return nil
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
		return fmt.Errorf(`acceptance pre-flight 未通过（验收未实跑/快照过期/未通过）: %s；逃生（落 checklog 审计，降 evidence 强度到 Weak；重证据任务按证据缩放豁免）: forge task override --acceptance-gate disable 或 FORGE_ACCEPTANCE_GATE=disable`,
			strings.Join(reasons, `; `))
	}

	// doc pre-flight（输出→回检循环的流程节点）：任务变更了 markdown 产物时，
	// complete 前 L1 确定性 lint 全过 + L2 回检证据（DocReview fresh/Passed/
	// ≥75 分）+ 零未决 Critical。无文档产物放行；逃生舱与 acceptance 对称。
	// 设计：docs/design/output-readability-gates.md（飞书《AI 产物可读性差调研
	// 设计》落地方案二）。
	//
	// doc pre-flight (the process node of the output→re-check loop): when the
	// task changed markdown deliverables, before complete every changed doc
	// passes L1 deterministic lint + L2 re-check evidence (DocReview fresh/
	// Passed/score ≥75) + zero unresolved Criticals. No doc deliverables →
	// pass; escape hatch symmetric to acceptance.
	if ok, reasons := taskpipeline.CheckDocGate(root, state); !ok {
		return fmt.Errorf(`doc gate 未通过（文档产物未过 L1 lint / L2 回检）: %s；流程：forge docs lint <paths> 修 L1 → 按 code-review-gate/references/rubric-docs.md 评审（产出者不能自检）→ forge task doc-review 记录证据。逃生（落 checklog 审计，降 evidence 强度到 Weak）: forge task override --doc-gate disable 或 FORGE_DOC_GATE=disable`,
			strings.Join(reasons, `; `))
	}

	// MarkComplete 恰在此处（pre-flight 之后）：完成标记属于 `forge task complete` 的整个
	// 动作而非某道 gate（dogfood 2026-08-18 死锁修复的另一面——见 runTaskGate 的对应注释）。
	// firstComplete 门控（review m1）：仅首次完成时标记——评分失败留下的 CompletedAt!=nil、
	// Score==nil 中间态重试时，再评分是预期恢复，但 CompletedAt 不得被重置（污染
	// duration 度量）、Act 结论不得二次追加（act.Append 无 TaskRef 去重）。
	// 先落盘再评分，评分失败也不丢完成状态。
	//
	// MarkComplete exactly here (after the pre-flight): completion belongs to `forge
	// task complete`'s whole action, not to any single gate (the other face of the
	// dogfood 2026-08-18 deadlock fix — see the matching comment in runTaskGate).
	// The firstComplete gate (review m1): mark ONLY on the first completion — retrying
	// the CompletedAt!=nil/Score==nil intermediate state a scoring failure leaves
	// behind SHOULD re-score (expected recovery), but must not reset CompletedAt
	// (pollutes duration metrics) nor append a second Act conclusion (act.Append has
	// no TaskRef dedup). Persist before scoring so a scoring failure cannot lose the
	// completed state.
	firstComplete := state.CompletedAt == nil
	if firstComplete {
		state.MarkComplete()
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
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
	// (stderr, so --json stdout stays clean). Only on firstComplete (review m1) — a
	// scoring-failure retry must not append a second conclusion for the same task.
	// When no Nudge fires (strong evidence + high score), sedimentReminder still prints
	// one soft line: clean tasks also produce reusable lessons (the 2026-08-18
	// case-split/CI-sweep sessions scored A yet yielded several), and before this the
	// sediment evaluation depended entirely on the user remembering to ask.
	//
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
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
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

// runTaskDocReview records the L2 doc re-check evidence after a rubric review
// (code-review-gate/references/rubric-docs.md). The gate (CheckDocGate) is the
// consumer: complete is refused until the review is recorded, fresh, Passed and
// score ≥ threshold. Recording alone never fakes a pass — --passed fail keeps
// the task blocked and counts a round toward the escalation cap. Critical
// findings land as Findings (Source=doc-review, Severity=critical) and block
// until resolved via forge task finding resolve.
//
// runTaskDocReview 在 rubric 评审（code-review-gate/references/rubric-docs.md）
// 后记录 L2 文档回检证据。门禁（CheckDocGate）是消费方：评审未记录、过期、
// 未通过或得分低于阈值时 complete 被拒。仅记录不会伪造通过——--passed fail
// 保持阻断并累加轮次（升级上限的计数）。Critical 发现落 Findings
// （Source=doc-review、Severity=critical），经 forge task finding resolve 解决。
func runTaskDocReview(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
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
		return fmt.Errorf(`--passed 必填且只接受 pass | fail，got %q（先按 code-review-gate/references/rubric-docs.md 评审——产出者不能当回检者）`, passedFlag)
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
		// Auto-increment from the previous recorded round: review round 2 must
		// not silently restart at 1 (the escalation cap counts real rounds).
		//
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

	// Round history retention (the loop's observable convergence): prior rounds
	// stay in DocReviewHistory so the score trend is queryable from task state —
	// "two rounds without Criticals dropping" is an anomaly signal, not prose.
	// Capped at the last 10 rounds (memory hygiene).
	//
	// 轮次历史保留（循环的可观测收敛）：历史轮次留在 DocReviewHistory，得分
	// 趋势可从任务状态查询——「两轮之间 Critical 不降」是异常信号而非散文。
	// 截断保留最近 10 轮（内存卫生）。
	if state.DocReview != nil && !state.DocReview.ReviewedAt.IsZero() {
		state.DocReviewHistory = append(state.DocReviewHistory, *state.DocReview)
		if len(state.DocReviewHistory) > 10 {
			state.DocReviewHistory = state.DocReviewHistory[len(state.DocReviewHistory)-10:]
		}
	}

	state.DocReview = &taskpipeline.DocReview{
		Passed:          passedFlag == "pass",
		RubricScore:     score,
		Round:           round,
		Reviewer:        reviewer,
		ReviewedAt:      time.Now(),
		HeadCommit:      taskpipeline.GetHeadCommit(root),
		DocsFingerprint: taskpipeline.DocContentFingerprint(root, state),
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to save task state: %w", err)
	}

	fmt.Printf("doc-review 已记录（round %d，score %d，verdict %s，critical +%d）。\n", round, score, passedFlag, len(criticals))
	if state.DocReview.Passed && score < taskpipeline.DocRubricThreshold {
		fmt.Printf("注意：verdict=pass 但 score %d < 阈值 %d——doc gate 仍会拦截（得分与结论矛盾，复评）。\n", score, taskpipeline.DocRubricThreshold)
	}
	if !state.DocReview.Passed && round >= taskpipeline.DocReviewMaxRounds {
		fmt.Printf("已 %d 轮未过（上限 %d）——升级人工确认：用户裁定放行（forge task override --doc-gate disable）或给出下一轮修复方向。\n", round, taskpipeline.DocReviewMaxRounds)
	}
	return nil
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
	// Review-rework loop metric (result indicator for whether the review process is converging).
	// Informational only — not part of the weighted score.
	//
	// 审查-返工循环度量（检验审查流程是否收敛的结果指标）。仅信息展示——不进加权总分。
	if ev := state.Score.Evidence; ev != nil && (ev.ReviewPasses > 0 || ev.CompleteRejections > 0) {
		fmt.Printf("  返工轮次: review pass %d 次 / task-complete 被拒 %d 次\n", ev.ReviewPasses, ev.CompleteRejections)
	}
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
func appendConclusion(root string, state *taskpipeline.TaskState) (string, bool) {
	_, directive, err := taskpipeline.AppendConclusion(root, state)
	if err != nil {
		fmt.Fprintln(os.Stderr, `Warning:`, err)
		return ``, false
	}
	return directive, true
}

// sedimentReminder decides the single line printed at task complete: the Act
// directive when a RetrospectiveNudge fired (it already ends with the sediment
// action entry), otherwise one soft reminder that clean completions can still
// yield reusable lessons. Deterministic and host-independent — unlike a
// model-side trigger, it cannot be lost to a dead Stop channel or a forgotten
// prompt. The judgement of WHAT deserves sediment is delegated to
// session-retrospective's own no-sediment list (common knowledge / one-off
// details / anything the code already records), so the line stays noise-bounded.
//
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
	slices.SortFunc(tasks, func(a, b *taskpipeline.TaskState) int {
		return a.StartedAt.Compare(b.StartedAt)
	})
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
