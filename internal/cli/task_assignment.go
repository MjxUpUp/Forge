package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/agentsignals"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_assignment.go: the multi-agent delegation command layer. A task transitions through an
// A2A lifecycle — offered (orchestrator assigns) → claimed (worker accepts) → delivered
// (worker hands back). The state machine lives in types.go (AssignTo/Claim/Deliver/...);
// these commands are the thin CLI surface that drives it and keep the worker's session
// anchored to the claimed task (claim = "I take it", which is exactly the multi-directional
// anchoring "successor attaches" action). mine lets a worker discover what is offered to it.
//
// task_assignment.go：多 agent 分派命令层。任务经 A2A 生命周期——offered（编排器分派）→
// claimed（工作方认领）→ delivered（工作方交回）。状态机在 types.go（AssignTo/Claim/
// Deliver/...）；本命令是驱动它的薄 CLI 表层，并把工作方 session 锚定到所认领的任务
// （claim = 我接手，正是多向锚定的「接手方 attach」动作）。mine 让工作方发现分派给自己的任务。
var taskAssignCmd = &cobra.Command{
	Use:   `assign --ref <ref> --to <agent> [--role <role>] [--by <tool>]`,
	Short: `把任务分派给指定 agent（offered 起步，编排器侧）`,
	Long: `forge task assign 是多 agent 分派的入口：编排器把一个已存在的任务交给指定 agent，
任务进入 offered 态等待对方 claim。--to 建议是 agentsignals 已知 agent（kimi/reasonix/cursor/
copilot/windsurf/codex/opencode/cline/claude-code）；未知 agent 仅警告但仍接受（如 codebuddy
这类无项目标记的 agent，需用户显式确认）。
创建任务时即可用 forge task start --assignee <agent> 一步到位；assign 用于给已存在任务追加分派。`,
	RunE: runTaskAssign,
}

var taskClaimCmd = &cobra.Command{
	Use:   `claim --ref <ref> [--as <agent>]`,
	Short: `工作方认领分派给自己的任务（offered→claimed，并把当前 session 锚定到该任务）`,
	RunE:  runTaskClaim,
}

var taskDeliverCmd = &cobra.Command{
	Use:   `deliver --ref <ref> [--as <agent>]`,
	Short: `工作方交付任务（claimed→delivered，交回编排器）`,
	RunE:  runTaskDeliver,
}

var taskMineCmd = &cobra.Command{
	Use:   `mine [--agent <agent>] [--role <role>] [--all-projects] [--blocked] [--json]`,
	Short: `列出分派给当前/指定 agent 的任务（offered 待认领 + 已处理历史；--blocked 只看被上游依赖卡住的）`,
	RunE:  runTaskMine,
}

// 失败/回抛/撤回路径（设计阶段2）：question/answer/fail/cancel/reopen。状态机方法在 types.go
// 已就绪（phase 1 预留 + assignment_test 覆盖），本层是薄 CLI 表层驱动它们——与 assign/claim/
// deliver 同构（loadTaskOrActive + MutateTaskState + 方法调用 + 输出指引下一步）。
//
// Failure/clarify/withdraw paths (design phase 2): question/answer/fail/cancel/reopen. The state-
// machine methods are already in place in types.go (phase 1 reservation + assignment_test coverage);
// this layer is the thin CLI surface driving them — isomorphic with assign/claim/deliver
// (loadTaskOrActive + MutateTaskState + method call + output guiding the next step).
var taskQuestionCmd = &cobra.Command{
	Use:   `question --ref <ref> --content <text>`,
	Short: `工作方回抛问题（claimed→input-required，暂停等编排器/人答复）`,
	RunE:  runTaskQuestion,
}
var taskAnswerCmd = &cobra.Command{
	Use:   `answer --ref <ref> [--content <text>]`,
	Short: `编排器答复回抛（input-required→claimed，答复记入 Decisions 可追溯）`,
	RunE:  runTaskAnswer,
}
var taskFailCmd = &cobra.Command{
	Use:   `fail --ref <ref> --reason <text>`,
	Short: `工作方标记任务失败（claimed→failed，记录原因）`,
	RunE:  runTaskFail,
}
var taskCancelCmd = &cobra.Command{
	Use:   `cancel --ref <ref> --reason <text>`,
	Short: `编排器撤回分派（offered/claimed/input-required→canceled，记录原因）`,
	RunE:  runTaskCancel,
}
var taskReopenCmd = &cobra.Command{
	Use:   `reopen --ref <ref> --reason <text>`,
	Short: `交付后重开（delivered→claimed，交付后发现 bug）`,
	RunE:  runTaskReopen,
}

var taskReclaimCmd = &cobra.Command{
	Use:   `reclaim [--dry-run] [--json]`,
	Short: `回收 claimed 僵尸任务（claimed>TTL 无 checklog 活动）回 offered`,
	Long: `forge task reclaim 扫描当前项目的 claimed 任务，把认领方失联（超过默认 TTL 7 天无
checklog 活动）的任务回收到 offered，重置认领时钟——补齐设计 §3 的 TTL 回收接线
（Abandon 原语 + IsClaimedStale 检测先前已就绪，只缺触发）。

回收（Abandon）保留 Assignment.Agent 不变，而 Claim 要求认领 agent 与分派 agent 一致，故回收后
只有「原认领 agent」能在新会话（崩溃/重启后）重新认领——这正是 TTL 回收的场景。要改派给别的 agent
请用 forge task cancel + forge task assign。复用 task health 的僵尸检测同一真相源（IsClaimedStale），
故 health 报告的 claimed>TTL 与本命令回收的目标永远一致。--dry-run 只列出将回收的任务不改状态；
--json 输出 {reclaimed, dry_run, count}。offered/delivered/终态任务不受影响（Abandon 只接受 claimed）。`,
	RunE: runTaskReclaim,
}

