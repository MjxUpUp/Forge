package taskpipeline

import (
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// TaskGate defines a lightweight task-level quality gate.
//
// TaskGate 定义轻量 task 级 quality gate。
type TaskGate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Auto        bool   `json:"auto"` // true = 由 hook 自动检查
}

// AcceptanceCriterion is an executable acceptance criterion (from the dev-workflow Plan's
// Run: <cmd>, Expected: <output>). Persisted into TaskState so the criterion survives beyond the plan text;
// verify-acceptance actually runs Run, compares against Expected, and records deterministic evidence — turning the spec
// into an unforgeable verification that counters the blind spot of an agent self-reporting acceptance satisfied.
//
// AcceptanceCriterion 是一条可执行的验收标准（来自 dev-workflow Plan 的
// "Run: <cmd>, Expected: <output>"）。持久化进 TaskState，使验收标准不随 plan 文本
// 消失；verify-acceptance 实跑 Run、比对 Expected，记 deterministic 证据——把 spec
// 变成不可伪造的验证，对冲 agent 自述「满足验收」的盲区。
type AcceptanceCriterion struct {
	Run      string `json:"run"`                // 实跑的命令（如 "go test ./..."）
	Expected string `json:"expected,omitempty"` // 期望输出的子串；空=只看退出码 0
	Passed   bool   `json:"passed,omitempty"`   // 上次 verify-acceptance 的结果
	Output   string `json:"output,omitempty"`   // 上次实跑的输出（截断），供排查
	// AcceptedHeadCommit is the HEAD snapshot when VerifyAcceptance actually ran this criterion (state.go GetHeadCommit).
	// forge_task_proof compares it == current HEAD to decide whether Passed is fresh — preventing an agent from reading a stale
	// Passed based on old code and declaring done. Empty = never verified (old-state compatible); proof falls back to the v1 rerun path.
	//
	// AcceptedHeadCommit 是 VerifyAcceptance 实跑该条时的 HEAD 快照（state.go GetHeadCommit）。
	// forge_task_proof 比对 == 当前 HEAD 判定 Passed 是否 fresh——避免 agent 读基于旧代码的过时
	// Passed 声明 done。空 = 未跑过 verify（老 state 兼容），proof 走 v1 重跑兜底。
	AcceptedHeadCommit string `json:"accepted_head_commit,omitempty"`
}

// ExternalOrigin is the external work source of a task (an issue-tracker issue). forge_task_start --from_issue
// parses the URL and fills it in, extending a task's origin from the branch to an external issue — the key to two-layer decoupling:
// when a spawn-style orchestrator (Symphony-like) launches an agent run, the task is natively associated with the external issue, not inferred from the branch.
//
// ExternalOrigin 是 task 的外部工作来源（issue tracker issue）。forge_task_start --from_issue
// 解析 URL 填充，把 task 的 origin 从 branch 扩展到外部 issue——两层解耦的关键：spawn 式编排器
// （Symphony 类）拉起 agent run 时，task 天然关联到外部 issue，不靠 branch 推断。
type ExternalOrigin struct {
	Tracker    string `json:"tracker,omitempty"`    // linear | github | ""（URL 解析推断）
	IssueID    string `json:"issue_id,omitempty"`   // tracker 内部稳定 ID（若可解析）
	Identifier string `json:"identifier,omitempty"` // 人类可读 key（ABC-123 / org/repo#123）
	URL        string `json:"url,omitempty"`
}

// Decision is a confirmed technical/product decision (corresponding to the Decisions section of the
// cross-tool-context AI_CONTEXT.md, promoted to a structured field). Persisted into TaskState so the decision survives
// session compaction and is visible cross-tool — any successor resuming knows what has already been decided and should not be relitigated.
//
// Decision 是一条已确认的技术/产品决策（对应 cross-tool-context AI_CONTEXT.md 的
// Decisions 节，升格为结构化字段）。持久化进 TaskState，使决策不随会话压缩丢失、跨工具
// 可见——任何接手方 resume 即知「已经决定了什么、不要再推翻」。
type Decision struct {
	ID        string    `json:"id"`      // 稳定标识，供 resolve/引用
	Content   string    `json:"content"` // 决策内容
	DecidedAt time.Time `json:"decided_at"`
	By        string    `json:"by,omitempty"`        // 确认方（工具/人，如 [pi]/[claude-code]/人）
	Affects   []string  `json:"affects,omitempty"`   // 影响的文件/模块
	Rationale string    `json:"rationale,omitempty"` // 为什么这么决定（HANDOFF 纪律：写"为什么"不只写"是什么"）
}

// Blocker is an impediment (corresponding to the known-issues/blockers of HANDOFF). Status drives the workflow:
// open → resolved (fixed) / wontfix (abandoned).
//
// Blocker 是一项阻塞（对应 HANDOFF 的「已知问题/阻塞」）。Status 驱动工作流：
// open → resolved（已解决）/ wontfix（放弃解决）。
type Blocker struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	RaisedAt   time.Time `json:"raised_at"`
	Status     string    `json:"status"`               // open | resolved | wontfix
	Resolution string    `json:"resolution,omitempty"` // 解决方式（resolved/wontfix 时填）
	By         string    `json:"by,omitempty"`
}

// Finding is a problem/risk discovered by some tool (corresponding to the Findings section of AI_CONTEXT.md). It carries a Source
// tool so that who-found-it is visible in cross-tool collaboration — avoiding duplicate discoveries and easing evidence tracing.
//
// Finding 是某工具发现的问题/风险（对应 AI_CONTEXT.md 的 Findings 节）。带 Source 来源
// 工具，让跨工具协作时「谁发现的」可见——避免重复发现、便于回溯证据。
type Finding struct {
	ID       string    `json:"id"`
	Content  string    `json:"content"`
	Source   string    `json:"source"`             // 来源工具 [pi]/[claude-code]/[opencode]…
	Evidence string    `json:"evidence,omitempty"` // 证据（文件:行 / 命令输出）
	Severity string    `json:"severity,omitempty"` // "" | critical | important | minor——doc-review 的 critical 未决会阻断 doc gate；空（旧 findings）永不阻断
	Status   string    `json:"status"`             // open | fixed | wontfix
	RaisedAt time.Time `json:"raised_at"`
}

// Artifact is a reference to a task-related artifact (file / command output / url / doc). Indexed but not gated —
// letting a successor know what this task produced and which key files it changed.
//
// Artifact 是任务的相关产物引用（文件/命令输出/url/文档）。仅索引不门禁——让接手方知道
// 「这个任务产出了什么、改了哪些关键文件」。
type Artifact struct {
	Path string `json:"path"` // 文件路径 / url
	Kind string `json:"kind"` // file | cmd-output | url | doc
	Note string `json:"note,omitempty"`
}

// Assignment status values (Assignment.Status). A compressed A2A Task lifecycle: the full A2A set is
// submitted/working/input-required/completed/failed/canceled; Forge collapses submitted→offered,
// working→claimed, completed→delivered, keeping input-required/failed/canceled intact because they
// carry distinct handoff semantics (worker回抛 / 做失败 / 派发撤回).
//
// 分派状态值（Assignment.Status）。压缩版 A2A Task lifecycle：完整 A2A 是
// submitted/working/input-required/completed/failed/canceled；Forge 把 submitted→offered、
// working→claimed、completed→delivered，保留 input-required/failed/canceled 因其承载不同接续语义。
const (
	AssignOffered       = `offered`
	AssignClaimed       = `claimed`
	AssignInputRequired = `input-required`
	AssignDelivered     = `delivered`
	AssignFailed        = `failed`
	AssignCanceled      = `canceled`
)

