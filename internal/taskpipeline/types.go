package taskpipeline

import (
	crand "crypto/rand"
	"encoding/hex"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// TaskGate defines a lightweight task-level quality gate.
type TaskGate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Auto        bool   `json:"auto"` // true = checked automatically by hooks
}

// AcceptanceCriterion 是一条可执行的验收标准（来自 dev-workflow Plan 的
// "Run: <cmd>, Expected: <output>"）。持久化进 TaskState，使验收标准不随 plan 文本
// 消失；verify-acceptance 实跑 Run、比对 Expected，记 deterministic 证据——把 spec
// 变成不可伪造的验证，对冲 agent 自述"满足验收"的盲区。
type AcceptanceCriterion struct {
	Run      string `json:"run"`                // 实跑的命令（如 "go test ./..."）
	Expected string `json:"expected,omitempty"` // 期望输出的子串；空=只看退出码 0
	Passed   bool   `json:"passed,omitempty"`   // 上次 verify-acceptance 的结果
	Output   string `json:"output,omitempty"`   // 上次实跑的输出（截断），供排查
}

// Decision 是一条已确认的技术/产品决策（对应 cross-tool-context AI_CONTEXT.md 的
// Decisions 节，升格为结构化字段）。持久化进 TaskState，使决策不随会话压缩丢失、跨工具
// 可见——任何接手方 resume 即知"已经决定了什么、不要再推翻"。
type Decision struct {
	ID        string    `json:"id"`      // 稳定标识，供 resolve/引用
	Content   string    `json:"content"` // 决策内容
	DecidedAt time.Time `json:"decided_at"`
	By        string    `json:"by,omitempty"`        // 确认方（工具/人，如 [pi]/[claude-code]/人）
	Affects   []string  `json:"affects,omitempty"`   // 影响的文件/模块
	Rationale string    `json:"rationale,omitempty"` // 为什么这么决定（HANDOFF 纪律：写"为什么"不只写"是什么"）
}

// Blocker 是一项阻塞（对应 HANDOFF 的"已知问题/阻塞"）。Status 驱动工作流：
// open → resolved（已解决）/ wontfix（放弃解决）。
type Blocker struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	RaisedAt   time.Time `json:"raised_at"`
	Status     string    `json:"status"`               // open | resolved | wontfix
	Resolution string    `json:"resolution,omitempty"` // 解决方式（resolved/wontfix 时填）
	By         string    `json:"by,omitempty"`
}

// Finding 是某工具发现的问题/风险（对应 AI_CONTEXT.md 的 Findings 节）。带 Source 来源
// 工具，让跨工具协作时"谁发现的"可见——避免重复发现、便于回溯证据。
type Finding struct {
	ID       string    `json:"id"`
	Content  string    `json:"content"`
	Source   string    `json:"source"`             // 来源工具 [pi]/[claude-code]/[opencode]…
	Evidence string    `json:"evidence,omitempty"` // 证据（文件:行 / 命令输出）
	Status   string    `json:"status"`             // open | fixed | wontfix
	RaisedAt time.Time `json:"raised_at"`
}

// Artifact 是任务的相关产物引用（文件/命令输出/url/文档）。仅索引不门禁——让接手方知道
// "这个任务产出了什么、改了哪些关键文件"。
type Artifact struct {
	Path string `json:"path"` // 文件路径 / url
	Kind string `json:"kind"` // file | cmd-output | url | doc
	Note string `json:"note,omitempty"`
}

// SessionLink 是 task 与一个 agent session 的锚定（多向锚定的一项）。task 默认只记创建方
// session；接手方（跨会话/跨工具）通过 forge task attach 追加，形成 N 个 session 共同推进
// 一个 task 的双向锚定——任意接手方 resume 即知"谁参与过、用什么工具"。
type SessionLink struct {
	SessionID string    `json:"session_id"`
	Tool      string    `json:"tool,omitempty"` // 该 session 所属工具（pi/claude-code/opencode…）
	JoinedAt  time.Time `json:"joined_at"`
}