func init() {
	taskCmd.AddCommand(taskAssignCmd)
	taskCmd.AddCommand(taskClaimCmd)
	taskCmd.AddCommand(taskDeliverCmd)
	taskCmd.AddCommand(taskMineCmd)
	taskCmd.AddCommand(taskQuestionCmd)
	taskCmd.AddCommand(taskAnswerCmd)
	taskCmd.AddCommand(taskFailCmd)
	taskCmd.AddCommand(taskCancelCmd)
	taskCmd.AddCommand(taskReopenCmd)
	taskCmd.AddCommand(taskReclaimCmd)

	taskAssignCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskAssignCmd.Flags().String(`to`, ``, `分派给哪个 agent（如 kimi/reasonix/cursor）`)
	taskAssignCmd.Flags().String(`role`, ``, `分派角色（如 frontend/backend/testing）`)
	taskAssignCmd.Flags().String(`by`, ``, `分派发起方（工具/人，默认探测当前工具）`)

	taskClaimCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskClaimCmd.Flags().String(`as`, ``, `以哪个 agent 身份认领（默认探测当前工具）`)

	taskDeliverCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskDeliverCmd.Flags().String(`as`, ``, `以哪个 agent 身份交付（与 claim --as 对齐；与分派 agent 不符时仅提醒不阻断）`)

	taskMineCmd.Flags().String(`agent`, ``, `查询哪个 agent 的分派（默认探测当前工具）`)
	taskMineCmd.Flags().String(`role`, ``, `只看指定角色的分派`)
	taskMineCmd.Flags().Bool(`all-projects`, false, `全局扫描所有已登记 project（标注 project 归属，不自动 resume）`)
	taskMineCmd.Flags().Bool(`blocked`, false, `只看被上游依赖卡住的任务（DependsOn 未全交付）`)
	taskMineCmd.Flags().Bool(`json`, false, `JSON 格式输出`)

	taskQuestionCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskQuestionCmd.Flags().String(`content`, ``, `回抛的问题内容（必填）`)
	taskAnswerCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskAnswerCmd.Flags().String(`content`, ``, `答复内容（空则仅恢复 claimed 不记决策）`)
	taskFailCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskFailCmd.Flags().String(`reason`, ``, `失败原因（必填）`)
	taskCancelCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskCancelCmd.Flags().String(`reason`, ``, `撤回原因（必填）`)
	taskReopenCmd.Flags().String(`ref`, ``, `任务引用（不依赖分支检测）`)
	taskReopenCmd.Flags().String(`reason`, ``, `重开原因（交付后发现的问题，必填）`)

	taskReclaimCmd.Flags().Bool(`dry-run`, false, `只列出将回收的任务，不修改状态`)
	taskReclaimCmd.Flags().Bool(`json`, false, `JSON 格式输出`)
}

// warnIfUnknownAgent writes a warning to w when name is absent from the known-agent set,
// WITHOUT rejecting — marker-less agents (codebuddy) are legitimate when the user insists.
// Centralized so `task assign` and `task start --assignee` share ONE black-hole guard: a
// typo'd --assignee/--to would otherwise silently create a task no `mine` can match (mine
// filters by Assignment.Agent exact string). Writes to w (stderr) so it never pollutes stdout
// and JSON consumers stay clean.
//
// warnIfUnknownAgent 在 name 不属于已知 agent 集时写 w 警告但不拒绝——无标记 agent（codebuddy）
// 在用户坚持时合法。集中化使 task assign 与 task start --assignee 共享同一防黑洞守卫：否则拼错的
// --assignee/--to 会静默创建 mine（按 Assignment.Agent 精确串过滤）匹配不到的任务。写 w（stderr）
// 绝不污染 stdout，JSON 消费者保持干净。
func warnIfUnknownAgent(w io.Writer, name string) {
	if agentsignals.IsKnownAgent(name) {
		return
	}
	fmt.Fprintf(w, `⚠ agent %q 不在已知集（%s），task mine 无法自动匹配；确认该 agent 用 --as 显式认领`, name, strings.Join(agentsignals.KnownAgents(), `/`))
	fmt.Fprintln(w)
}

func runTaskAssign(cmd *cobra.Command, args []string) error {
	to, _ := cmd.Flags().GetString(`to`)
	if to == `` {
		return fmt.Errorf(`--to 必填（分派给哪个 agent）。已知 agent: %s`, strings.Join(agentsignals.KnownAgents(), `/`))
	}
	// Soft validation shared with `task start --assignee` via warnIfUnknownAgent — see that
	// helper for why unknown agents are warned but not rejected (black-hole guard).
	warnIfUnknownAgent(cmd.ErrOrStderr(), to)
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	role, _ := cmd.Flags().GetString(`role`)
	by, _ := cmd.Flags().GetString(`by`)
	if by == `` {
		by = resolveOriginTool(root, ``)
	}
	var status string
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		if err := s.AssignTo(to, role, by); err != nil {
			return err
		}
		status = s.Assignment.Status
		return nil
	})
	if err != nil {
		return fmt.Errorf(`分派失败: %w`, err)
	}
	fmt.Printf(`✓ 任务 %s 已分派给 %s（角色=%s，状态=%s）`, state.TaskRef, to, role, status)
	fmt.Println()
	fmt.Println(`对方用 forge task claim --ref ` + state.TaskRef + ` 认领；forge task mine 查看分派给自己的任务`)
	// #7（设计§14）：task 的 findings/decisions 是跨 agent 共享的——被分派方 claim/resume 后即见。
	// 分派时提醒编排器：你记的 findings/decisions 会被对方看到，若含敏感内容先审视。
	fmt.Fprintf(cmd.ErrOrStderr(), `⚠ 该任务的 findings/decisions 将对被分派方 %s 可见（跨 agent 共享）；若含敏感内容请先审视`, to)
	fmt.Fprintln(cmd.ErrOrStderr())
	return nil
}