// Assignment carries a task's delegation to one agent plus its full collaboration lifecycle
// (a Forge-simplified A2A Task lifecycle). Single-valued pointer enforces "one task, one owner" —
// multi-agent collaboration is split into multiple tasks (each pointing at the same orchestrator task
// via ParentTaskRef), not one task with multiple assignees. nil = an ordinary non-delegated task,
// fully backward-compatible (zero behavior change).
//
// Assignment 承载任务向某 agent 的分派 + 完整协作生命周期（A2A Task lifecycle 的 Forge 简化版）。
// 单值指针强制「一任务一 owner」——多 agent 协作拆成多个 task（靠 ParentTaskRef 指同一编排任务），
// 而非一个 task 多 assignee。nil = 普通未分派任务，完全向后兼容（零行为变化）。
//
// Status is the collaboration dimension (who works on it, which handoff phase); task gates are the
// quality dimension (implement/verify/complete). delivered ≠ complete — a delivered task whose gates
// are not all passed is a legitimate intermediate state. This layering decouples handoff from QA.
//
// Status 是协作维度（谁在做、协作到哪阶段）；task gate 是质量维度（implement/verify/complete）。
// delivered ≠ complete——交付了但门禁未全过是合法中间态。此分层让「交付」与「质量验收」解耦。
type Assignment struct {
	Agent          string     `json:"agent"`                // ∈ agentsignals 已知集（写入校验，未知即拒）
	Role           string     `json:"role,omitempty"`       // frontend/backend/orchestrator（可选）
	Status         string     `json:"status"`               // Assign* 枚举之一
	OfferedBy      string     `json:"offered_by,omitempty"` // 派发的编排器 agent
	OfferedAt      *time.Time `json:"offered_at,omitempty"` // 派发时间（TTL 基准；*time.Time 因 encoding/json 的 omitempty 对 time.Time 不生效）
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	QuestionAt     *time.Time `json:"question_at,omitempty"` // 进入 input-required 的时刻；IsInputReqStale 基线之一——claim→立即回抛时与 ClaimedAt 等价（canonical 卡问题仍标僵尸），claim 久后近期才回抛则不再误判
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	LastQuestion   string     `json:"last_question,omitempty"`   // input-required 时的回抛内容
	FailReason     string     `json:"fail_reason,omitempty"`     // failed 时的原因；Reopen() 也复用此字段记「交付后重开理由」——双用途但语义不冲突：Fail 是终态、禁止 Reopen，故同一字段在不同状态分支下不会被两种含义同时读到
	CancelReason   string     `json:"cancel_reason,omitempty"`   // canceled 时的原因
	NotifiedAt     *time.Time `json:"notified_at,omitempty"`     // 上次 hook 推送时间（去重防轰炸）
	AbandonedCount int        `json:"abandoned_count,omitempty"` // claimed 超 TTL 回收次数（僵尸信号）
	AbandonedAt    *time.Time `json:"abandoned_at,omitempty"`    // 最近一次回收时间
	// AutoDelivered marks that the delivered terminal was set by MarkComplete's auto-reconcile,
	// not by a human `forge task deliver` — the audit trail distinguishing "task pipeline finished,
	// assignment reclaimed by the state machine" from a deliberate hand-deliver. Cleared by Reopen
	// (mirrors DeliveredAt being cleared) so a reopened→redelivered cycle re-records truthfully.
	//
	// AutoDelivered 标记 delivered 终态由 MarkComplete 的自动回收设置，而非人工 forge task
	// deliver——区分「管线完成、状态机回收分派」与「刻意手动交付」的审计痕迹。Reopen 时清零
	// （镜像 DeliveredAt 的清零），使重开→再交付的循环如实重记。
	AutoDelivered bool `json:"auto_delivered,omitempty"`
}

// Assignment state-transition errors. Sentinel values (not fmt.Errorf inline) so callers can match
// specific failures (e.g. mine silently skips errClaimNotOffered after a TOCTOU race).
//
// 分派状态转换错误。用哨兵值（非 fmt.Errorf 内联），使调用方能精确匹配（如 mine 在 TOCTOU 竞态后
// 静默跳过 errClaimNotOffered）。
var (
	errAssignmentEmptyAgent = errors.New(`assignment: assignee agent must not be empty`)
	errAssignmentExists     = errors.New(`assignment: task already has an assignment (use reassign to change owner)`)
	errNoAssignment         = errors.New(`assignment: task has no assignment`)
	errClaimWrongAgent      = errors.New(`assignment: claim agent does not match the offered assignee`)
	errClaimNotOffered      = errors.New(`assignment: can only claim an offered task`)
	errDeliverNotClaimed    = errors.New(`assignment: can only deliver a claimed task`)
	errQuestionNotClaimed   = errors.New(`assignment: can only raise a question on a claimed task`)
	errAnswerNotInputReq    = errors.New(`assignment: can only answer a task awaiting input (input-required)`)
	errFailNotClaimed       = errors.New(`assignment: can only fail a claimed task`)
	errCancelTerminal       = errors.New(`assignment: can only cancel a non-terminal task (offered/claimed/input-required)`)
	errReopenNotDelivered   = errors.New(`assignment: can only reopen a delivered task`)
	errAbandonNotClaimed    = errors.New(`assignment: can only abandon a claimed task`)
)

// SessionLink is the anchoring of a task to an agent session (one item of multi-way anchoring). A task records only the
// creator session by default; a successor (cross-session/cross-tool) appends via forge task attach, forming a two-way anchoring
// where N sessions jointly advance one task — any successor resuming knows who participated and with which tool.
//
// SessionLink 是 task 与一个 agent session 的锚定（多向锚定的一项）。task 默认只记创建方
// session；接手方（跨会话/跨工具）通过 forge task attach 追加，形成 N 个 session 共同推进
// 一个 task 的双向锚定——任意接手方 resume 即知「谁参与过、用什么工具」。
type SessionLink struct {
	SessionID string    `json:"session_id"`
	Tool      string    `json:"tool,omitempty"` // 该 session 所属工具（pi/claude-code/opencode…）
	JoinedAt  time.Time `json:"joined_at"`
	// Imported 标记本链接是跨机器 task import 带入的「幽灵 session」——它记录的是源机器上谁参与过本
	// task，仅用于溯源/看板显示（SessionTools 仍计入它），但不代表本机 session 已锚定。故 attach 路径
	// （HasSession/AddSession）忽略 Imported 链接：本机 session 的锚定永远独立于幽灵记录，不会被它
	// 误判为「已锚定」而跳过，也不会与它去重而吞掉本机链接。
	//
	// Imported marks this link as a "ghost session" carried in by a cross-machine task import — it records
	// who participated on the SOURCE machine and is for provenance/dashboard display only (SessionTools
	// still counts it), NOT a signal that the local session is anchored. So the attach path (HasSession/
	// AddSession) ignores Imported links: a local session's anchoring is always independent of the ghost
	// record, never short-circuited as "already attached" nor deduped away by it.
	Imported bool `json:"imported,omitempty"`
}

