package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/MjxUpUp/Forge/internal/worktree"
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
	// StringArray（非 StringSlice）：cobra/pflag 的 StringSlice 默认按逗号切分，会把
	// 含逗号的命令拆坏；StringArray 每个 --accept 整条不切。验收标准是完整"run :: expected"串。
	taskStartCmd.Flags().StringArray("accept", nil, `验收标准（可重复 --accept）：格式 "run :: expected"（expected=输出子串）或裸 "run"（只看退出码 0）。forge task verify-acceptance 实跑回扣。run 为 go test 且带 expected 而未加 -v 时自动补 -v（否则无 PASS 行永不匹配）`)
	// PlanScope：开工前声明计划改动的文件白名单（规划前置 → 可度量契约）。
	// 支持精确路径/glob/目录前缀。advisory：实改超出声明记
	// scope-drift（checklog），不阻塞（变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract）。
	taskStartCmd.Flags().StringArray("scope", nil, `计划改动文件白名单（可重复 --scope）：精确路径 internal/cli/task.go / glob internal/cli/*.go / 目录前缀 internal/cli。开工前声明，advisory 检测 scope-drift；中途可用 forge task scope add 追加`)
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
	// Per-task 僵尸 TTL 覆盖（设计 §3/§9 --ttl）：需按自己的时钟失联的分派——比全局 7d 默认更快
	//（短时效）或更慢（长跑任务）——设此项。零（不带 flag）保持全局常量，完全向后兼容。
	// health.effectiveTTL 读取它。
	taskStartCmd.Flags().Duration(`ttl`, 0, `每任务僵尸 TTL，覆盖全局 7d 默认（如 24h/30m/72h）；0=用全局（向后兼容）。offered/claimed/input-required 超此时长无活动即标僵尸`)
	taskStartCmd.Flags().String(`ref`, ``, `任务引用（如 feat/add-auto-branch），默认从分支名推断`)
	taskStartCmd.Flags().String("from-issue", "", "外部 issue URL（linear/github），解析为 task.ExternalOrigin 锚定外部 issue（衔接 spawn 式编排器）")
	taskStartCmd.Flags().Bool("branch", false, "从 main/master 创建新分支并切换（ref 作为分支名）")
	taskStartCmd.Flags().Bool("worktree", false, "在 repo 树外为此任务创建独立 worktree+分支+绑定（multi-task-concurrency L4；需 --ref）")
	taskStartCmd.Flags().String("base", "", "--worktree 的基线 ref（默认主线 main/master）")
	taskStartCmd.Flags().String("wt-dir", "", "--worktree 的父目录（默认 <repo 父目录>/<repo 名>-wt/）")
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
	taskDocReviewCmd.Flags().Int("score", 0, "rubric 四维总分 0-100（判据见 doc-review skill）")
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
	Short: "记录 L2 文档回检证据（按 doc-review skill 评审后落档；doc gate 消费）",
	RunE:  runTaskDocReview,
}

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

// nonGitTaskWarning 是项目非 git 仓库时 `task start` 打印的降级模式提示。
// forge 设计上 git-optional——门禁照常过、`complete` 照常评分——但 agent 须
// 知道哪些评分维度失真，免得把中性分读成管道坏了。提到 `abort`：用户不想继续
// 的降级任务正是 abort 存在的场景。
func nonGitTaskWarning() string {
	return "⚠️ 当前项目不是 git 仓库。forge 以降级模式运行：门禁照常通过、任务可完成评分，但以下评分维度将不可用或偏低：\n" +
		"  - 变更范围 (scope)：无 git diff，固定中性分\n" +
		"如需完整质量保障，执行 `git init`（任务流程本身可继续）。任务无法推进或临时放弃时用 `forge task abort --ref <ref>` 清理。"
}

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
	// 宿主注入的 shell env（目前仅 claude-code 的 CLAUDE_CODE_SESSION_ID）——
	// 注册表驱动，见 hostcap.Host.ShellSessionEnv。
	if host, _ := hostcap.ProbeShellIdentity(); host != "" {
		return host
	}
	return ""
}

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