func runTaskClaim(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	as, _ := cmd.Flags().GetString(`as`)
	if as == `` {
		as = resolveOriginTool(root, ``)
	}
	if as == `` {
		return fmt.Errorf(`无法探测当前 agent（无 agent env）。跨工具认领请显式传 --as <agent>（如 kimi/reasonix/cursor）`)
	}
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		return s.Claim(as)
	})
	if err != nil {
		return fmt.Errorf(`认领失败: %w`, err)
	}
	// claim is the "successor attaches" action of multi-directional anchoring: the worker now
	// owns the task, so anchor this session to it (same effect as forge task attach, but
	// implicit in the claim). Anchor failures are non-fatal — the claim itself already
	// succeeded and the task state is what continuity reads from.
	//
	// claim 即多向锚定的「接手方 attach」：工作方此刻拥有任务，故把当前 session 锚定过去
	// （等价 forge task attach，但 claim 内含）。锚定失败不致命——认领本身已成功，而任务
	// state 才是接续读取的真相源。
	if sid := taskpipeline.CurrentSessionID(); sid != `` {
		if err := taskpipeline.SetActiveTaskRef(root, sid, state.TaskRef); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), `⚠ session 锚定失败（不影响认领）: %v`, err)
			fmt.Fprintln(cmd.ErrOrStderr())
		}
	}
	fmt.Printf(`✓ 已认领任务 %s（%s）。交付时用 forge task deliver --ref %s`, state.TaskRef, as, state.TaskRef)
	fmt.Println()
	return nil
}

func runTaskDeliver(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	// --as parity with claim (usage-log fix: `claim --as kimi` succeeded but
	// `deliver --as kimi` errored "unknown flag", wasting a turn). Deliver needs no
	// identity for the state machine, so the flag is interface consistency only —
	// soft-checked against the assigned agent, advisory, never blocks the delivery.
	//
	// --as 与 claim 对齐（usage 日志修复：`claim --as kimi` 成功后 `deliver --as kimi`
	// 报 unknown flag 白跑一轮）。状态机的 Deliver 不需要身份，故该 flag 只是接口
	// 一致性——与分派 agent 不符时仅提醒，绝不阻断交付。
	as, _ := cmd.Flags().GetString(`as`)
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		return s.Deliver()
	})
	if err != nil {
		return fmt.Errorf(`交付失败: %w`, err)
	}
	if as != `` && state.Assignment != nil && state.Assignment.Agent != as {
		fmt.Fprintf(cmd.ErrOrStderr(), `⚠ 交付身份 %q 与分派 agent %q 不符（已交付；若非预期请核对认领身份）`, as, state.Assignment.Agent)
		fmt.Fprintln(cmd.ErrOrStderr())
	}
	fmt.Printf(`✓ 任务 %s 已交付（delivered）。编排器可用 forge task resume --ref %s 验收`, state.TaskRef, state.TaskRef)
	fmt.Println()
	return nil
}

func runTaskQuestion(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	content, _ := cmd.Flags().GetString(`content`)
	if content == `` {
		return fmt.Errorf(`--content 必填（回抛的问题内容）`)
	}
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		return s.Question(content)
	})
	if err != nil {
		return fmt.Errorf(`回抛失败: %w`, err)
	}
	fmt.Printf(`✓ 任务 %s 已回抛问题（input-required）。编排器用 forge task answer --ref %s 答复`, state.TaskRef, state.TaskRef)
	fmt.Println()
	return nil
}

func runTaskAnswer(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	content, _ := cmd.Flags().GetString(`content`)
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		return s.Answer(content)
	})
	if err != nil {
		return fmt.Errorf(`答复失败: %w`, err)
	}
	note := ``
	if content == `` {
		note = `（空答复，仅恢复 claimed 未记决策）`
	}
	fmt.Printf(`✓ 已答复任务 %s，回 claimed%s`, state.TaskRef, note)
	fmt.Println()
	return nil
}

func runTaskFail(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	reason, _ := cmd.Flags().GetString(`reason`)
	if reason == `` {
		return fmt.Errorf(`--reason 必填（失败原因）`)
	}
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		return s.Fail(reason)
	})
	if err != nil {
		return fmt.Errorf(`标记失败出错: %w`, err)
	}
	fmt.Printf(`✓ 任务 %s 已标记失败（failed）：%s`, state.TaskRef, reason)
	fmt.Println()
	return nil
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	reason, _ := cmd.Flags().GetString(`reason`)
	if reason == `` {
		return fmt.Errorf(`--reason 必填（撤回原因）`)
	}
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		return s.Cancel(reason)
	})
	if err != nil {
		return fmt.Errorf(`撤回失败: %w`, err)
	}
	fmt.Printf(`✓ 任务 %s 已撤回分派（canceled）：%s`, state.TaskRef, reason)
	fmt.Println()
	return nil
}