// TaskState tracks the state of a single task pipeline.
// Stored in DataDir/tasks/{sanitized-ref}.json.
//
// TaskState 追踪单个 task pipeline 的状态。
// 存于 DataDir/tasks/{sanitized-ref}.json。
type TaskState struct {
	TaskRef        string                    `json:"task_ref"`
	Branch         string                    `json:"branch"`
	Source         string                    `json:"source"` // explicit | branch
	Summary        string                    `json:"summary"`
	CurrentGate    string                    `json:"current_gate"`
	History        []TaskGateResult          `json:"history"`
	StartedAt      time.Time                 `json:"started_at"`
	CompletedAt    *time.Time                `json:"completed_at,omitempty"`
	Score          *scoringtypes.ScoreResult `json:"score,omitempty"`
	HeadCommit     string                    `json:"head_commit,omitempty"`     // 用于 duplicate detection
	SessionID      string                    `json:"session_id,omitempty"`      // 创建本 task 的 agent session
	ExternalOrigin ExternalOrigin            `json:"external_origin,omitempty"` // 外部 issue 来源（--from_issue 解析）；空=本地 branch 推断 origin
	ReviewPassed   bool                      `json:"review_passed,omitempty"`   // code-review-gate 通过标记；task-complete 门禁的硬前置
	ResumeStale    bool                      `json:"resume_stale,omitempty"`    // legacy 的 task-scoped「刚压缩过」标志：仅无 session ID 的回落路径与旧版 binary 写入；有 session ID 的 host 一律用 per-session sentinel（.resume-stale-<sid>，见 state.go），reinject 读到本字段会兑现一次并清零。task-scoped 的固有边界：两 session 共享同一 task 时，B 的 prompt 可能在 A 压缩后先消费并清掉标志（最坏漏注一次，handoff 内容相同故无数据损坏）——per-session sentinel 已对新路径消除该边界。
	// ReviewedHeadCommit/ReviewedChangeHash bind the code snapshot at review-pass time — the key to the review-fix-recheck loop.
	// At review pass, (HEAD, SourceChangesSince(HEAD)) is recorded; the task-complete gate recomputes SourceChangesSince(ReviewedHeadCommit)
	// and compares it to ReviewedChangeHash — a mismatch means code changed after review, forcing a re-review (no longer relying on agent self-discipline). See executor.go.
	// The commit-then-review flow (the E2E real sequence: commit first, then review, with a clean working tree at review time) → ReviewedChangeHash is empty,
	// so the baseline-set judgment uses ReviewedHeadCommit != "", not hash empty/non-empty.
	//
	// ReviewedHeadCommit/ReviewedChangeHash 绑定 review pass 时的代码快照——审查-修复-复审闭环的关键。
	// review pass 时记 (HEAD, SourceChangesSince(HEAD))；task-complete 门禁重算 SourceChangesSince(ReviewedHeadCommit)
	// 比对 ReviewedChangeHash，不一致说明审查后改了码，强制复审（不再靠 agent 自律重审）。详见 executor.go。
	// commit-then-review 流（E2E 真实序列：先 commit 再 review，审查时工作区干净）→ ReviewedChangeHash 为空，
	// 故「基线已设」判据用 ReviewedHeadCommit != ""，不能用 hash 空/非空。
	ReviewedHeadCommit string `json:"reviewed_head_commit,omitempty"`
	ReviewedChangeHash string `json:"reviewed_change_hash,omitempty"`

	// ReviewRounds is the append-only history of every review pass (one entry per `forge review pass`),
	// making the review-rework loop measurable: len(ReviewRounds) = review pass count, and together with
	// the failed task-complete entries in History it reconstructs how many rework rounds a task went
	// through. The latest entry duplicates ReviewedHeadCommit/ReviewedChangeHash (those two fields stay
	// the snapshot-check source of truth; this list only adds history, zero behavior change to the gate).
	//
	// ReviewRounds 是每次 review pass 的只追加历史（每次 `forge review pass` 一条），
	// 让审查-返工循环可度量：len(ReviewRounds) = review pass 次数，配合 History 里
	// task-complete 失败条目可还原任务经历了几轮返工。最后一条与
	// ReviewedHeadCommit/ReviewedChangeHash 重复（那两字段仍是快照校验的真相源；
	// 本列表只加历史，对门禁零行为变化）。
	ReviewRounds []ReviewRound `json:"review_rounds,omitempty"`

	// DesignPhases is the design phase inferred by inferDesignPhases at the task-verify gate.
	// Filled by the task-verify gate (executor.go ExecuteTaskGate) calling inferDesignPhases(taskChangedFiles)
	// and persisted via SaveTaskState, with zero friction: no user declaration required. The review sub-agent loads the corresponding
	// references/phase-X.md checklist based on it. Empty = no matching design artifact, falling back to the generic review-checklist.md.
	//
	// DesignPhases 是 inferDesignPhases 在 task-verify gate 推断出的设计阶段。
	// 由 task-verify gate（executor.go ExecuteTaskGate）调 inferDesignPhases(taskChangedFiles)
	// 填充并 SaveTaskState 持久化，零摩擦：不要求用户声明。review 子 agent 据此加载对应
	// references/phase-X.md checklist。空 = 无匹配设计产物，回落到通用 review-checklist.md。
	DesignPhases []DesignPhase         `json:"design_phases,omitempty"`
	Acceptance   []AcceptanceCriterion `json:"acceptance,omitempty"` // 验收标准（dev-workflow Plan 的 Run+Expected），verify-acceptance 实跑回扣
	// AcceptanceForeign marks that the acceptance Run commands entered this TaskState from an
	// untrusted source (task import bundle / .forge migrate of repo-committed state) — never typed
	// by a local user flag. Run commands are executable strings; executing foreign-authored ones
	// is arbitrary command execution, so verify-acceptance refuses to run them until the user has
	// reviewed the command list and explicitly trusted them (--trust-foreign); the first trusted
	// run clears this flag (re-runs are then local-verified evidence).
	//
	// AcceptanceForeign 标记验收 Run 命令来自不可信源（task import bundle / repo 提交状态经
	// .forge migrate 提升）——绝非本机用户 flag 亲手输入。Run 命令是可执行字符串，执行外来
	// 作者的命令即任意命令执行，故 verify-acceptance 拒绝直接跑：须用户审阅命令清单并显式
	// 受信（--trust-foreign）后才执行；首次受信运行会清掉本标记（之后的重跑即本机验证证据）。
	AcceptanceForeign bool `json:"acceptance_foreign,omitempty"`
	// PlanScope is the planned-changed-files allowlist declared before a task starts (globs, repo-relative forward-slash paths).
	// It corresponds to the planning precondition of which files you intend to change — turning it into a measurable contract. Advisory:
	// the diff between actually changed files (TaskChangedFiles) and it is recorded as scope-drift for review, non-blocking. Change-impact-analysis
	// recall is only ~44%, so scope is treated as a prediction rather than a contract, and drift is a routine signal. Declared via task start --scope, iteratively appended mid-task via task scope add (layered positioning).
	//
	// PlanScope 是任务开工前声明的「计划改动文件」白名单（glob，repo-relative 正斜杠路径）。
	// 对应「打算改哪些文件」的规划前置——把它变成可度量契约。advisory：实改文件（TaskChangedFiles）
	// 与之的差集记 scope-drift 供 review，不阻塞。变更影响分析召回率仅 ~44%，故 scope 当 prediction
	// 而非 contract，drift 是常态信号。task start --scope 声明，task scope add 中途迭代追加（分层定位）。
	PlanScope []string `json:"plan_scope,omitempty"`

	// Overrides holds the per-task escape-hatch settings (plan-5 anti-leak): they take precedence over the global env
	// FORGE_WORK_ACTIVITY/FORGE_TEST_COVERAGE. One task escaping does not pollute other tasks in the same shell.
	// Using a VERIFICATION-class escape hatch → CheckEscapeHatch → Strength capped at Weak (evidence-scaled since
	// 2026-08: ratio>=0.85 && det>=20 exempt — see checklog.EscapeDowngradedStrength); work-activity (rhythm gate)
	// never caps. Set via `forge task override`.
	//
	// Overrides 承载 per-task 逃生舱设置（方案5 防泄漏）：优先于全局 env
	// FORGE_WORK_ACTIVITY/FORGE_TEST_COVERAGE。一个任务逃生不污染同 shell 的其他任务。
	// 用了验证类逃生舱 → CheckEscapeHatch → Strength cap Weak（2026-08 起证据缩放：
	// ratio>=0.85 且 det>=20 豁免——见 checklog.EscapeDowngradedStrength）；work-activity
	//（节奏门禁）永不 cap。由 `forge task override` 设置。
	Overrides TaskOverrides `json:"overrides,omitempty"`

	// Continuity source of truth: promotes plan/decisions/next-steps/blockers/cross-tool-findings/artifacts from in-session
	// transient state (agent context, lost on compaction) and discipline-reliant markdown (HANDOFF.md/AI_CONTEXT.md) into structured
	// first-class fields of the task. Any new session cold-starts forge task resume to pull them back, and same-machine
	// cross-tool/cross-person handoff continues from the same record. It corresponds to the information structure of
	// session-continuity HANDOFF + cross-tool-context AI_CONTEXT, but is persisted into the user-level DataDir/tasks/<ref>.json
	// rather than relying on the agent to self-disciplinedly read/write md. Boundary: user-level state does not travel with the
	// repo — cross-machine handoff needs an explicit export/import vehicle (not built yet).
	//
	// 接续真相源（continuity）：把 plan/决策/下一步/阻塞/跨工具发现/产物从会话内临时状态
	// （agent 上下文，压缩即丢）和靠纪律的 markdown（HANDOFF.md/AI_CONTEXT.md）升格为 task 的
	// 结构化一等公民字段。任何新会话冷启动 forge task resume 即拉回，同机跨工具/跨人基于同一份
	// 记录接续。对应 session-continuity HANDOFF + cross-tool-context AI_CONTEXT 的信息结构，
	// 但持久化进用户级 DataDir/tasks/<ref>.json 而非靠 agent 自觉读写 md。边界：用户级 state
	// 不随仓库走——跨机器接续需要显式的 export/import 载体（尚未建）。
	Kind          string        `json:"kind,omitempty"`            // "" | "code" = 走 3 道门禁（默认，向后兼容）；"generic" = 不走门禁，承载调研/设计/纯接续任务
	OriginTool    string        `json:"origin_tool,omitempty"`     // 声明式发起工具（pi/claude-code/opencode/codex/cursor…）；区别于 SessionRecord.AgentType 的目录探测弱信号
	Goal          string        `json:"goal,omitempty"`            // 目标叙述（可多行；比 Summary 一行标题更丰富，是"为什么做"）
	Plan          string        `json:"plan,omitempty"`            // 计划正文（markdown；--plan file 读入或直接传文本）
	SessionLinks  []SessionLink `json:"session_links,omitempty"`   // 参与本 task 的全部 session 锚定（含创建方），多向锚定——支持 pi 起、claude-code 接的跨工具/跨会话接续
	Decisions     []Decision    `json:"decisions,omitempty"`       // 已确认决策（AI_CONTEXT.md 的 Decisions 节升格）
	NextSteps     []string      `json:"next_steps,omitempty"`      // 下一步（HANDOFF 的"下一步"升格）
	Blockers      []Blocker     `json:"blockers,omitempty"`        // 阻塞项（HANDOFF 的"已知问题/阻塞"升格）
	Findings      []Finding     `json:"findings,omitempty"`        // 跨工具发现的问题（AI_CONTEXT.md 的 Findings 节升格，带来源工具）
	Artifacts     []Artifact    `json:"artifacts,omitempty"`       // 相关产物（文件/命令输出/url，关联但不门禁）
	ParentTaskRef string        `json:"parent_task_ref,omitempty"` // 子任务指向父 task ref（subtask 拆解）
	DependsOn     []string      `json:"depends_on,omitempty"`      // 依赖的前序 task ref（任务间依赖）
	Assignment    *Assignment   `json:"assignment,omitempty"`      // 任务分派（owner agent + 协作生命周期状态）；nil = 普通未分派任务，零行为变化
	// DocReview is the L2 re-check evidence of the output→re-check loop
	// (docgate.go). nil on pre-doc-gate tasks — zero behavior change; the gate
	// treats nil as "not reviewed" only when the task actually changed markdown
	// deliverables. DocReviewHistory retains prior rounds (convergence is the
	// observable value of the loop: two rounds without Criticals dropping is an
	// anomaly signal) — capped by the CLI writer.
	//
	// DocReview 是输出→回检循环的 L2 回检证据（docgate.go）。doc gate 之前的
	// 任务为 nil——零行为变化；仅当任务确实变更了 markdown 产物时，门禁才把
	// nil 视为「未回检」。DocReviewHistory 保留历史轮次（循环的可观测价值在于
	// 收敛趋势：两轮之间 Critical 不降即异常信号）——由 CLI 写入方截断。
	DocReview        *DocReview  `json:"doc_review,omitempty"`
	DocReviewHistory []DocReview `json:"doc_review_history,omitempty"`
	// Lease is the cross-machine node claim (sync-convergence.md §4): advisory in v1
	// (personal profile), fencing-monotonic so merges always pick the newest claim.
	// nil on pre-multi-machine tasks — zero behavior change.
	//
	// Lease 是跨机器节点认领（sync-convergence.md §4）：v1 为 advisory（个人档），
	// fencing 单调使合并恒取最新认领。多机器前的任务为 nil——零行为变化。
	Lease *Lease `json:"lease,omitempty"`

	// TTL is the per-task zombie-window override (design §3/§9 --ttl). When > 0 it overrides the
	// global Offered/Claimed/InputReq zombie constants for THIS task only, so a short-fuse delegation
	// surfaces as stale sooner (or a long runner gets more room) without changing the window every
	// other task shares. Zero — the default for tasks started without --ttl — falls back to the
	// global constant: fully backward compatible, no migration. health.effectiveTTL is the read side.
	//
	// TTL 是 per-task 僵尸窗口覆盖（设计 §3/§9 --ttl）。> 0 时仅对本任务覆盖全局
	// Offered/Claimed/InputReq 僵尸常量：短时效分派更快被标失联（或长跑任务给更多余量），而不改
	// 所有其他任务共享的窗口。零值——不带 --ttl 启动的默认——回落全局常量：完全向后兼容，无需迁移。
	// health.effectiveTTL 是读取侧。
	TTL time.Duration `json:"ttl,omitempty"`
}