// TaskState tracks the state of a single task's pipeline.
// Stored in DataDir/tasks/{sanitized-ref}.json.
type TaskState struct {
	TaskRef      string                    `json:"task_ref"`
	Branch       string                    `json:"branch"`
	Source       string                    `json:"source"` // "explicit", "branch"
	Summary      string                    `json:"summary"`
	CurrentGate  string                    `json:"current_gate"`
	History      []TaskGateResult          `json:"history"`
	StartedAt    time.Time                 `json:"started_at"`
	CompletedAt  *time.Time                `json:"completed_at,omitempty"`
	Score        *scoringtypes.ScoreResult `json:"score,omitempty"`
	HeadCommit   string                    `json:"head_commit,omitempty"`   // for duplicate detection
	SessionID    string                    `json:"session_id,omitempty"`    // agent session that created this task
	ReviewPassed bool                      `json:"review_passed,omitempty"` // code-review-gate 通过标记；task-complete 门禁的硬前置
	ResumeStale  bool                      `json:"resume_stale,omitempty"` // gap#2 claude-code 根治层：PostCompact hook 设 true → 下个 UserPromptSubmit reinject 注入完整 handoff 后清零。codex/cursor/opencode 无 compaction lifecycle，ForgeHookSpec 过滤不装此链。task-scoped 非 session-scoped：两 session 共享同一 task 时，B 的 prompt 可能在 A 压缩后先消费并清掉标志（最坏漏注一次，handoff 内容相同故无数据损坏，可接受边界）。
	// ReviewedHeadCommit/ReviewedChangeHash 绑定 review pass 时的代码快照——审查-修复-复审闭环的关键。
	// review pass 时记 (HEAD, SourceChangesSince(HEAD))；task-complete 门禁重算 SourceChangesSince(ReviewedHeadCommit)
	// 比对 ReviewedChangeHash，不一致说明审查后改了码，强制复审（不再靠 agent 自律重审）。详见 executor.go。
	// commit-then-review 流（E2E 真实序列：先 commit 再 review，审查时工作区干净）→ ReviewedChangeHash 为空，
	// 故"基线已设"判据用 ReviewedHeadCommit != ""，不能用 hash 空/非空。
	ReviewedHeadCommit string                `json:"reviewed_head_commit,omitempty"`
	ReviewedChangeHash string                `json:"reviewed_change_hash,omitempty"`

	// DesignPhases 是 inferDesignPhases 在 task-verify gate 推断出的设计阶段。
	// 由 task-verify gate（executor.go ExecuteTaskGate）调 inferDesignPhases(taskChangedFiles)
	// 填充并 SaveTaskState 持久化，零摩擦：不要求用户声明。review 子 agent 据此加载对应
	// references/phase-X.md checklist。空 = 无匹配设计产物，回落到通用 review-checklist.md。
	DesignPhases []DesignPhase `json:"design_phases,omitempty"`
	Acceptance         []AcceptanceCriterion `json:"acceptance,omitempty"` // 验收标准（dev-workflow Plan 的 Run+Expected），verify-acceptance 实跑回扣
	// PlanScope 是任务开工前声明的"计划改动文件"白名单（glob，repo-relative 正斜杠路径）。
	// 对应 Terraform desired state / Copilot Workspace plan 的"打算改哪些文件"——把规划前置
	// 变成可度量契约。advisory：实改文件（TaskChangedFiles）与之的差集记 scope-drift 供 review，
	// 不阻塞。学术界变更影响分析召回率仅 ~44%（PASTE），故 scope 当 prediction 而非 contract，
	// drift 是常态信号。task start --scope 声明，task scope add 中途迭代追加（Agentless 分层定位）。
	PlanScope []string `json:"plan_scope,omitempty"`

	// 接续真相源（continuity）：把 plan/决策/下一步/阻塞/跨工具发现/产物从会话内临时状态
	// （agent 上下文，压缩即丢）和靠纪律的 markdown（HANDOFF.md/AI_CONTEXT.md）升格为 task 的
	// 结构化一等公民字段。任何新会话冷启动 forge task resume 即拉回，跨工具/跨人基于同一份
	// 记录接续。对应 session-continuity HANDOFF + cross-tool-context AI_CONTEXT 的信息结构，
	// 但持久化进 DataDir/tasks/<ref>.json 而非靠 agent 自觉读写 md。
	Kind          string     `json:"kind,omitempty"`            // "" | "code" = 走 3 道门禁（默认，向后兼容）；"generic" = 不走门禁，承载调研/设计/纯接续任务
	OriginTool    string     `json:"origin_tool,omitempty"`     // 声明式发起工具（pi/claude-code/opencode/codex/cursor…）；区别于 SessionRecord.AgentType 的目录探测弱信号
	Goal          string     `json:"goal,omitempty"`            // 目标叙述（可多行；比 Summary 一行标题更丰富，是"为什么做"）
	Plan          string     `json:"plan,omitempty"`            // 计划正文（markdown；--plan file 读入或直接传文本）
	SessionLinks  []SessionLink `json:"session_links,omitempty"` // 参与本 task 的全部 session 锚定（含创建方），多向锚定——支持 pi 起、claude-code 接的跨工具/跨会话接续
	Decisions     []Decision `json:"decisions,omitempty"`       // 已确认决策（AI_CONTEXT.md 的 Decisions 节升格）
	NextSteps     []string   `json:"next_steps,omitempty"`      // 下一步（HANDOFF 的"下一步"升格）
	Blockers      []Blocker  `json:"blockers,omitempty"`        // 阻塞项（HANDOFF 的"已知问题/阻塞"升格）
	Findings      []Finding  `json:"findings,omitempty"`        // 跨工具发现的问题（AI_CONTEXT.md 的 Findings 节升格，带来源工具）
	Artifacts     []Artifact `json:"artifacts,omitempty"`       // 相关产物（文件/命令输出/url，关联但不门禁）
	ParentTaskRef string     `json:"parent_task_ref,omitempty"` // 子任务指向父 task ref（subtask 拆解）
	DependsOn     []string   `json:"depends_on,omitempty"`      // 依赖的前序 task ref（任务间依赖）
}