func runTaskReopen(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	reason, _ := cmd.Flags().GetString(`reason`)
	if reason == `` {
		return fmt.Errorf(`--reason 必填（重开原因，交付后发现的问题）`)
	}
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		return s.Reopen(reason)
	})
	if err != nil {
		return fmt.Errorf(`重开失败: %w`, err)
	}
	fmt.Printf(`✓ 任务 %s 已重开（回 claimed，交付后发现 bug）：%s`, state.TaskRef, reason)
	fmt.Println()
	return nil
}

// runTaskReclaim wires the §3 TTL recovery trigger: it scans for claimed tasks whose claimer has
// gone silent (IsClaimedStale — claimed >ClaimedZombieTTL with no checklog activity) and reclaims
// each to offered via Abandon(), resetting the claim clock so the SAME agent can re-claim it in a
// fresh session (the typical TTL scenario: the claimer crashed/restarted). Abandon preserves
// Assignment.Agent, and Claim requires a matching agent, so a reclaimed task is re-claimable only by
// its original agent — to reassign to a different agent use task cancel + task assign. This closes
// the phase-2 milestone "claimed 僵死能回收" — previously health only REPORTED these, never recovered
// them. Detection reuses IsClaimedStale (the same primitive task health uses), so the report and the
// reclamation can never disagree on what a "claimed zombie" is. Because detection scans at one
// instant and reclamation re-locks per task, staleness can drift in between: we re-run IsClaimedStale
// INSIDE the lock and skip (without abandoning) any candidate no longer stale, so a concurrent
// legitimate claim is never blown away. --dry-run lists candidates without mutating; --json emits
// {reclaimed, dry_run, count}.
//
// runTaskReclaim 接线 §3 的 TTL 回收触发：扫描认领方失联的 claimed 任务（IsClaimedStale——
// claimed 超 ClaimedZombieTTL 且无 checklog 活动），用 Abandon() 把每个回收为 offered，重置认领
// 时钟使「原认领 agent」能在新会话中重新认领（典型 TTL 场景：认领方崩溃/重启）。Abandon 保留
// Assignment.Agent，而 Claim 要求 agent 匹配，故回收后的任务只能被原 agent 重新认领——要改派给别的
// agent 用 task cancel + task assign。这补齐阶段2 里程碑「claimed 僵死能回收」——先前 health 只报告、
// 从不回收。检测复用 IsClaimedStale（与 task health 同一原语），故报告与回收对「claimed 僵尸」永不分歧。
// 因检测在某一瞬时扫描、回收按 task 重新加锁，时效可能漂移：在锁内复跑 IsClaimedStale，对不再过期的
// 候选「跳过不回收」，使并发中的合法认领永不被误回收。--dry-run 只列候选不改状态；
// --json 输出 {reclaimed, dry_run, count}。
func runTaskReclaim(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool(`dry-run`)
	asJSON, _ := cmd.Flags().GetBool(`json`)
	now := time.Now()

	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return fmt.Errorf(`读取任务列表失败: %w`, err)
	}
	// Detect candidates with the SAME primitive task health uses (IsClaimedStale), so the report
	// and the reclamation can never disagree on what "claimed zombie" means.
	//
	// 用与 task health 相同的原语（IsClaimedStale）检测候选，使报告与回收对「claimed 僵尸」永不分歧。
	var candidates []string
	for _, s := range states {
		if s == nil {
			continue
		}
		if ok, _ := taskpipeline.IsClaimedStale(root, s, now); ok {
			candidates = append(candidates, s.TaskRef)
		}
	}

	if dryRun {
		if asJSON {
			// nil slice marshals as `null`; normalize to `[]` (convention shared with task mine/health
			// JSON) so empty results are a stable empty array, not a missing field.
			//
			// nil slice 序列化为 `null`；归一化为 `[]`（与 task mine/health JSON 同约定），使空结果是
			// 稳定的空数组而非缺失字段。
			refs := candidates
			if refs == nil {
				refs = []string{}
			}
			out, _ := json.MarshalIndent(reclaimResult{Reclaimed: refs, DryRun: true, Count: len(refs)}, ``, `  `)
			fmt.Println(string(out))
			return nil
		}
		if len(candidates) == 0 {
			fmt.Println(`✓ 无 claimed 僵尸任务需回收（无 claimed>TTL）`)
			return nil
		}
		fmt.Printf(`将回收 %d 个 claimed 僵尸（干跑，未改状态）:`, len(candidates))
		fmt.Println()
		for _, ref := range candidates {
			fmt.Printf(`  %s`, ref)
			fmt.Println()
		}
		return nil
	}

	// Reclaim each candidate under its own lock. Detection (above) reads each state ONCE at scan
	// time, but reclamation re-acquires the lock per task, so the staleness verdict can drift in
	// between — e.g. the claimer writes fresh checklog activity after the scan but before the lock.
	// That window is the false-positive the TTL rule exists to prevent, so we RE-RUN IsClaimedStale
	// under the lock (on the freshly-loaded state) and skip — WITHOUT abandoning — any candidate no
	// longer stale. Abandon() separately guards status drift (claimed→other) via errAbandonNotClaimed.
	// Both drift cases are skip-not-fail, so a concurrent legitimate claim is never blown away. A
	// skip-not-mutate returns nil; MutateTaskState then saves the unchanged state (a harmless no-op).
	//
	// 每个候选在各自锁内回收。上方检测在扫描时对每个状态只读一次，回收时按 task 重新加锁，故两次之间
	// 时效判定可能漂移——例如认领方在扫描后、加锁前写入了新的 checklog 活动。该窗口正是 TTL 规则要防的
	// 假阳性，故在锁内（对新加载的状态）复跑 IsClaimedStale，对不再过期的候选「跳过不回收」。Abandon()
	// 另以 errAbandonNotClaimed 守护状态漂移（claimed→其他）。两类漂移都视作跳过不致命，使并发中的合法
	// 认领永不被误回收。跳过不改状态时返回 nil；MutateTaskState 随后保存不变的状态（无害 no-op）。
	var reclaimed []string
	for _, ref := range candidates {
		reclaimedThis := false
		err := taskpipeline.MutateTaskState(root, ref, func(s *taskpipeline.TaskState) error {
			if ok, _ := taskpipeline.IsClaimedStale(root, s, time.Now()); !ok {
				return nil // 不再过期（锁内有新活动）——不改状态，跳过
			}
			if err := s.Abandon(); err != nil {
				return err
			}
			reclaimedThis = true
			return nil
		})
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), `⚠ 跳过 %s: %v`, ref, err)
			fmt.Fprintln(cmd.ErrOrStderr())
			continue
		}
		if reclaimedThis {
			reclaimed = append(reclaimed, ref)
		}
	}

	if asJSON {
		// Normalize nil→[] (see the dry-run path above) so empty reclaims marshal as `[]` not `null`.
		//
		// 归一化 nil→[]（见上方 dry-run 路径），使空回收序列化为 `[]` 而非 `null`。
		refs := reclaimed
		if refs == nil {
			refs = []string{}
		}
		out, _ := json.MarshalIndent(reclaimResult{Reclaimed: refs, DryRun: false, Count: len(refs)}, ``, `  `)
		fmt.Println(string(out))
		return nil
	}
	if len(reclaimed) == 0 {
		if len(candidates) == 0 {
			fmt.Println(`✓ 无 claimed 僵尸任务需回收（无 claimed>TTL）`)
		} else {
			fmt.Printf(`✓ 候选 %d 个但回收时均已非 claimed（可能刚被认领/回收）`, len(candidates))
			fmt.Println()
		}
		return nil
	}
	fmt.Printf(`✓ 已回收 %d 个 claimed 僵尸 → offered:`, len(reclaimed))
	fmt.Println()
	for _, ref := range reclaimed {
		fmt.Printf(`  %s`, ref)
		fmt.Println()
	}
	return nil
}