// TaskGateResult records the result of a single task gate.
//
// TaskGateResult 记录单道 task gate 的结果。
type TaskGateResult struct {
	Gate        string    `json:"gate"`
	Passed      bool      `json:"passed"`
	CompletedAt time.Time `json:"completed_at"`
	HeadCommit  string    `json:"head_commit,omitempty"` // gate 通过时的 git HEAD
}

// ReviewRound records one `forge review pass` event (the reviewed snapshot + when).
//
// ReviewRound 记录一次 `forge review pass` 事件（审过的快照 + 时间）。
type ReviewRound struct {
	HeadCommit string    `json:"head_commit,omitempty"`
	ChangeHash string    `json:"change_hash,omitempty"`
	ReviewedAt time.Time `json:"reviewed_at"`
}

// IsComplete returns true when all task gates have passed.
//
// IsComplete 在所有 task gate 通过时返回 true。
func (s *TaskState) IsComplete() bool {
	if len(s.History) == 0 {
		return false
	}
	gates := DefaultGates()
	for _, g := range gates {
		if !s.gatePassed(g.ID) {
			return false
		}
	}
	return true
}

// NextGate returns the next incomplete gate in the sequence, or an empty string when all are complete.
//
// NextGate 返回序列中下一道未完成 gate，全部完成返回空串。
func (s *TaskState) NextGate() string {
	gates := DefaultGates()
	for _, g := range gates {
		if !s.gatePassed(g.ID) {
			return g.ID
		}
	}
	return ""
}

// MarkComplete records the task completion time. It also reconciles the assignment state
// machine with the task pipeline (dogfood 2026-08-18 脱节修复): a task can finish its gates
// without anyone running claim/deliver, leaving the assignment suspended at offered — mine
// then renders a COMPLETED task as 待认领 forever and the zombie scan flags it after 7d. So
// completion auto-reclaims an in-flight assignment (offered/claimed/input-required) to
// delivered, stamping DeliveredAt and AutoDelivered (the audit trail for "not a human
// deliver"). Terminal states (delivered/failed/canceled) and unassigned tasks are no-ops.
// Placing the reconcile HERE (not in the CLI) covers both complete paths — runTaskCompleteAt
// and completeGenericTask — plus any future caller, keeping the CLI a thin shell.
//
// MarkComplete 记录 task 完成时间。同时把分派状态机与任务管线 reconcile（dogfood 2026-08-18
// 脱节修复）：任务可能走完门禁却无人执行 claim/deliver，分派悬置在 offered——mine 会把已完成
// 任务永久渲染成待认领，僵尸扫描 7 天后误报。故完成时把在途分派（offered/claimed/
// input-required）自动回收为 delivered，记 DeliveredAt 与 AutoDelivered（「非人工 deliver」的
// 审计痕迹）。终态（delivered/failed/canceled）与无分派任务为 no-op。reconcile 放在此处
// （而非 CLI）使两条完成路径——runTaskCompleteAt 与 completeGenericTask——及任何未来调用方
// 都被覆盖，CLI 保持薄表层。
func (s *TaskState) MarkComplete() {
	now := time.Now()
	s.CompletedAt = &now
	s.CurrentGate = ""
	if s.Assignment != nil {
		switch s.Assignment.Status {
		case AssignOffered, AssignClaimed, AssignInputRequired:
			s.Assignment.Status = AssignDelivered
			s.Assignment.DeliveredAt = &now
			s.Assignment.AutoDelivered = true
			// Reclaiming from input-required bypasses Answer() — clear the pending question
			// text so no reader surfaces a stale "current question" (mirrors Answer's cleanup).
			//
			// 从 input-required 回收绕过 Answer()——清掉待答问题文本，避免读者看到陈旧的
			// 「当前问题」（镜像 Answer 的清理）。
			s.Assignment.LastQuestion = ``
		}
	}
}

// IsDelivered reports whether this task's output is available to dependents — the unblock signal
// for DependsOn. An ASSIGNED task's delivery is its Assignment status alone: deliver sets
// Status==delivered, and reopen/fail/cancel revoke it. IsComplete is deliberately NOT consulted
// for assigned tasks — it stays true across a reopen (gate history is retained), so using it would
// falsely unblock dependents after a bug-driven reopen of a task that had finished its gates. A
// task with NO assignment (ordinary/generic) has only the completion signal, so IsComplete is its
// delivery. Missing/aborted tasks are never loaded, so the caller does not call IsDelivered on them.
//
// IsDelivered 报告本 task 的产出是否对依赖方可用——DependsOn 的放行信号。有分派的 task 的交付只看
// Assignment 状态：deliver 置 Status==delivered，reopen/fail/cancel 撤销。刻意不对分派 task 查
// IsComplete——它跨 reopen 仍 true（gate 历史保留），若用它会在「已过门禁的 task 因 bug 被 reopen」后
// 错误放行依赖方。无分派的 task（普通/generic）只有完成信号，故 IsComplete 即其交付。缺失/已 abort
// 的 task 根本不被 load，故调用方不会对它们调此方法。
func (s *TaskState) IsDelivered() bool {
	if s.Assignment != nil {
		return s.Assignment.Status == AssignDelivered
	}
	return s.IsComplete()
}