// TaskGateResult records the outcome of a single task gate.
type TaskGateResult struct {
	Gate        string    `json:"gate"`
	Passed      bool      `json:"passed"`
	CompletedAt time.Time `json:"completed_at"`
	HeadCommit  string    `json:"head_commit,omitempty"` // git HEAD at gate pass time
}

// IsComplete returns true if all task gates have passed.
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

// NextGate returns the next incomplete gate in sequence, or "" if all done.
func (s *TaskState) NextGate() string {
	gates := DefaultGates()
	for _, g := range gates {
		if !s.gatePassed(g.ID) {
			return g.ID
		}
	}
	return ""
}

// MarkComplete records task completion time.
func (s *TaskState) MarkComplete() {
	now := time.Now()
	s.CompletedAt = &now
	s.CurrentGate = ""
}

// MarkReviewPassed records that code-review-gate has been run and passed for this
// task, 并绑定审查时的代码快照 (headCommit, changeHash)。它是 task-complete 门禁的硬前置
// (see executor.go)——确保提交前子 agent 审查真的跑过；快照让 task-complete 能强制"审查后改码必复审"。
// headCommit 为空 → 跳过快照检查（老 state 兼容 / 测试用），仅保留 ReviewPassed 硬前置语义。
func (s *TaskState) MarkReviewPassed(headCommit, changeHash string) {
	s.ReviewPassed = true
	s.ReviewedHeadCommit = headCommit
	s.ReviewedChangeHash = changeHash
}