// reclaimResult is the JSON shape of `forge task reclaim --json` output: the reclaimed refs (or
// would-be-reclaimed under --dry-run), the dry-run flag, and the count.
//
// reclaimResult 是 forge task reclaim --json 输出的 JSON 形状：回收的 ref（或 --dry-run 下将
// 回收的 ref）、dry-run 标志、数量。
type reclaimResult struct {
	Reclaimed []string `json:"reclaimed"`
	DryRun    bool     `json:"dry_run"`
	Count     int      `json:"count"`
}

// delegatedEntry is the JSON shape of one row in forge task mine output.
type delegatedEntry struct {
	Ref              string       `json:"ref"`
	Title            string       `json:"title"`
	Role             string       `json:"role,omitempty"`
	Status           string       `json:"status"`
	OfferedBy        string       `json:"offered_by,omitempty"`
	PendingDeps      []string     `json:"pending_deps,omitempty"`
	PendingDepDetail []pendingDep `json:"pending_dep_detail,omitempty"`
	IsZombie         bool         `json:"is_zombie,omitempty"`
	ZombieReasons    []string     `json:"zombie_reasons,omitempty"`
}

// pendingDep annotates one pending upstream dependency with its collaboration status and gate
// progress, so a worker sees WHERE it is blocked (design §4: "卡在 feat/backend[claimed, 进度 60%]").
// Status is the Assignment.Status for a delegated dep, or complete/incomplete/missing for an
// ordinary or absent predecessor. Gate progress is passed/total of DefaultGates.
//
// pendingDep 标注一条待交付上游依赖的协作状态与门禁进度，让工作方看清卡在哪一环
// （设计§4：「卡在 feat/backend[claimed, 进度 60%]」）。Status 对分派依赖取 Assignment.Status，
// 对普通/缺失前序取 complete/incomplete/missing。门禁进度为 DefaultGates 的 passed/total。
type pendingDep struct {
	Ref        string `json:"ref"`
	Status     string `json:"status"`
	GatePassed int    `json:"gate_passed"`
	GateTotal  int    `json:"gate_total"`
}

// annotateDep describes one pending upstream dependency: collaboration status + gate progress.
// A delegated dep → its Assignment.Status; an ordinary predecessor → complete/incomplete; a
// missing/aborted ref → "missing". Gate progress is passed/total over DefaultGates.
// Cross-repo deps (key:ref, multi-repo workspace Option B) resolve via
// taskpipeline.LoadDepState — mirroring task_health.go's lookupState — instead of
// reading as forever-missing: the foreign task can never be in this repo's byRef
// index, so without the resolution a live cross-repo dep would render "missing"
// and hide WHERE the worker is actually blocked.
//
// annotateDep 描述一条待交付上游依赖：协作状态 + 门禁进度。分派依赖 → 其 Assignment.Status；
// 普通前序 → complete/incomplete；缺失/已 abort 的 ref → "missing"。门禁进度为 DefaultGates 的 passed/total。
// 跨仓依赖（key:ref，多仓 workspace Option B）经 taskpipeline.LoadDepState 解析——镜像
// task_health.go 的 lookupState——而非恒读作 missing：他仓任务本就不可能在本仓 byRef 索引里，
// 不解析的话一个在途的跨仓依赖会渲染成 "missing"，掩盖工作方真正卡在哪一环。
func annotateDep(root string, ref string, byRef map[string]*taskpipeline.TaskState) (status string, passed, total int) {
	total = len(taskpipeline.DefaultGates())
	var s *taskpipeline.TaskState
	if s = byRef[ref]; s == nil {
		// Not in this repo's index: a cross-repo ref (key:ref) resolves from the
		// member repo's data dir; only a FAILED resolution (unknown key, missing/
		// unreadable state) is "missing" — same conservative shape as the gate's
		// PendingDependencies.
		//
		// 不在本仓索引：跨仓 ref（key:ref）从成员仓数据目录解析；只有解析失败
		// （key 未知、state 缺失/不可读）才是 "missing"——与门禁 PendingDependencies
		// 同样的保守形态。
		if key, _ := taskpipeline.SplitDepRef(ref); key != "" {
			cs, err := taskpipeline.LoadDepState(root, ref)
			if err != nil || cs == nil {
				return `missing`, 0, total
			}
			s = cs
		} else {
			return `missing`, 0, total
		}
	}
	passed = len(s.CompletedGates())
	if s.Assignment != nil {
		return s.Assignment.Status, passed, total
	}
	if s.IsComplete() {
		return `complete`, passed, total
	}
	return `incomplete`, passed, total
}