// AddDependency appends refs to DependsOn with dedup + cycle detection, all-or-nothing: validated
// refs accumulate in a local slice and commit to s.DependsOn only after every ref passes, so a
// mid-batch cycle error leaves s.DependsOn untouched (the caller need not reason about partial
// writes). lookup resolves a ref to its TaskState so the cycle check can walk the dependency chain
// without types.go reaching back into the storage layer (the caller injects LoadTaskState). Adding
// a ref whose transitive dependencies lead back to this task is rejected — a cycle would deadlock
// every task in the ring. A self-reference (ref == this task) is likewise rejected. Dedup checks
// both the existing DependsOn and the current batch (so --depends-on A --depends-on A collapses).
//
// AddDependency 把 refs 追加进 DependsOn，去重 + 环检测，all-or-nothing：校验通过的 ref 先攒在局部
// slice，全部通过后才提交到 s.DependsOn，故批次中途的环错误不碰 s.DependsOn（调用方无需操心部分写入）。
// lookup 把 ref 解析成 TaskState，使环检测能遍历依赖链而 types.go 不反向依赖存储层（调用方注入
// LoadTaskState）。若某 ref 的传递依赖最终指回本 task 则拒绝——环会让环上每个 task 死锁。自引用
// （ref == 本 task）同样拒绝。去重同时查既有 DependsOn 与本批次（故 --depends-on A --depends-on A 折叠）。
//
// 并发契约：环检测的 lookup 遍历「其他」task 的 DependsOn，而 MutateTaskState 只串行化「单个」task 的
// load→mutate→save——故本方法不自行加跨任务锁。当前唯一生产调用方（cli/task.go 的 task start）作用于一
// 个全新构建、无并发的 state，今天安全。任何未来在 MutateTaskState 锁内调用本方法的调用方，须自行保证
// lookup 反映一个静止的依赖图（跨 lookup 涉及的所有 task 串行），否则并发 AddDependency 可能在 DFS 进行
// 中加边，使 visited-set BFS 漏判一个短暂存在的环。需要并发安全的图变更应改走 per-task 锁遍历。
func (s *TaskState) AddDependency(refs []string, lookup func(string) *TaskState) error {
	added := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref == `` {
			continue
		}
		if ref == s.TaskRef {
			return fmt.Errorf(`dependency cycle: %s 不能依赖自身`, s.TaskRef)
		}
		dup := false
		for _, d := range s.DependsOn {
			if d == ref {
				dup = true
				break
			}
		}
		if !dup {
			for _, d := range added {
				if d == ref {
					dup = true
					break
				}
			}
		}
		if dup {
			continue
		}
		if createsCycle(s.TaskRef, ref, lookup) {
			return fmt.Errorf(`dependency cycle: %s→%s 将引入环（环上每个 task 互相等待，死锁）`, s.TaskRef, ref)
		}
		added = append(added, ref)
	}
	s.DependsOn = append(s.DependsOn, added...)
	return nil
}

// createsCycle reports whether walking the DependsOn chain from start (via lookup) ever reaches
// self — i.e. start transitively depends on self, so self→start would close a ring. DFS with a
// visited set guards against pre-existing cycles and bounds the walk.
//
// createsCycle 报告从 start 出发沿 DependsOn 链（经 lookup）是否会到达 self——即 start 传递依赖
// self，故 self→start 会闭合环。DFS + visited 集合既防预存环也限查找范围。
func createsCycle(self, start string, lookup func(string) *TaskState) bool {
	visited := map[string]bool{}
	stack := []string{start}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == self {
			return true
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if t := lookup(cur); t != nil {
			stack = append(stack, t.DependsOn...)
		}
	}
	return false
}

// MarkReviewPassed records that this task has run code-review-gate and passed,
// binding the code snapshot at review time (headCommit, changeHash). It is a hard prerequisite of the task-complete gate
// (see executor.go) — ensuring the sub-agent review actually ran before submission; the snapshot lets task-complete enforce a mandatory re-review after any post-review code change.
// An empty headCommit → skip the snapshot check (old-state compatible / for tests), keeping only the ReviewPassed hard-prerequisite semantics.
//
// MarkReviewPassed 记录本 task 已跑过 code-review-gate 且通过，
// task, 并绑定审查时的代码快照 (headCommit, changeHash)。它是 task-complete 门禁的硬前置
// (see executor.go)——确保提交前子 agent 审查真的跑过；快照让 task-complete 能强制「审查后改码必复审」。
// headCommit 为空 → 跳过快照检查（老 state 兼容 / 测试用），仅保留 ReviewPassed 硬前置语义。
func (s *TaskState) MarkReviewPassed(headCommit, changeHash string) {
	s.ReviewPassed = true
	s.ReviewedHeadCommit = headCommit
	s.ReviewedChangeHash = changeHash
	s.ReviewRounds = append(s.ReviewRounds, ReviewRound{
		HeadCommit: headCommit,
		ChangeHash: changeHash,
		ReviewedAt: time.Now(),
	})
}

// ReworkRounds derives the review-rework loop counts from recorded history:
// reviewPasses = number of `forge review pass` events, completeRejections = failed task-complete
// gate attempts (each forced re-review / blocked complete is one rework round). Pure derivation,
// no new state.
//
// ReworkRounds 从已记录的历史推导审查-返工循环计数：reviewPasses = `forge review pass`
// 次数，completeRejections = task-complete 失败次数（每次强制复审/被拒 complete 算一轮
// 返工）。纯推导，不新增状态。
func (s *TaskState) ReworkRounds() (reviewPasses, completeRejections int) {
	reviewPasses = len(s.ReviewRounds)
	for _, h := range s.History {
		if h.Gate == "task-complete" && !h.Passed {
			completeRejections++
		}
	}
	return reviewPasses, completeRejections
}

// RecordGateResult appends a gate result and advances CurrentGate.
// If the gate already passed, it is a no-op (preventing stop-hook repeated verify from producing duplicate history entries).
// A previously failed gate can be retried, adding a new entry.
//
// RecordGateResult 添加 gate 结果并推进 CurrentGate。
// 若该 gate 已通过则为 no-op（防止 stop hook 重复 verify 产生重复 history 条目）。
// 先前失败的 gate 可重试，会新增一条 entry。
func (s *TaskState) RecordGateResult(gateID string, passed bool, headCommit string) {
	// Skip if this gate already passed — prevents stop-hook from repeatedly verifying the same gate and producing 25x duplicate entries.
	// 本 gate 已通过则跳过——防止 stop hook 反复 verify 同一 gate 产生 25 倍重复
	// 条目。
	if passed && s.gatePassed(gateID) {
		return
	}

	s.History = append(s.History, TaskGateResult{
		Gate:        gateID,
		Passed:      passed,
		CompletedAt: time.Now(),
		HeadCommit:  headCommit,
	})
	if passed {
		s.CurrentGate = s.NextGate()
	} else {
		s.CurrentGate = gateID
	}
}

// gatePassed checks whether the specified gate has passed.
//
// gatePassed 检查指定 gate 是否通过。
func (s *TaskState) gatePassed(gateID string) bool {
	for _, r := range s.History {
		if r.Gate == gateID && r.Passed {
			return true
		}
	}
	return false
}

// CompletedGates returns the list of passed gate IDs.
//
// CompletedGates 返回已通过 gate ID 列表。
func (s *TaskState) CompletedGates() []string {
	var result []string
	for _, g := range DefaultGates() {
		if s.gatePassed(g.ID) {
			result = append(result, g.ID)
		}
	}
	return result
}

// HasAcceptance reports whether the task has any persisted acceptance criterion.
//
// HasAcceptance 报告 task 是否有任何持久化验收标准。
func (s *TaskState) HasAcceptance() bool {
	return len(s.Acceptance) > 0
}