// RecordGateResult adds a gate result and advances CurrentGate.
// If the gate was already passed, this is a no-op (prevents duplicate history
// entries from stop hook re-verification). A previously failed gate can be
// retried and will add a new entry.
func (s *TaskState) RecordGateResult(gateID string, passed bool, headCommit string) {
	// Skip if this gate was already passed — prevents 25x duplicate entries
	// from stop hook repeatedly verifying the same gate.
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

// gatePassed checks if a specific gate has passed.
func (s *TaskState) gatePassed(gateID string) bool {
	for _, r := range s.History {
		if r.Gate == gateID && r.Passed {
			return true
		}
	}
	return false
}

// CompletedGates returns a list of passed gate IDs.
func (s *TaskState) CompletedGates() []string {
	var result []string
	for _, g := range DefaultGates() {
		if s.gatePassed(g.ID) {
			result = append(result, g.ID)
		}
	}
	return result
}

// HasAcceptance reports whether the task has any persisted acceptance criteria.
func (s *TaskState) HasAcceptance() bool {
	return len(s.Acceptance) > 0
}

// AllAcceptancePassed reports whether every acceptance criterion has Passed=true.
// Empty acceptance returns true (nothing to reconcile). task-verify 据此决定是否提醒回扣。
func (s *TaskState) AllAcceptancePassed() bool {
	for i := range s.Acceptance {
		if !s.Acceptance[i].Passed {
			return false
		}
	}
	return true
}

// --- 接续真相源（continuity）方法 ---

// TaskKindGeneric 是 generic kind 的常量值。Kind 字段为空或 "code" 都走门禁（向后兼容
// 老 task 无 Kind 字段）；只有显式 "generic" 才不走门禁。
const TaskKindGeneric = "generic"

// IsGeneric 报告 task 是否为非门禁类型（Kind=="generic"）。generic task 承载调研/设计/纯
// 接续工作，不走 implement→verify→complete 门禁、complete 不评分。
func (s *TaskState) IsGeneric() bool { return s.Kind == TaskKindGeneric }

// HasContinuity 报告 task 是否携带任何接续内容（goal/plan/decisions/next/blockers/
// findings/artifacts 任一非空）。用于判断 resume 是否有结构化上下文可拉回。
func (s *TaskState) HasContinuity() bool {
	return s.Goal != "" || s.Plan != "" ||
		len(s.Decisions) > 0 || len(s.NextSteps) > 0 ||
		len(s.Blockers) > 0 || len(s.Findings) > 0 || len(s.Artifacts) > 0
}

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
	for _, l := range s.SessionLinks {
		if l.SessionID == sid {
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

// HasSession 报告 sid 是否已锚定到本 task。
func (s *TaskState) HasSession(sid string) bool {
	for _, l := range s.SessionLinks {
		if l.SessionID == sid {
			return true
		}
	}
	return false
}

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

// AddNext 追加一条下一步。
func (s *TaskState) AddNext(step string) {
	if step == "" {
		return
	}
	s.NextSteps = append(s.NextSteps, step)
}

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

// AddArtifact 追加一条产物引用。
func (s *TaskState) AddArtifact(a Artifact) {
	s.Artifacts = append(s.Artifacts, a)
}

// continuityCounter 保证同一进程内同一纳秒生成的 continuity ID 也不碰撞（Windows 时钟低精度 /
// 测试连续调用会让 UnixNano 相同）。resolve/resolveFinding 按 ID 精确命中，碰撞会让"解决
// 第二条却命中首条"——对 resolve 准确性是真实 bug，故加原子递增 seq 后缀。
//
// 但 seq 是进程内变量，跨 forge 进程各自从 0 开始；UnixNano 在 Windows 约 15ms 分辨率。两个
// 并行 forge 进程在 15ms 窗口内同 prefix 调用会拿到相同 nano + 相同 seq → 碰撞。故再加 4 字节
// crypto/rand 后缀彻底去碰撞（跨进程碰撞概率从 15ms 并行窗口降到 2^-32）。
var continuityCounter uint64

// newContinuityID 生成 continuity 实体（Decision/Blocker/Finding）的短唯一 ID：前缀 +
// UnixNano base36（时间序单调）+ 原子 seq（进程内同纳秒去重）+ 4 字节随机（跨进程去碰撞）。
func newContinuityID(prefix string) string {
	seq := atomic.AddUint64(&continuityCounter, 1)
	nano := strconv.FormatInt(time.Now().UnixNano(), 36)
	var b [4]byte
	if _, err := crand.Read(b[:]); err != nil {
		// crypto/rand 极少失败；退化时仍由 nano+seq 保证进程内唯一（跨进程理论碰撞窗口 15ms）。
		return prefix + nano + "-" + strconv.FormatUint(seq, 36)
	}
	return prefix + nano + "-" + strconv.FormatUint(seq, 36) + "-" + hex.EncodeToString(b[:])
}