// checkNameAssignmentUnclaimed is the checklog name for the task-implement assignment
// advisory: the gated task is assigned to a DIFFERENT agent and still unclaimed — the worker
// is advancing gates on a delegation that was never claimed (the 2026-08-18 脱节 precursor:
// the pipeline moves while the assignment state machine does not). Advisory only, never
// blocks (an orchestrator running gates on behalf of the assignee is legitimate).
//
// checkNameAssignmentUnclaimed 是 task-implement 分派 advisory 的 checklog 名：过门禁的
// 任务分派给了另一个 agent 且尚未 claimed——执行方在从未认领的分派上推进门禁（2026-08-18
// 脱节的前兆：管线在走而分派状态机不动）。仅 advisory，永不阻断（编排器代跑是合法场景）。
const checkNameAssignmentUnclaimed checklog.CheckName = "assignment-unclaimed"

// adviseUnclaimedAssignment emits the assignment advisory at the task-implement gate (P2 of
// the 2026-08-18 脱节修复): when the gated task is offered to an agent other than the current
// one and has not been claimed, warn on stderr and record a checklog trail. Skipped silently
// when the current agent cannot be detected (a false positive is worse than a missed hint) or
// when the assignee themselves is gating. Advisory only — never changes the gate outcome.
//
// adviseUnclaimedAssignment 在 task-implement 门禁发出分派 advisory（2026-08-18 脱节修复
// 的 P2）：过门禁的任务 offered 给非当前 agent 且尚未 claimed 时，stderr 提醒并落 checklog
// 痕迹。当前 agent 探测不到（误报比漏报更糟）或受派方本人过门禁时静默跳过。仅
// advisory——绝不改变门禁结果。
func adviseUnclaimedAssignment(root, gateID string, passed bool, state *taskpipeline.TaskState) {
	if gateID != `task-implement` || !passed || state == nil || state.Assignment == nil {
		return
	}
	if state.Assignment.Status != taskpipeline.AssignOffered {
		return
	}
	current := resolveOriginTool(root, ``)
	if current == `` || current == state.Assignment.Agent {
		return // 探测不到当前 agent，或正是受派方本人——不打扰
	}
	detail := fmt.Sprintf(`ADVISORY: 任务 %s 分派给 %s 且尚未 claimed，当前由 %s 推进 task-implement（编排器代跑合法；若是执行方接手，请先 forge task claim）`,
		state.TaskRef, state.Assignment.Agent, current)
	fmt.Fprintf(os.Stderr, "⚠️ [assignment] %s\n", detail)
	if err := checklog.Record(root, &checklog.Entry{
		Check:   checkNameAssignmentUnclaimed,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelAdvisory,
		TaskRef: state.TaskRef,
		Detail:  detail,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to record assignment advisory: %v\n", err)
	}
}

// scanDelegations collects the delegated tasks matching agent (and optional role/blocked filters)
// under one project root. It builds a ref→state index so each pending dep can be annotated with its
// status + gate progress. Returns (nil, err) only when ListTaskStates fails; an empty slice means
// the agent has no matching delegations under this root. Shared by the single-project and
// --all-projects paths so the two views never disagree on what matches.
//
// scanDelegations 收集某 project root 下匹配 agent（及可选 role/blocked 过滤）的分派任务。建 ref→state
// 索引使每条待交依赖可标注其状态 + 门禁进度。仅 ListTaskStates 失败返 (nil, err)；空切片表示该 root 下
// agent 无匹配分派。单 project 与 --all-projects 共用，使两种视图对「匹配」永不分歧。
func scanDelegations(root, agent, role string, blocked bool, now time.Time) ([]delegatedEntry, error) {
	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return nil, err
	}
	// Index states by ref so each pending dep can be annotated with its status + gate progress
	// without a second ListTaskStates pass.
	//
	// 按 ref 索引 states，使每条待交依赖无需二次 ListTaskStates 即可标注其状态 + 门禁进度。
	byRef := make(map[string]*taskpipeline.TaskState, len(states))
	for _, s := range states {
		if s != nil {
			byRef[s.TaskRef] = s
		}
	}
	var entries []delegatedEntry
	for _, s := range states {
		if s == nil || s.Assignment == nil || s.Assignment.Agent != agent {
			continue
		}
		if role != `` && s.Assignment.Role != role {
			continue
		}
		// Pending upstream deps (design phase 3): a task whose DependsOn is not fully delivered is
		// blocked at verify/complete. Computed via the same PendingDependencies the gate uses, so mine
		// and the gate can never disagree on what "blocked" means. --blocked filters to only these.
		//
		// 未交付的上游依赖（设计阶段3）：DependsOn 未全交付的 task 在 verify/complete 被阻。用与门禁相同的
		// PendingDependencies 计算，使 mine 与门禁对「阻塞」永远一致。--blocked 只留这些。
		var pend []string
		if len(s.DependsOn) > 0 {
			pend = taskpipeline.PendingDependencies(root, s.DependsOn)
		}
		if blocked && len(pend) == 0 {
			continue
		}
		// Annotate each pending dep with WHERE it is stuck (design §4): the dep's collaboration
		// status + gate progress, so the worker knows not just that it is blocked but on whom/what.
		//
		// 标注每条待交依赖卡在哪一环（设计§4）：依赖的协作状态 + 门禁进度，
		// 让工作方不只知被阻塞，更知被谁/什么阻塞。
		var details []pendingDep
		for _, d := range pend {
			st, passed, tot := annotateDep(root, d, byRef)
			details = append(details, pendingDep{Ref: d, Status: st, GatePassed: passed, GateTotal: tot})
		}
		isZombie, zombieReasons := taskpipeline.IsZombie(root, s, now)
		// Reconcile the rendered status with the task pipeline (2026-08-18 脱节修复): a task whose
		// gates all passed but whose assignment was never claimed/delivered (e.g. legacy states
		// pre-dating MarkComplete's auto-reclaim) must NOT render as 待认领 — that would present a
		// finished task as pending forever. Render `complete` instead (JSON status field carries the
		// same value, so dashboards get one consistent signal); the row is kept, not filtered, to
		// preserve visibility of the completed fact. Terminal assignment states pass through as-is.
		//
		// 渲染状态与任务管线 reconcile（2026-08-18 脱节修复）：门禁全过但分派从未 claim/deliver
		// 的任务（如 MarkComplete 自动回收之前的存量状态）不得渲染为待认领——那会把已完成任务
		// 永久显示成待办。改渲染 `complete`（JSON status 字段同值，看板等消费者读到一致信号）；
		// 行保留不过滤，以可见方式呈现已完成事实。分派终态原样透传；被 Reopen 打回返工的任务
		// （IsReopened）也透传真实协作状态——卡住的返工不得伪装成 complete。
		status := s.Assignment.Status
		if s.IsComplete() && !s.IsReopened() {
			switch s.Assignment.Status {
			case taskpipeline.AssignOffered, taskpipeline.AssignClaimed, taskpipeline.AssignInputRequired:
				status = `complete`
			}
		}
		entries = append(entries, delegatedEntry{
			Ref:              s.TaskRef,
			Title:            s.Summary,
			Role:             s.Assignment.Role,
			Status:           status,
			OfferedBy:        s.Assignment.OfferedBy,
			PendingDeps:      pend,
			PendingDepDetail: details,
			IsZombie:         isZombie,
			ZombieReasons:    zombieReasons,
		})
	}
	return entries, nil
}