// AllAcceptancePassed reports whether every acceptance criterion has Passed=true.
// Empty acceptance returns true (nothing to reconcile). task-verify uses this to decide whether to remind about reconciliation.
//
// AllAcceptancePassed 报告是否所有 acceptance criterion 都 Passed=true。
// Empty acceptance returns true (nothing to reconcile). task-verify 据此决定是否提醒回扣。
func (s *TaskState) AllAcceptancePassed() bool {
	for i := range s.Acceptance {
		if !s.Acceptance[i].Passed {
			return false
		}
	}
	return true
}

// --- continuity methods ---
//
// --- 接续真相源（continuity）方法 ---

// TaskKindGeneric is the constant value of the generic kind. An empty Kind field or code both go through the gates (backward-compatible
// with old tasks that have no Kind field); only an explicit generic skips the gates.
//
// TaskKindGeneric 是 generic kind 的常量值。Kind 字段为空或"code"都走门禁（向后兼容
// 老 task 无 Kind 字段）；只有显式"generic"才不走门禁。
const TaskKindGeneric = "generic"

// IsGeneric reports whether the task is the non-gated type (Kind == generic). A generic task carries research/design/pure-handoff
// work, skips the implement→verify→complete gates, and is not scored on complete.
//
// IsGeneric 报告 task 是否为非门禁类型（Kind=="generic"）。generic task 承载调研/设计/纯
// 接续工作，不走 implement→verify→complete 门禁、complete 不评分。
func (s *TaskState) IsGeneric() bool { return s.Kind == TaskKindGeneric }

// HasContinuity reports whether the task carries any continuity content (any of goal/plan/decisions/next/blockers/
// findings/artifacts is non-empty). Used to decide whether resume has structured context to pull back.
//
// HasContinuity 报告 task 是否携带任何接续内容（goal/plan/decisions/next/blockers/
// findings/artifacts 任一非空）。用于判断 resume 是否有结构化上下文可拉回。
func (s *TaskState) HasContinuity() bool {
	return s.Goal != "" || s.Plan != "" ||
		len(s.Decisions) > 0 || len(s.NextSteps) > 0 ||
		len(s.Blockers) > 0 || len(s.Findings) > 0 || len(s.Artifacts) > 0
}

// AddSession anchors (sid, tool) to the task (session-deduped). Multi-way anchoring: on cross-tool/cross-session handoff, the successor
// attaches its own session+tool, and the task records all participants. It also backfills the single-valued SessionID (creator semantics)
// for backward compatibility — old code reading SessionID still gets the first session.
//
// When tool is empty, it does not fall back to OriginTool: if a successor (attach/resume) fails tool detection and falls back to the creator's
// OriginTool, a claude-code-handoff session would be misattributed to pi — the entire point of attach is cross-tool anchoring, and misattribution defeats it.
// The creator's task start passes OriginTool explicitly; the display-side SessionTools() falls back to OriginTool for an empty tool (store the raw value, fall back at display — correctly layered).
//
// AddSession 把 (sid, tool) 锚定到 task（session 去重）。多向锚定：跨工具/跨会话接续时，接手方
// 把自己的 session+工具挂上，task 即记录所有参与方。同时回填单值 SessionID（创建方语义）
// 保持向后兼容——老代码读 SessionID 仍拿到首个 session。
//
// tool 为空时不回退 OriginTool：接手方（attach/resume）若 tool 探测失败却回退到创建方
// OriginTool，会把 claude-code 接手的 session 错误归属成 pi——attach 的整个存在意义就是跨工具
// 锚定，错误归属让它失效。创建方 task start 显式传 OriginTool；显示侧 SessionTools() 对空
// tool 做 OriginTool 兜底（存储存原值，显示做回退，分层正确）。
func (s *TaskState) AddSession(sid, tool string) {
	if sid == "" {
		return
	}
	// Dedup against LOCAL links only: an imported ghost with the same sid (cross-machine collision)
	// must not swallow this local attach — see SessionLink.Imported for the ghost semantics.
	//
	// 只对本机链接去重：同 sid 的导入幽灵（跨机器碰撞）不能吞掉本次本机 attach——幽灵语义见
	// SessionLink.Imported。
	for _, l := range s.SessionLinks {
		if !l.Imported && l.SessionID == sid {
			return
		}
	}
	s.SessionLinks = append(s.SessionLinks, SessionLink{
		SessionID: sid,
		Tool:      tool,
		JoinedAt:  time.Now(),
	})
	if s.SessionID == "" {
		s.SessionID = sid
	}
}

// SessionTools returns the deduped list of tools that participated in this task (in first-appearance order). For resume/dashboard display of who participated.
// A SessionLink with an empty Tool (left by the creator's task start when OriginTool detection failed) falls back to OriginTool
// for display — the storage layer AddSession does not fall back (to avoid misattributing a successor session to the creator's tool), and the display layer
// falls back per-link so the dashboard does not drop the creator.
//
// SessionTools 返回参与过本 task 的工具去重列表（按首次出现序）。供 resume/看板显示谁参与过。
// 空 Tool 的 SessionLink（创建方 task start 时 OriginTool 探测失败留下的空 tool）用 OriginTool
// 兜底显示——存储层 AddSession 不回退（避免接手方 session 错误归属到创建方工具），显示层
// per-link 兜底让看板不漏创建方。
func (s *TaskState) SessionTools() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range s.SessionLinks {
		tool := l.Tool
		if tool == "" {
			tool = s.OriginTool
		}
		if tool != "" && !seen[tool] {
			seen[tool] = true
			out = append(out, tool)
		}
	}
	return out
}

// HasSession reports whether sid is already anchored to this task as a LOCAL session.
// Imported (ghost) links are ignored: a ghost records that sid participated on another machine,
// not that the local session is anchored — so attach always proceeds for a local session even if a
// ghost with the same id was imported (cross-machine collision). Use HasAnySession to count ghosts too.
//
// HasSession 报告 sid 是否已作为本机 session 锚定到本 task。Imported（幽灵）链接被忽略：幽灵只记录
// 该 sid 在另一台机器上参与过，不代表本机 session 已锚定——故即便导入了同 id 的幽灵（跨机器碰撞），
// 本机 attach 仍照常进行。统计幽灵用 HasAnySession。
func (s *TaskState) HasSession(sid string) bool {
	for _, l := range s.SessionLinks {
		if l.Imported {
			continue
		}
		if l.SessionID == sid {
			return true
		}
	}
	return false
}

// HasAnySession reports whether any link (local OR imported ghost) carries sid — used where the full
// provenance record matters (e.g. de-duping a re-import), as opposed to HasSession which answers the
// local-anchoring question. Kept separate so the attach path can never mistake a ghost for a local anchor.
//
// HasAnySession 报告是否有任意链接（本机或导入幽灵）携带 sid——用于关心完整溯源记录处（如重复 import
// 去重），区别于回答「本机是否已锚定」的 HasSession。分离二者使 attach 路径绝不会把幽灵当本机锚点。
func (s *TaskState) HasAnySession(sid string) bool {
	for _, l := range s.SessionLinks {
		if l.SessionID == sid {
			return true
		}
	}
	return false
}

// AddDecision appends a decision (auto-filling ID and time).
//
// AddDecision 追加一条决策（自动补 ID 和时间）。
func (s *TaskState) AddDecision(d Decision) {
	if d.ID == "" {
		d.ID = newContinuityID("d")
	}
	if d.DecidedAt.IsZero() {
		d.DecidedAt = time.Now()
	}
	s.Decisions = append(s.Decisions, d)
}

// AddNext appends a next step.
//
// AddNext 追加一条下一步。
func (s *TaskState) AddNext(step string) {
	if step == "" {
		return
	}
	s.NextSteps = append(s.NextSteps, step)
}

// AddBlocker appends a blocker (defaulting to open).
//
// AddBlocker 追加一条阻塞（默认 open）。
func (s *TaskState) AddBlocker(b Blocker) {
	if b.ID == "" {
		b.ID = newContinuityID("b")
	}
	if b.RaisedAt.IsZero() {
		b.RaisedAt = time.Now()
	}
	if b.Status == "" {
		b.Status = "open"
	}
	s.Blockers = append(s.Blockers, b)
}

// ResolveBlocker marks the blocker with the given ID as resolved (with a resolution note). Returns false if not found.
//
// ResolveBlocker 把指定 ID 的阻塞标为 resolved（附 resolution 说明）。未找到返 false。
func (s *TaskState) ResolveBlocker(id, resolution string) bool {
	for i := range s.Blockers {
		if s.Blockers[i].ID == id {
			s.Blockers[i].Status = "resolved"
			s.Blockers[i].Resolution = resolution
			return true
		}
	}
	return false
}