func runTaskStart(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	explicitRef, _ := cmd.Flags().GetString("ref")
	title, _ := cmd.Flags().GetString("title")
	createBranch, _ := cmd.Flags().GetBool("branch")
	useWorktree, _ := cmd.Flags().GetBool("worktree")

	// --worktree (multi-task-concurrency §7, L4): create the isolated workspace FIRST,
	// then run the whole start flow rooted there — task state lands in the shared DataDir
	// (worktrees share the project key by design), the binding anchors the NEW path, and
	// the session pointer is unchanged. The created worktree is deliberately kept on any
	// later failure (宁留勿删): the guidance line tells the user how to clean up.
	//
	// --worktree（multi-task-concurrency §7，L4）：先建隔离 workspace，再以它为根跑
	// 整个 start 流程——任务状态落共享 DataDir（worktree 按设计共享 project key），
	// 绑定锚定【新】路径，会话指针不变。后续步骤失败时刻意保留 worktree（宁留勿
	// 删）：指引行告知用户如何清理。
	if useWorktree {
		// --worktree 自带分支创建（createTaskWorktree 内的 deriveBranchName）；
		// 与 --branch 组合必在 worktree 副作用【之后】命中主检出守卫而失败，
		// 留下孤儿 worktree。在任何磁盘副作用前拒绝该组合。
		if createBranch {
			return fmt.Errorf("--worktree 与 --branch 互斥：--worktree 自带任务分支创建（可用 --wt-dir/--base 定制），无需 --branch")
		}
		base, _ := cmd.Flags().GetString("base")
		wtDir, _ := cmd.Flags().GetString("wt-dir")
		wtRoot, werr := createTaskWorktree(root, explicitRef, base, wtDir)
		if werr != nil {
			return werr
		}
		root = wtRoot
		// stdout belongs to the --json contract (machine-parseable only); human hints go to stderr.
		fmt.Fprintf(cmd.ErrOrStderr(), "worktree 已创建: %s（如本次启动失败可 git worktree remove 清理）\n", wtRoot)
		fmt.Fprintf(cmd.ErrOrStderr(), "下一步：cd %s 并重开窗口，或直接 forge task resume 接续\n", wtRoot)
	}

	// --branch：从 main/master 创建新分支并切过去。
	if createBranch {
		if explicitRef == "" {
			return fmt.Errorf("--branch requires --ref (e.g., --ref feat/add-auto-branch)")
		}
		// dogfood 发现 #6（2026-08-28，conventions-profile 会话审计）：--branch 此前
		// 直接校验 ref 本身，非惯例前缀 ref 被拒——与 --worktree 共享 deriveBranchName
		// 派生（feat/<ref 斜杠转连字>）。
		branchName, err := deriveBranchName(explicitRef)
		if err != nil {
			return err
		}
		detected := taskcontext.Detect(root)
		if !isMainBranch(detected.Branch) {
			return fmt.Errorf("--branch can only be used on main/master (current: %s)", detected.Branch)
		}
		if err := createAndSwitchBranch(root, branchName); err != nil {
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

	// 检查 task 是否已存在。只有确证的 ErrNotExist 才落到创建路径——串号冲突
	//（两个 ref 折叠到同一文件名）必须中止，而不是被新任务的 SaveTaskState 覆盖。
	existing, err := taskpipeline.LoadTaskState(root, ctx.TaskRef)
	if err == nil && existing != nil {
		return fmt.Errorf("task %q already exists (started at %s). Use 'forge task status' to check progress",
			ctx.TaskRef, existing.StartedAt.Format("2006-01-02 15:04"))
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("检查既有任务 %q 失败（可能是 ref 串号冲突，拒绝覆盖）: %w", ctx.TaskRef, err)
	}

	state := taskpipeline.NewTaskState(ctx)

	// 节点租约（sync-convergence.md §4）：开工即为本机认领任务——v1 仅 advisory，
	// fail-open（身份问题绝不阻塞开工）。
	taskpipeline.ClaimLeaseForCurrentNode(state)

	// 记录当前 HEAD 用于重复检测。
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
	// Per-task TTL 覆盖（设计 §3/§9 --ttl）：持久化进 state.TTL，使 health.effectiveTTL 按本任务自己
	// 的时钟标失联，独立于全局 7d 常量。零（无 --ttl）留 state.TTL 于零值回落——legacy 任务无行为变化。
	if ttl, _ := cmd.Flags().GetDuration("ttl"); ttl > 0 {
		state.TTL = ttl
	}
	// planAcceptanceAdded：--plan-file 提取后实际新增入库的条数（净增，扣除与显式 --accept
	// 去重的部分），仅供下方成功输出标注来源。用 merge 后的 len 差值而非提取前的 len(extracted)——
	// 后者在 --accept 共存时会数进去被去重丢弃的条目，误导用户以为它们进了 state。
	planAcceptanceAdded := 0
	hasPlanInput := false
	if goal, _ := cmd.Flags().GetString("goal"); goal != "" {
		hasPlanInput = true
	}
	if planFile, _ := cmd.Flags().GetString("plan-file"); planFile != "" {
		hasPlanInput = true
		planData, err := os.ReadFile(planFile)
		if err != nil {
			return fmt.Errorf("读取 --plan-file %q 失败: %w", planFile, err)
		}
		state.Plan = string(planData)
		// L6（multi-task-concurrency §9）：plan 同时作为首个 specs 产物落文件
		//（harness repo tracked），TaskState 持哈希引用（I5：文件拥有内容，状态只
		// 指向）。best-effort——产物写失败不阻断任务创建。
		if aref, aerr := taskpipeline.WriteArtifact(root, ctx.TaskRef, "plan", state.Plan); aerr == nil {
			if state.SpecArtifacts == nil {
				state.SpecArtifacts = map[string]taskpipeline.ArtifactRef{}
			}
			state.SpecArtifacts["plan"] = aref
		}
		// 从 Plan markdown 自动提取验收标准（Run:/Expected: 块），消除把 plan 的 Run/Expected
		// 手抄到 --accept 的断口（dogfood：靠自觉手抄必漏；没抄时 acceptance advisory 零信号）。
		// 显式 --accept 优先，plan 提取按 Run 去重补充（MergeAcceptance）。
		if extracted := taskpipeline.ParseAcceptanceFromPlan(state.Plan); len(extracted) > 0 {
			baseBefore := len(state.Acceptance)
			state.Acceptance = taskpipeline.MergeAcceptance(state.Acceptance, extracted)
			planAcceptanceAdded = len(state.Acceptance) - baseBefore
		}
	}
	// go test 人体工学（usage 日志修复）：`go test` 不带 -v 时输出没有 PASS 行，Expected
	// 写 "PASS" 永不匹配，agent 到 verify-acceptance 才发现——真实失败模式是 abort 重开
	// 任务。自动补 -v（只影响输出、退出码语义不变）并明示——绝不静默改写登记的命令。
	// Expected 为空（只看退出码）的命令不动：它们不需要 verbose 输出。
	if adjusted := taskpipeline.EnsureGoTestVerbose(state.Acceptance); len(adjusted) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "ℹ️ 验收命令自动补 -v（go test 无 -v 时输出无 PASS 行，Expected 子串永不匹配）：%s\n", strings.Join(adjusted, ", "))
	}
	if parent, _ := cmd.Flags().GetString("parent"); parent != "" {
		state.ParentTaskRef = parent
	}
	originTool, _ := cmd.Flags().GetString("origin-tool")
	state.OriginTool = resolveOriginTool(root, originTool)

	// 外部 issue origin：把 task 的来源从 branch 扩展到外部 issue（linear/github），
	// 衔接 spawn 式编排器（Symphony 类）——编排器拉起 run 时 task 天然关联 issue，不靠 branch 推断。
	if fromIssue, _ := cmd.Flags().GetString(`from-issue`); fromIssue != `` {
		state.ExternalOrigin = taskpipeline.ParseExternalOriginURL(fromIssue)
	}
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

	// 确保 session 存在并把 task 链上去。
	session, err := taskpipeline.EnsureSession(root, sid)
	if err != nil {
		// Degrade without anchoring is fine, but silent — a missing SessionID later
		// suppresses phase-blast warnings and timeline bucketing; keep it attributable.
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to ensure session %q: %v\n", sid, err)
	} else {
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
	// harness repo 批量提交（multi-task-concurrency §13 提交策略：任务边界触发，
	// 绝不逐 hook——延迟预算）。best-effort 静默。
	HarnessCommitBestEffort("task started: " + ctx.TaskRef)

	// 清理超过 retention 窗口的已完成 task state 文件，保持 DataDir/tasks/ 有界。
	// 与 log 归档同窗口，让 task 元数据与其 log 同步淘汰。best-effort：此处错误不致命。
	if days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); days > 0 {
		taskpipeline.PruneOldTasks(root, time.Now().AddDate(0, 0, -days))
	}

	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	// 标记为 active task（让 hook 检测无歧义）。
	// session-scoped，并发 session 不会互相覆盖。
	if err := taskpipeline.SetActiveTaskRef(root, sid, ctx.TaskRef); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to set active task ref: %v\n", err)
	}

	// L1 workspace 绑定（multi-task-concurrency §4）：把 cwd 锚到任务上，使本目录/
	// worktree 里的【新】窗口无需任何会话存活即可解析到它。尽力而为——绑定失败只
	// 降级为会话指针/分支解析。
	if err := worktree.BindTask(root, ctx.TaskRef, state.Branch, sid); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to bind workspace: %v\n", err)
	}

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
	// 使用偏离引导（conventions-profile 会话审计，2026-08-28；均 advisory 不阻断）：
	// (a) L4 未用 worktree——主检出直接开任务分支是受支持的降级形态，但多任务并发
	//     时未提交变更共堆一处正是隔离设计要消的风险面；仅在其他未完成任务存在时
	//     提示（单任务用户零打扰）。
	// (b) L6 产物链未用——plan/goal 空白则接续字段全空（task-implement 的 plan-first
	//     advisory 在门禁才响，这里 shift-left 一行）。
	if !useWorktree {
		if others, _ := taskpipeline.ListTaskStates(root); countOtherIncomplete(others, ctx.TaskRef) > 0 {
			fmt.Fprintf(os.Stderr, "提示：当前有其他未完成任务，本任务在主检出开分支（共享工作树）——多任务并发建议 forge task start --worktree 获得独立工作树（multi-task-concurrency L4）\n")
		}
	}
	if !hasPlanInput {
		fmt.Fprintf(os.Stderr, "提示：本任务无 --plan-file/--goal——接续字段将为空（压缩/换窗口后现场靠 git 猜）；建议 task start 带 --plan-file <方案> 或 --goal <目标>，中途用 forge task decide/next 落盘（multi-task-concurrency L6）\n")
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

// isMainBranch 检查分支名是否为 main/master。
func isMainBranch(branch string) bool {
	lower := strings.ToLower(branch)
	return lower == "main" || lower == "master"
}

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

// countOtherIncomplete counts incomplete tasks other than the given ref — the worktree
// nudge's trigger（conventions-profile 会话审计引导，2026-08-28；advisory only，单任务
// 用户零打扰）。
//
// countOtherIncomplete 数除指定 ref 外的未完成任务数——worktree 引导的触发条件
// （conventions-profile 会话审计引导，2026-08-28；仅 advisory，单任务用户零打扰）。
func countOtherIncomplete(states []*taskpipeline.TaskState, ownRef string) int {
	n := 0
	for _, s := range states {
		if s != nil && s.TaskRef != ownRef && s.CompletedAt == nil {
			n++
		}
	}
	return n
}