// formatEntry renders one delegatedEntry as an indented single-line row (status, ref, role,
// offerer, title, blocking-dep annotations, zombie marker). Shared by the single-project and
// --all-projects outputs so the two views never drift in row format.
//
// formatEntry 把一条 delegatedEntry 渲染为缩进单行（状态/ref/角色/分派方/标题/阻塞依赖标注/僵尸标记）。
// 单 project 与 --all-projects 共用，使两种视图行格式永不漂移。
func formatEntry(e delegatedEntry) string {
	roleStr := e.Role
	if roleStr == `` {
		roleStr = `-`
	}
	s := fmt.Sprintf(`  %s  [%s]  角色=%s  分派方=%s  %s`, e.Status, e.Ref, roleStr, e.OfferedBy, e.Title)
	if len(e.PendingDepDetail) > 0 {
		parts := make([]string, 0, len(e.PendingDepDetail))
		for _, d := range e.PendingDepDetail {
			parts = append(parts, fmt.Sprintf(`%s[%s,%d/%d gate]`, d.Ref, d.Status, d.GatePassed, d.GateTotal))
		}
		s += fmt.Sprintf(`  ⏳阻塞于: %s`, strings.Join(parts, `, `))
	}
	// Zombie annotation (design §12 标黄): a row whose delegation has stalled (offered>7d /
	// claimed>TTL / input-required>7d / abandoned_count≥2) gets a ⚠ marker with its reasons.
	// Human-color terminals can't reliably render ANSI here, so the marker + reasons are the
	// signal; the dashboard renders true yellow.
	//
	// 僵尸标注（设计 §12 标黄）：分派停滞的行（offered>7d / claimed>TTL /
	// input-required>7d / abandoned_count≥2）挂 ⚠ 标记并附 reason。终端难可靠渲染 ANSI，
	// 故标记 + reason 即信号；真黄色在看板渲染。
	if e.IsZombie {
		s += fmt.Sprintf(`  ⚠僵尸(%s)`, strings.Join(e.ZombieReasons, `,`))
	}
	return s
}