// OpenBlockers returns all unresolved (open) blockers.
//
// OpenBlockers 返回所有未解决（open）阻塞。
func (s *TaskState) OpenBlockers() []Blocker {
	var out []Blocker
	for _, b := range s.Blockers {
		if b.Status == "open" || b.Status == "" {
			out = append(out, b)
		}
	}
	return out
}

// AddFinding appends a cross-tool finding.
//
// AddFinding 追加一条跨工具发现。
func (s *TaskState) AddFinding(f Finding) {
	if f.ID == "" {
		f.ID = newContinuityID("f")
	}
	if f.RaisedAt.IsZero() {
		f.RaisedAt = time.Now()
	}
	if f.Status == "" {
		f.Status = "open"
	}
	s.Findings = append(s.Findings, f)
}

// ResolveFinding marks the finding with the given ID as fixed. Returns false if not found.
//
// ResolveFinding 把指定 ID 的 finding 标为 fixed。未找到返 false。
func (s *TaskState) ResolveFinding(id string) bool {
	for i := range s.Findings {
		if s.Findings[i].ID == id {
			s.Findings[i].Status = "fixed"
			return true
		}
	}
	return false
}

// AddArtifact appends an artifact reference.
//
// AddArtifact 追加一条产物引用。
func (s *TaskState) AddArtifact(a Artifact) {
	s.Artifacts = append(s.Artifacts, a)
}

// --- assignment methods ---
//
// --- 分派方法（assignment）---

// HasAssignment reports whether the task is delegated to an agent (Assignment != nil).
//
// HasAssignment 报告 task 是否已分派给某 agent（Assignment != nil）。
func (s *TaskState) HasAssignment() bool { return s.Assignment != nil }

// IsOfferedTo reports whether the task is offered (awaiting claim) to the given agent.
// Note: mine matches by Assignment.Agent across ALL statuses (incl. delivered/failed/canceled), so
// it does NOT use this method — IsOfferedTo is a state-machine predicate for future hook/TTL logic
// needing the offered-awaiting-claim signal. Agent normalization is the caller's duty so the
// storage layer stays agent-neutral.
//
// 注意：mine 按 Assignment.Agent 匹配全状态（含 delivered/failed/canceled），不用此方法——
// IsOfferedTo 是状态机谓词，供未来 hook/TTL 判定 offered 待认领态用。agent 归一化是调用方职责，
// 使存储层保持 agent-neutral。
func (s *TaskState) IsOfferedTo(agent string) bool {
	return s.Assignment != nil && s.Assignment.Status == AssignOffered && s.Assignment.Agent == agent
}

// ShouldNotify reports whether this offered task should be pushed at SessionStart at instant now
// (design §8 ③ NotifiedAt 去重). Rule: the offer-baseline — max(OfferedAt, AbandonedAt), mirroring
// IsOfferedZombie — must be newer than the last NotifiedAt (or NotifiedAt must be nil). After
// Abandon re-offers a task, AbandonedAt advances past the prior NotifiedAt, so the next session
// re-notifies; without a fresh abandon/re-offer, subsequent sessions stay silent (anti-轰炸). now
// is injected (not time.Now) so tests are deterministic. nil-receiver safe.
//
// ShouldNotify 报告此 offered 任务在 now 时刻是否应在 SessionStart 推送（设计 §8 ③ NotifiedAt
// 去重）。规则：派发基线——max(OfferedAt, AbandonedAt)，镜像 IsOfferedZombie——须晚于上次
// NotifiedAt（或 NotifiedAt 为 nil）。Abandon 重新派发后 AbandonedAt 越过旧 NotifiedAt，故下次
// 会话重新推送；无新的 abandon/重新派发则后续会话保持静默（防轰炸）。now 注入（非 time.Now）
// 使测试确定。nil 接收者安全。
func (a *Assignment) ShouldNotify(now time.Time) bool {
	if a == nil || a.Status != AssignOffered {
		return false
	}
	var baseline time.Time
	if a.OfferedAt != nil {
		baseline = *a.OfferedAt
	}
	if a.AbandonedAt != nil && a.AbandonedAt.After(baseline) {
		baseline = *a.AbandonedAt
	}
	if a.NotifiedAt == nil {
		return true // 首次推送（即便基线为零——legacy 状态也推一次）
	}
	if baseline.IsZero() {
		return false // 无基线可越过 NotifiedAt——已推过一次
	}
	return baseline.After(*a.NotifiedAt)
}

// AssignTo creates an offered-status Assignment delegating the task to agent. It refuses if an
// assignment already exists (re-delegation goes through a dedicated reassign path that records the
// prior owner), so a task never silently changes owner. by is the orchestrator agent that offered it.
//
// AssignTo 创建一个 offered 状态的 Assignment，把 task 派给 agent。若已存在分派则拒绝（改派走专门
// 的 reassign 路径以记录原 owner），故 task 绝不静默易主。by 是发起派发的编排器 agent。
func (s *TaskState) AssignTo(agent, role, by string) error {
	if agent == `` {
		return errAssignmentEmptyAgent
	}
	if s.Assignment != nil {
		return errAssignmentExists
	}
	now := time.Now()
	s.Assignment = &Assignment{
		Agent:     agent,
		Role:      role,
		Status:    AssignOffered,
		OfferedBy: by,
		OfferedAt: &now,
	}
	return nil
}

// Claim transitions offered→claimed, anchoring owner work. It requires the claiming agent to match
// the offered Agent (a kimi-offered task cannot be claimed by reasonix), and refuses if the status is
// not offered (already claimed / delivered / canceled). The caller sets the session's active-task-ref
// after a successful claim (claim = start working) — done in the CLI layer, not here, so this storage
// method stays free of session/root coupling.
//
// Claim 把 offered→claimed，锚定 owner 工作。要求认领 agent 匹配派发的 Agent（派给 kimi 的任务
// reasonix 不能认领），且 status 非 offered（已认领/已交付/已取消）则拒绝。认领成功后由调用方
// （CLI 层）设 session 的 active-task-ref（认领 = 开始工作）——不放存储方法，使方法不耦合 session/root。
func (s *TaskState) Claim(agent string) error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	if s.Assignment.Agent != agent {
		return errClaimWrongAgent
	}
	if s.Assignment.Status != AssignOffered {
		return errClaimNotOffered
	}
	now := time.Now()
	s.Assignment.Status = AssignClaimed
	s.Assignment.ClaimedAt = &now
	return nil
}

// Deliver transitions claimed→delivered — the signal that unblocks dependents. Requires claimed
// (forbids the offered→delivered skip). DeliveredAt set.
//
// Deliver 把 claimed→delivered——这是放行依赖方的信号。要求 claimed（禁止 offered→delivered 跳跃）。设 DeliveredAt。
func (s *TaskState) Deliver() error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	if s.Assignment.Status != AssignClaimed {
		return errDeliverNotClaimed
	}
	now := time.Now()
	s.Assignment.Status = AssignDelivered
	s.Assignment.DeliveredAt = &now
	return nil
}

// Question transitions claimed→input-required, recording a回抛 (worker needs the orchestrator/human to
// clarify before proceeding). Requires claimed. The orchestrator's answer (task answer) appends this
// question into Decisions so the resolution is traceable.
//
// Question 把 claimed→input-required，记一条回抛（worker 需编排器/人澄清后才能继续）。要求 claimed。
// 编排器的答复（task answer）会把这个 question 追加进 Decisions，使决议可追溯。
func (s *TaskState) Question(content string) error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	if s.Assignment.Status != AssignClaimed {
		return errQuestionNotClaimed
	}
	now := time.Now()
	s.Assignment.Status = AssignInputRequired
	s.Assignment.LastQuestion = content
	s.Assignment.QuestionAt = &now
	return nil
}

// Answer transitions input-required→claimed, recording the orchestrator's resolution as a Decision so
// the回抛's outcome is durable (survives compaction, visible cross-tool). Requires input-required.
//
// Answer 把 input-required→claimed，把编排器的答复记成一条 Decision，使回抛的结局持久（抗压缩、跨工具可见）。要求 input-required。
func (s *TaskState) Answer(content string) error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	if s.Assignment.Status != AssignInputRequired {
		return errAnswerNotInputReq
	}
	s.Assignment.Status = AssignClaimed
	if content != `` {
		q := s.Assignment.LastQuestion
		s.AddDecision(Decision{Content: content, By: s.Assignment.OfferedBy, Rationale: q})
	}
	// The回抛 is now resolved — clear its text so a later deliver / re-open does not surface stale
	// "current question" content to a reader (mirrors Abandon clearing ClaimedAt on the reverse
	// transition). QuestionAt (the timestamp) is kept as history; only the pending text is ephemeral.
	//
	// 回抛已解决——清其文本，使后续 deliver/re-open 不向读者暴露陈旧的「当前问题」内容（镜像
	// Abandon 在反向转换时清 ClaimedAt）。QuestionAt（时间戳）作为历史保留；仅 pending 文本是短暂的。
	s.Assignment.LastQuestion = ``
	return nil
}

// Fail transitions claimed→failed, recording why the owner could not complete it. Requires claimed.
//
// Fail 把 claimed→failed，记录 owner 为何无法完成。要求 claimed。
func (s *TaskState) Fail(reason string) error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	if s.Assignment.Status != AssignClaimed {
		return errFailNotClaimed
	}
	s.Assignment.Status = AssignFailed
	s.Assignment.FailReason = reason
	return nil
}

// Cancel transitions a non-terminal task (offered/claimed/input-required)→canceled, recording the
// orchestrator's reason for withdrawing the delegation. Terminal states (delivered/failed/canceled)
// cannot be canceled.
//
// Cancel 把非终态 task（offered/claimed/input-required）→canceled，记录编排器撤回分派的原因。终态（delivered/failed/canceled）不能 cancel。
func (s *TaskState) Cancel(reason string) error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	switch s.Assignment.Status {
	case AssignOffered, AssignClaimed, AssignInputRequired:
		s.Assignment.Status = AssignCanceled
		s.Assignment.CancelReason = reason
		return nil
	default:
		return errCancelTerminal
	}
}

// Reopen transitions delivered→claimed when a delivered task is found to have a bug. The reason is
// recorded as a FailReason-equivalent note on the assignment for traceability.
//
// Reopen 把 delivered→claimed，用于交付后发现 bug。原因记入 assignment 供追溯。
func (s *TaskState) Reopen(reason string) error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	if s.Assignment.Status != AssignDelivered {
		return errReopenNotDelivered
	}
	s.Assignment.Status = AssignClaimed
	s.Assignment.DeliveredAt = nil
	s.Assignment.AutoDelivered = false
	s.Assignment.FailReason = reason
	return nil
}

// IsReopened reports whether a delivered task was sent back for rework via Reopen. Reopen
// lands on claimed (or later input-required via Question) with FailReason set — and FailReason
// on a NON-failed status has no other writer (Fail() sets it only together with the failed
// terminal; import strips it), so (offered|claimed|input-required)+FailReason uniquely
// identifies the reopened shape. offered is included because Abandon() does not clear
// FailReason: reopen→claimed→(owner gone)→reclaim→offered leaves the residue, and without
// this branch the reclaimed rework would fall back under the completion immunity and turn
// invisible (review M1 复审). The distinction matters because IsComplete() deliberately stays
// true across a reopen (gate history is retained): completion-based consumers — the zombie
// immunity in assignmentInFlight and mine's `complete` render — must NOT treat a reopened
// task as finished, or a stuck rework would be silently hidden (review M1 of the 2026-08-18
// 脱节修复).
//
// IsReopened 报告一个已交付任务是否被 Reopen 打回返工。Reopen 落在 claimed（或再经
// Question 到 input-required）且置 FailReason——而非 failed 状态上的 FailReason 没有其他
// 写入方（Fail() 只在进入 failed 终态时设置；import 会清空），故 (offered|claimed|
// input-required)+FailReason 唯一标识 reopen 形态。offered 纳入是因为 Abandon() 不清
// FailReason：reopen→claimed→（认领方失联）→reclaim→offered 会留下残留，少了这个分支，
// 被回收的返工会重新落入完成免疫而彻底隐形（review M1 复审）。区分的意义：IsComplete()
// 跨 reopen 刻意保持 true（gate 历史保留）——基于完成态的消费者（assignmentInFlight 的
// 僵尸免疫与 mine 的 complete 渲染）不得把 reopen 的任务当成已完成，否则卡住的返工会被
// 静默隐藏（2026-08-18 脱节修复的 review M1）。
func (s *TaskState) IsReopened() bool {
	if s == nil || s.Assignment == nil {
		return false
	}
	switch s.Assignment.Status {
	case AssignOffered, AssignClaimed, AssignInputRequired:
		return s.Assignment.FailReason != ``
	}
	return false
}

// Abandon transitions claimed→offered (TTL recovery for a claimed task whose owner went away).
// Bumps AbandonedCount (a zombie signal surfaced in mine/health) and clears ClaimedAt so it is
// re-offered fresh. Requires claimed. The TTL trigger is forge task reclaim
// (cli/task_assignment.go runTaskReclaim), which scans for IsClaimedStale tasks and calls this.
//
// Abandon 把 claimed→offered（claimed 的 owner 失联时的 TTL 回收）。AbandonedCount++（在 mine/health
// 上浮的僵尸信号）并清 ClaimedAt 使其重新 offered。要求 claimed。TTL 触发是 forge task reclaim
// （cli/task_assignment.go runTaskReclaim），它扫描 IsClaimedStale 任务并调用本方法。
func (s *TaskState) Abandon() error {
	if s.Assignment == nil {
		return errNoAssignment
	}
	if s.Assignment.Status != AssignClaimed {
		return errAbandonNotClaimed
	}
	s.Assignment.Status = AssignOffered
	s.Assignment.ClaimedAt = nil
	s.Assignment.AbandonedCount++
	now := time.Now()
	s.Assignment.AbandonedAt = &now
	return nil
}

// continuityCounter ensures that continuity IDs generated within the same process at the same nanosecond do not collide either (low-precision Windows clock /
// consecutive test calls can yield identical UnixNano). resolve/resolveFinding hit by exact ID, and a collision would resolve-the-second-but-hit-the-first —
// a real bug for resolve accuracy, hence the atomic-increment seq suffix.
//
// But seq is a process-local variable, and each forge process starts it from 0; UnixNano has ~15ms resolution on Windows. Two parallel forge processes
// calling the same prefix within a 15ms window get the same nano + same seq → collision. So a 4-byte crypto/rand suffix is added to fully eliminate collisions
// (cross-process collision probability drops from a 15ms parallel window to 2^-32).
//
// continuityCounter 保证同一进程内同一纳秒生成的 continuity ID 也不碰撞（Windows 时钟低精度 /
// 测试连续调用会让 UnixNano 相同）。resolve/resolveFinding 按 ID 精确命中，碰撞会让「解决
// 第二条却命中首条」——对 resolve 准确性是真实 bug，故加原子递增 seq 后缀。
//
// 但 seq 是进程内变量，跨 forge 进程各自从 0 开始；UnixNano 在 Windows 约 15ms 分辨率。两个
// 并行 forge 进程在 15ms 窗口内同 prefix 调用会拿到相同 nano + 相同 seq → 碰撞。故再加 4 字节
// crypto/rand 后缀彻底去碰撞（跨进程碰撞概率从 15ms 并行窗口降到 2^-32）。
var continuityCounter uint64

// newContinuityID generates a short unique ID for a continuity entity (Decision/Blocker/Finding): prefix +
// UnixNano base36 (monotonic in time order) + atomic seq (intra-process same-nanosecond dedup) + 4 random bytes (cross-process collision elimination).
//
// newContinuityID 生成 continuity 实体（Decision/Blocker/Finding）的短唯一 ID：前缀 +
// UnixNano base36（时间序单调）+ 原子 seq（进程内同纳秒去重）+ 4 字节随机（跨进程去碰撞）。
func newContinuityID(prefix string) string {
	seq := atomic.AddUint64(&continuityCounter, 1)
	nano := strconv.FormatInt(time.Now().UnixNano(), 36)
	var b [4]byte
	if _, err := crand.Read(b[:]); err != nil {
		// crypto/rand rarely fails; on degradation, nano+seq still guarantees intra-process uniqueness (cross-process theoretical collision window 15ms).
		// crypto/rand 极少失败；退化时仍由 nano+seq 保证进程内唯一（跨进程理论碰撞窗口 15ms）。
		return prefix + nano + "-" + strconv.FormatUint(seq, 36)
	}
	return prefix + nano + "-" + strconv.FormatUint(seq, 36) + "-" + hex.EncodeToString(b[:])
}