func runTaskMine(cmd *cobra.Command, args []string) error {
	agent, _ := cmd.Flags().GetString(`agent`)
	allProjects, _ := cmd.Flags().GetBool(`all-projects`)
	if agent == `` {
		agent = detectOriginTool(``)
		// Pointer fallback (single-project mode only): a worker driving forge from
		// a kimi/codex/... Bash tool has no identity env — the last-session pointer
		// attributes it. all-projects mode spans roots, so no single pointer applies.
		//
		// 指针回落（仅单项目模式）：从 kimi/codex/... 的 Bash 工具驱动 forge 的
		// worker 没有身份 env——last-session 指针补上归属。all-projects 模式跨
		// root，无单一指针可用。
		if agent == `` && !allProjects {
			if root, err := findProjectRoot(); err == nil {
				agent = resolveOriginTool(root, ``)
			}
		}
	}
	if agent == `` {
		return fmt.Errorf(`无法探测当前 agent（无 agent env）。显式传 --agent <agent>（如 kimi/reasonix/cursor）查看分派给该 agent 的任务`)
	}
	role, _ := cmd.Flags().GetString(`role`)
	blocked, _ := cmd.Flags().GetBool(`blocked`)
	asJSON, _ := cmd.Flags().GetBool(`json`)
	// now is captured once for the zombie scan (offered>7d / claimed>TTL / input-required>7d all
	// age against the same instant). Reuses taskpipeline.IsZombie so mine, the dashboard, and
	// `task health` share ONE truth about what "zombie" means (design §12).
	//
	// now 只取一次供僵尸扫描（offered>7d / claimed>TTL / input-required>7d 都相对同一时刻老化）。
	// 复用 taskpipeline.IsZombie，使 mine、看板、task health 对「僵尸」共享同一真相源（设计 §12）。
	now := time.Now()

	if allProjects {
		return runTaskMineAllProjects(agent, role, blocked, now, asJSON)
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	entries, err := scanDelegations(root, agent, role, blocked, now)
	if err != nil {
		return fmt.Errorf(`读取任务列表失败: %w`, err)
	}
	return printDelegations(entries, agent, role, asJSON)
}

// printDelegations renders the single-project view: a JSON array, or the human header + one row
// per entry. Empty → a "no delegations" line.
//
// printDelegations 渲染单 project 视图：JSON 数组，或人类可读表头 + 每条一行。空 → 「无分派」行。
func printDelegations(entries []delegatedEntry, agent, role string, asJSON bool) error {
	if asJSON {
		if entries == nil {
			entries = []delegatedEntry{}
		}
		out, _ := json.MarshalIndent(entries, ``, `  `)
		fmt.Println(string(out))
		return nil
	}
	if len(entries) == 0 {
		fmt.Printf(`没有分派给 %s 的任务`, agent)
		if role != `` {
			fmt.Printf(`（角色 %s）`, role)
		}
		fmt.Println()
		return nil
	}
	fmt.Printf(`分派给 %s 的任务（%d）:`, agent, len(entries))
	fmt.Println()
	for _, e := range entries {
		fmt.Println(formatEntry(e))
	}
	return nil
}

// runTaskMineAllProjects renders the cross-project view (design §8): scan every registered project
// for delegations to agent, group by project with a project-key label, and give overview counts +
// per-project rows. Never auto-resumes (unlike the SessionStart hook — mine is read-only discovery).
// A failed project is warned on stderr and skipped so one broken root does not blind the global view.
//
// runTaskMineAllProjects 渲染跨 project 视图（设计§8）：扫描每个已登记 project 分派给 agent 的任务，
// 按 project 分组并标 project-key，给概览计数 + 每 project 明细。绝不自动 resume（区别于 SessionStart
// hook——mine 是只读发现）。失败的 project 在 stderr 警告并跳过，使一个坏 root 不致盲全局视图。
func runTaskMineAllProjects(agent, role string, blocked bool, now time.Time, asJSON bool) error {
	roots := registry.List()
	if len(roots) == 0 {
		return fmt.Errorf(`全局视图无已登记项目——在项目目录跑 forge init 登记后重试`)
	}
	type projectGroup struct {
		Project string           `json:"project"`
		Root    string           `json:"root"`
		Count   int              `json:"count"`
		Entries []delegatedEntry `json:"entries"`
	}
	var groups []projectGroup
	for _, r := range roots {
		entries, err := scanDelegations(r, agent, role, blocked, now)
		if err != nil {
			fmt.Fprintf(os.Stderr, `Warning: 扫描 project %s 失败（跳过）: %v`+"\n", r, err)
			continue
		}
		// M2: forgedata.Key 失败（如注册后 .git 被移除、worktree 清理）时 key 为空——text 分支会
		// 回退 filepath.Base，但 JSON 的 project 字段会拿到空串无法区分「无 key」与「key 为空」。这里
		// 统一回退 base，使 JSON 与 text 一致、消费者不收空值。
		key, keyErr := forgedata.Key(r)
		if keyErr != nil {
			key = filepath.Base(r)
		}
		if entries == nil {
			entries = []delegatedEntry{}
		}
		groups = append(groups, projectGroup{Project: key, Root: r, Count: len(entries), Entries: entries})
	}
	if asJSON {
		if groups == nil {
			groups = []projectGroup{}
		}
		out, _ := json.MarshalIndent(map[string]any{`projects`: groups}, ``, `  `)
		fmt.Println(string(out))
		return nil
	}
	total := 0
	for _, g := range groups {
		total += g.Count
	}
	fmt.Printf(`全局分派给 %s 的任务（%d 个 project，共 %d）:`, agent, len(groups), total)
	fmt.Println()
	for _, g := range groups {
		label := g.Project
		if label == `` {
			label = filepath.Base(g.Root)
		}
		fmt.Printf(`▶ project %s: %d 个`, label, g.Count)
		fmt.Println()
		for _, e := range g.Entries {
			fmt.Println(formatEntry(e))
		}
	}
	return nil
}
