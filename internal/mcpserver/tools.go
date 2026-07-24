package mcpserver

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/health"
	"github.com/MjxUpUp/Forge/internal/review"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
)

// 本文件是 7 个 MCP 工具的核心逻辑。每个工具有三部分：
//   - xxxInput / xxxOutput：typed schema struct（go-sdk 自动推断 JSON Schema）
//   - xxxCore：纯逻辑函数（接收已解析的 root），可独立单测
//   - handler 包装在 server.go，注入 root 解析 + 适配 mcp.AddTool 签名
//
// 工具直接复用 internal 包的公开函数，不 shell-out forge CLI——MCP 层是
// agent 可编程接口，应是最薄封装，避免文本解析的脆弱性。

// =====================================================================
// forge_task_status —— 查看任务状态（当前 session 活跃任务 或 指定 ref）
// =====================================================================

type taskStatusInput struct {
	Ref string `json:"ref,omitempty" jsonschema:"任务 ref；空则取当前 session 活跃任务"`
}

type taskStatusOutput struct {
	TaskRef        string   `json:"task_ref"`
	Branch         string   `json:"branch"`
	Summary        string   `json:"summary"`
	CurrentGate    string   `json:"current_gate"`
	CompletedGates []string `json:"completed_gates,omitempty"`
	NextGate       string   `json:"next_gate,omitempty"`
	IsComplete     bool     `json:"is_complete"`
}

func taskStatusCore(root string, in taskStatusInput) (taskStatusOutput, error) {
	state, err := loadTaskState(root, in.Ref)
	if err != nil {
		return taskStatusOutput{}, err
	}
	if state == nil {
		return taskStatusOutput{}, fmt.Errorf("no active task (run 'forge task start' first)")
	}
	return taskStatusOutput{
		TaskRef:        state.TaskRef,
		Branch:         state.Branch,
		Summary:        state.Summary,
		CurrentGate:    state.CurrentGate,
		CompletedGates: state.CompletedGates(),
		NextGate:       state.NextGate(),
		IsComplete:     state.IsComplete(),
	}, nil
}

// =====================================================================
// forge_task_gate —— 推进 task 级门禁（task-implement / task-verify / task-complete）
// =====================================================================

type taskGateInput struct {
	GateID string `json:"gate_id" jsonschema:"task 门禁 id：task-implement / task-verify / task-complete"`
	Ref    string `json:"ref,omitempty" jsonschema:"任务 ref；空则取活跃任务"`
}

type taskGateOutput struct {
	GateID     string `json:"gate_id"`
	GateName   string `json:"gate_name"`
	Passed     bool   `json:"passed"`
	Message    string `json:"message,omitempty"`
	IsComplete bool   `json:"is_complete"`
}

// taskGateCore 复刻 cli runTaskGate 的完整推进流程：执行检查 → 记录结果 →
// 解析 HEAD commit → 满完成则 MarkComplete → 持久化 state。
// ExecuteTaskGate 本身不保存 state（那是调用方职责），所以这里必须 SaveTaskState，
// 否则 MCP 推进的门禁不落盘、后续 forge task status 看不到。
func taskGateCore(root string, in taskGateInput) (taskGateOutput, error) {
	state, err := loadTaskState(root, in.Ref)
	if err != nil {
		return taskGateOutput{}, err
	}
	if state == nil {
		return taskGateOutput{}, fmt.Errorf("no active task (run 'forge task start' first)")
	}
	gate := taskpipeline.GateByID(in.GateID)
	if gate == nil {
		return taskGateOutput{}, fmt.Errorf("unknown task gate %q (valid: %s)", in.GateID, strings.Join(taskpipeline.GateIDs(), ", "))
	}

	result, err := taskpipeline.ExecuteTaskGate(root, in.GateID, state)
	if err != nil {
		return taskGateOutput{}, err
	}

	// 解析 HEAD（git -C root，与 runTaskGate 一致——避免子目录调用记错 commit）
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = root
	headCommit, _ := headCmd.Output()
	state.RecordGateResult(in.GateID, result.Passed, strings.TrimSpace(string(headCommit)))

	if state.IsComplete() && result.Passed {
		state.MarkComplete()
	}

	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return taskGateOutput{}, fmt.Errorf("save task state: %w", err)
	}

	// Token 成本熔断（advisory）：超阈值时把警示塞进 Message（MCP 无 stderr 给 agent，
	// Message 是 agent 可读的承载），与 CLI runTaskGate 的 stderr 警示对齐。
	msg := result.Message
	if w, _ := toolusage.TaskTokenBreaker(root, state.TaskRef); w != "" && msg == "" {
		msg = w
	}

	return taskGateOutput{
		GateID:     in.GateID,
		GateName:   gate.Name,
		Passed:     result.Passed,
		Message:    msg,
		IsComplete: state.IsComplete(),
	}, nil
}

// =====================================================================
// forge_task_resume —— 接续真相源入口：拉回任务完整接续上下文（结构化，供 agent parse）
// =====================================================================

type taskResumeInput struct {
	Ref           string `json:"ref,omitempty" jsonschema:"任务 ref；空则取当前 session 活跃任务"`
	NoAttach      bool   `json:"no_attach,omitempty" jsonschema:"仅读取不锚定当前 session；默认 false=接手即锚定，记录参与方"`
	AttachSession string `json:"attach_session,omitempty" jsonschema:"显式指定要锚定的 session ID（默认取环境当前 session）"`
}

type gateProgressStep struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // passed | current | pending
}

// taskResumeOutput 是接手方冷启动拉回的结构化上下文。与 CLI 的 HANDOFF 文本渲染同源，
// 但字段化以便 agent 直接 parse（goal/plan/decisions/next/blockers/findings/artifacts +
// 参与工具 + 门禁进度 + git 已改未提交 + 是否本次新锚定）。
type taskResumeOutput struct {
	TaskRef       string                  `json:"task_ref"`
	Branch        string                  `json:"branch"`
	Kind          string                  `json:"kind"`
	OriginTool    string                  `json:"origin_tool,omitempty"`
	Summary       string                  `json:"summary,omitempty"`
	Goal          string                  `json:"goal,omitempty"`
	Plan          string                  `json:"plan,omitempty"`
	Decisions     []taskpipeline.Decision `json:"decisions,omitempty"`
	NextSteps     []string                `json:"next_steps,omitempty"`
	Blockers      []taskpipeline.Blocker  `json:"blockers,omitempty"`
	Findings      []taskpipeline.Finding  `json:"findings,omitempty"`
	Artifacts     []taskpipeline.Artifact `json:"artifacts,omitempty"`
	SessionTools  []string                `json:"session_tools,omitempty"`
	GateProgress  []gateProgressStep      `json:"gate_progress"`
	GitChanged    []string                `json:"git_changed,omitempty"`
	ParentTaskRef string                  `json:"parent_task_ref,omitempty"`
	DependsOn     []string                `json:"depends_on,omitempty"`
	Anchored      bool                    `json:"anchored,omitempty"` // 本次 resume 是否新锚定了当前 session
}

func taskResumeCore(root string, in taskResumeInput) (taskResumeOutput, error) {
	state, err := loadTaskState(root, in.Ref)
	if err != nil {
		return taskResumeOutput{}, err
	}
	if state == nil {
		return taskResumeOutput{}, fmt.Errorf("no active task (run 'forge task start' first)")
	}
	anchored := false
	if !in.NoAttach {
		sid := in.AttachSession
		if sid == "" {
			sid = taskpipeline.CurrentSessionID()
		}
		// tool 探测失败时跳过锚定（不回退 OriginTool——会把接手方 session 错误归属到创建方
		// 工具）。resume 永远返回上下文，锚定是附加动作；anchored=false 让 agent 知未锚定。
		tool := detectTool("")
		if sid != "" && tool != "" && !state.HasSession(sid) {
			state.AddSession(sid, tool)
			if err := taskpipeline.SaveTaskState(root, state); err != nil {
				return taskResumeOutput{}, fmt.Errorf("锚定 session 失败: %w", err)
			}
			anchored = true
		}
	}
	kind := state.Kind
	if kind == "" {
		kind = "code"
	}
	return taskResumeOutput{
		TaskRef:       state.TaskRef,
		Branch:        state.Branch,
		Kind:          kind,
		OriginTool:    state.OriginTool,
		Summary:       state.Summary,
		Goal:          state.Goal,
		Plan:          state.Plan,
		Decisions:     state.Decisions,
		NextSteps:     state.NextSteps,
		Blockers:      state.Blockers,
		Findings:      state.Findings,
		Artifacts:     state.Artifacts,
		SessionTools:  state.SessionTools(),
		GateProgress:  buildGateProgress(state),
		GitChanged:    gitStatusPorcelain(root),
		ParentTaskRef: state.ParentTaskRef,
		DependsOn:     state.DependsOn,
		Anchored:      anchored,
	}, nil
}

// =====================================================================
// forge_task_decide —— 记录已确认决策（持久化，跨会话/跨工具不再推翻）
// =====================================================================

type taskDecideInput struct {
	Ref       string   `json:"ref,omitempty" jsonschema:"任务 ref；空则取当前 session 活跃任务"`
	Content   string   `json:"content" jsonschema:"决策内容（必填）"`
	By        string   `json:"by,omitempty" jsonschema:"确认方（工具/人，如 [pi]/[claude-code]）"`
	Affects   []string `json:"affects,omitempty" jsonschema:"影响的文件/模块"`
	Rationale string   `json:"rationale,omitempty" jsonschema:"为什么这么决定"`
}

type taskDecideOutput struct {
	DecisionID     string `json:"decision_id"`
	Content        string `json:"content"`
	TotalDecisions int    `json:"total_decisions"`
}

func taskDecideCore(root string, in taskDecideInput) (taskDecideOutput, error) {
	if in.Content == "" {
		return taskDecideOutput{}, fmt.Errorf("content is required")
	}
	state, err := loadTaskState(root, in.Ref)
	if err != nil {
		return taskDecideOutput{}, err
	}
	if state == nil {
		return taskDecideOutput{}, fmt.Errorf("no active task (run 'forge task start' first)")
	}
	state.AddDecision(taskpipeline.Decision{
		Content:   in.Content,
		By:        in.By,
		Affects:   in.Affects,
		Rationale: in.Rationale,
	})
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return taskDecideOutput{}, err
	}
	d := state.Decisions[len(state.Decisions)-1]
	return taskDecideOutput{
		DecisionID:     d.ID,
		Content:        d.Content,
		TotalDecisions: len(state.Decisions),
	}, nil
}

// =====================================================================
// forge_task_attach —— 把 session+工具锚定到 task（跨工具接续的多向锚定）
// =====================================================================

type taskAttachInput struct {
	Ref       string `json:"ref" jsonschema:"任务 ref（必填：要锚定到哪个任务）"`
	Tool      string `json:"tool,omitempty" jsonschema:"该 session 所属工具（pi/claude-code/opencode…，默认探测当前）"`
	SessionID string `json:"session_id,omitempty" jsonschema:"要锚定的 session ID（默认取环境当前 session）"`
}

type taskAttachOutput struct {
	SessionID       string `json:"session_id"`
	Tool            string `json:"tool"`
	TotalSessions   int    `json:"total_sessions"`
	AlreadyAnchored bool   `json:"already_anchored"`
}

func taskAttachCore(root string, in taskAttachInput) (taskAttachOutput, error) {
	if in.Ref == "" {
		return taskAttachOutput{}, fmt.Errorf("ref is required")
	}
	state, err := taskpipeline.LoadTaskState(root, in.Ref)
	if err != nil {
		return taskAttachOutput{}, err
	}
	if state == nil {
		return taskAttachOutput{}, fmt.Errorf("task %q not found", in.Ref)
	}
	sid := in.SessionID
	if sid == "" {
		sid = taskpipeline.CurrentSessionID()
	}
	if sid == "" {
		return taskAttachOutput{}, fmt.Errorf("cannot determine session id (set session_id or run under an agent session)")
	}
	tool := detectTool(in.Tool)
	if tool == "" {
		return taskAttachOutput{}, fmt.Errorf(`cannot determine tool: set tool or run under an agent env. Cross-tool attach requires an explicit tool to avoid misattribution`)
	}
	already := state.HasSession(sid)
	state.AddSession(sid, tool)
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return taskAttachOutput{}, err
	}
	return taskAttachOutput{
		SessionID:       sid,
		Tool:            tool,
		TotalSessions:   len(state.SessionLinks),
		AlreadyAnchored: already,
	}, nil
}

// =====================================================================
// forge_trace_query —— 查询任务的完整质量事件时间线（checklog + toolusage）
// =====================================================================

type traceQueryInput struct {
	Ref string `json:"ref" jsonschema:"任务 ref"`
}

type traceEvent struct {
	Time    string `json:"time"`
	Source  string `json:"source"` // "check" or "tool"
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type traceQueryOutput struct {
	Ref       string       `json:"ref"`
	Events    []traceEvent `json:"events"`
	Checks    int          `json:"checks"`
	ToolCalls int          `json:"tool_calls"`
	EstTokens int          `json:"est_tokens"`
}

func traceQueryCore(root string, in traceQueryInput) (traceQueryOutput, error) {
	checks, err := checklog.LoadForTask(root, in.Ref)
	if err != nil {
		return traceQueryOutput{}, fmt.Errorf("load checklog: %w", err)
	}
	calls, err := toolusage.LoadForTaskAll(root, in.Ref)
	if err != nil {
		return traceQueryOutput{}, fmt.Errorf("load toollog: %w", err)
	}
	out := traceQueryOutput{
		Ref:       in.Ref,
		Checks:    len(checks),
		ToolCalls: len(calls),
		EstTokens: toolusage.SumEstTokens(calls),
	}

	type rawEv struct {
		t  time.Time
		ev traceEvent
	}
	var evs []rawEv
	for _, c := range checks {
		mark := "✗"
		if c.Passed {
			mark = "✓"
		}
		evs = append(evs, rawEv{c.RecordedAt, traceEvent{
			Time:    c.RecordedAt.Format("15:04:05"),
			Source:  "check",
			Summary: fmt.Sprintf("[%s] %s — %s", mark, c.Check, c.ToolName),
			Detail:  truncateDetail(c.Detail),
		}})
	}
	for _, c := range calls {
		evs = append(evs, rawEv{c.Timestamp, traceEvent{
			Time:    c.Timestamp.Format("15:04:05"),
			Source:  "tool",
			Summary: fmt.Sprintf("→ %s", c.ToolName),
		}})
	}
	slices.SortFunc(evs, func(a, b rawEv) int { return a.t.Compare(b.t) })
	for _, e := range evs {
		out.Events = append(out.Events, e.ev)
	}
	return out, nil
}

func truncateDetail(s string) string {
	const max = 200
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// =====================================================================
// 共用辅助
// =====================================================================

// loadTaskState 解析 task state：显式 ref 优先，否则取当前 session 活跃任务。
// 抽出来让 task_status / task_gate 共用同一解析逻辑（含 nil 语义）。
func loadTaskState(root, ref string) (*taskpipeline.TaskState, error) {
	if ref != "" {
		return taskpipeline.LoadTaskState(root, ref)
	}
	return taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
}

// detectTool 探测当前 agent 工具（与 cli.detectOriginTool 同逻辑；MCP server 进程也继承
// agent 注入的 CLAUDE_CODE_SESSION_ID）。explicit 非空则用之。
func detectTool(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return "claude-code"
	}
	return ""
}

// buildGateProgress 把 task History 投影成门禁进度（passed/current/pending）。
func buildGateProgress(state *taskpipeline.TaskState) []gateProgressStep {
	var steps []gateProgressStep
	for _, g := range taskpipeline.DefaultGates() {
		status := "pending"
		for _, r := range state.History {
			if r.Gate == g.ID && r.Passed {
				status = "passed"
				break
			}
		}
		if status != "passed" && state.CurrentGate == g.ID {
			status = "current"
		}
		steps = append(steps, gateProgressStep{ID: g.ID, Name: g.Name, Status: status})
	}
	return steps
}

// gitStatusPorcelain 返回 git status --porcelain 行（已改未提交）。失败返 nil——resume 不依赖 git。
func gitStatusPorcelain(root string) []string {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// =====================================================================
// forge_act_query —— 查询任务结论（证据强度/score/验收/低分维度）+ 回顾指令
// =====================================================================

type actQueryInput struct {
	Ref string `json:"ref,omitempty" jsonschema:"任务 ref（省略=最新完成的结论）"`
}

// actQueryOutput 嵌入 act.Conclusion 并补 Directive（Conclusion 的方法，序列化要字段）。
// 让 agent 在 loop 里读"这次完成声明有多少 deterministic 证据"——对冲 LLM-judge 看不出
// agent 跳过前置就声明完成的盲区（这是 Act 反馈臂的 agent 可编程读端）。
type actQueryOutput struct {
	act.Conclusion
	Directive string `json:"directive,omitempty"`
}

func actQueryCore(root string, in actQueryInput) (actQueryOutput, error) {
	proj, err := forgedata.ProjectFor(root)
	if err != nil {
		return actQueryOutput{}, fmt.Errorf("resolve project: %w", err)
	}
	if in.Ref != "" {
		cs, err := act.LoadAll(proj)
		if err != nil {
			return actQueryOutput{}, fmt.Errorf("load conclusions: %w", err)
		}
		var found *act.Conclusion
		for i := range cs {
			if cs[i].TaskRef == in.Ref {
				found = &cs[i] // 多次完成取最新（最后一个匹配）
			}
		}
		if found == nil {
			return actQueryOutput{}, fmt.Errorf("no act conclusion for task %q", in.Ref)
		}
		return actQueryOutput{Conclusion: *found, Directive: found.Directive()}, nil
	}
	c, err := act.Latest(proj)
	if err != nil {
		return actQueryOutput{}, fmt.Errorf("load latest conclusion: %w", err)
	}
	if c == nil {
		return actQueryOutput{}, fmt.Errorf("no act conclusions yet (forge task complete 产出)")
	}
	return actQueryOutput{Conclusion: *c, Directive: c.Directive()}, nil
}

// =====================================================================
// forge_health_query —— 项目级质量趋势上卷（task→project 粒度）
// =====================================================================

type healthQueryInput struct {
	// 无参数：项目级聚合，不依赖单个 ref。空 struct 让 go-sdk 推断"无需参数"。
}

// healthQueryOutput 直接是 health.Summary（已带 json tag）。盲区率 blind_spot_rate 是头条
// 信号：完成声明主要靠 agent 自述（Unverified/Weak）的任务占比——项目级 LLM-judge 盲区率。
type healthQueryOutput = health.Summary

func healthQueryCore(root string, _ healthQueryInput) (healthQueryOutput, error) {
	proj, err := forgedata.ProjectFor(root)
	if err != nil {
		return healthQueryOutput{}, nil // 非 forge 项目：合法空状态，返零值 Summary（total=0）
	}
	cs, err := act.LoadAll(proj)
	if err != nil {
		return healthQueryOutput{}, fmt.Errorf("load conclusions: %w", err)
	}
	// 无结论时 Summarize 返回零值 Summary（total_tasks=0）——有效输出，非错误。
	return health.Summarize(cs), nil
}

// =====================================================================
// forge_task_start —— 启动 Forge 任务（proof-of-work 闭环入口）
// =====================================================================

type taskStartInput struct {
	Ref        string   `json:"ref,omitempty" jsonschema:"任务 ref（建议 feat/xxx）；空则从当前 branch 推断"`
	Title      string   `json:"title,omitempty" jsonschema:"任务标题"`
	Kind       string   `json:"kind,omitempty" jsonschema:"任务类型：code（默认，走 3 道门禁）| generic（不走门禁，complete 不评分）"`
	Goal       string   `json:"goal,omitempty" jsonschema:"目标叙述（为什么做，可多行）"`
	Plan       string   `json:"plan,omitempty" jsonschema:"计划正文 markdown（自动提取 Run:/Expected: 验收标准）"`
	Accept     []string `json:"accept,omitempty" jsonschema:"显式验收标准（格式 run :: expected），优先于 Plan 提取"`
	Scope      []string `json:"scope,omitempty" jsonschema:"计划改动文件白名单（精确路径/glob/目录前缀）"`
	FromIssue  string   `json:"from_issue,omitempty" jsonschema:"外部 issue URL（linear/github），解析为 ExternalOrigin 锚定外部 issue"`
	OriginTool string   `json:"origin_tool,omitempty" jsonschema:"发起工具显式声明（pi/codex/cursor 等）；空则从 CLAUDE_CODE_SESSION_ID 探测。spawn 式非 claude-code 编排器（不注入该 env）用它声明 origin，否则 SessionLink 缺发起工具信号"`
}

type taskStartOutput struct {
	TaskRef         string                      `json:"task_ref"`
	Branch          string                      `json:"branch"`
	Summary         string                      `json:"summary"`
	Kind            string                      `json:"kind"`
	ExternalOrigin  taskpipeline.ExternalOrigin `json:"external_origin,omitempty"`
	AcceptanceCount int                         `json:"acceptance_count"`
	Created         bool                        `json:"created"`
}

// taskStartCore 复刻 cli runTaskStart 的核心：组装 TaskState 并持久化。不创建 git branch
// （--branch 是 CLI 交互便利；spawn 式编排器用 per-issue workspace，自管 checkout，MCP 只登记 task）。
// MCP 层是最薄封装：直接调 taskpipeline/taskcontext 原语，不 shell-out forge CLI——避免文本解析脆弱性。
func taskStartCore(root string, in taskStartInput) (taskStartOutput, error) {
	detected := taskcontext.Detect(root)
	taskRef := in.Ref
	if taskRef == "" {
		taskRef = detected.TaskRef
	}
	if taskRef == "" {
		return taskStartOutput{}, fmt.Errorf(`ref is required (no task context on current branch; set ref explicitly)`)
	}
	if existing, _ := taskpipeline.LoadTaskState(root, taskRef); existing != nil {
		return taskStartOutput{}, fmt.Errorf(`task %q already exists (started at %s)`, taskRef, existing.StartedAt.Format(`2006-01-02 15:04`))
	}
	ctx := &taskcontext.Context{
		Source:     "explicit",
		TaskRef:    taskRef,
		Branch:     detected.Branch,
		Summary:    in.Title,
		DetectedAt: detected.DetectedAt,
	}
	state := taskpipeline.NewTaskState(ctx)
	state.HeadCommit = taskpipeline.GetHeadCommit(root)

	if in.Accept != nil {
		state.Acceptance = taskpipeline.ParseAcceptance(in.Accept)
	}
	if in.Scope != nil {
		state.PlanScope = in.Scope
	}
	if in.Kind != "" {
		state.Kind = in.Kind
	}
	if in.Goal != "" {
		state.Goal = in.Goal
	}
	if in.Plan != "" {
		state.Plan = in.Plan
		if extracted := taskpipeline.ParseAcceptanceFromPlan(in.Plan); len(extracted) > 0 {
			state.Acceptance = taskpipeline.MergeAcceptance(state.Acceptance, extracted)
		}
	}
	if in.FromIssue != "" {
		state.ExternalOrigin = taskpipeline.ParseExternalOriginURL(in.FromIssue)
	}

	// OriginTool：声明式发起工具（与 cli.detectOriginTool 同源）。spawn 式编排器靠 MCP 起 task，
	// state.OriginTool 是它知道“这任务谁起的”的唯一声明式来源——必须在 AddSession 前赋值，
	// AddSession 的 agentType 参数用 state.OriginTool 记 SessionLink 发起方（对齐 CLI：先赋 OriginTool
	// 再 AddSession(state.SessionID, state.OriginTool)）。漏赋则 SessionLink 缺发起工具信号。
	state.OriginTool = detectTool(in.OriginTool)
	// 创建方 session 锚定（多向锚定起点；与 CLI runTaskStart 一致——EnsureSession 赋 SessionID 后再 AddSession）。
	sid := taskpipeline.CurrentSessionID()
	if session, err := taskpipeline.EnsureSession(root, sid); err == nil {
		state.SessionID = session.SessionID
		if state.SessionID != "" {
			state.AddSession(state.SessionID, state.OriginTool)
		}
	}

	// fresh task：清旧 checklog/toollog，避免新任务继承上一任务的证据（与 CLI 一致）。
	checklog.Clear(root)
	toolusage.Clear(root)

	// Prune completed task state files older than the retention window（与 CLI runTaskStart 一致），
	// 保持 DataDir/tasks/ 有界。同一 retention 窗口让 task metadata 与其 logs 同步过期。best-effort。
	if days := util.RetentionDays("FORGE_LOG_RETENTION_DAYS", 30); days > 0 {
		taskpipeline.PruneOldTasks(root, time.Now().AddDate(0, 0, -days))
	}

	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return taskStartOutput{}, fmt.Errorf(`save task state: %w`, err)
	}
	if err := taskpipeline.SetActiveTaskRef(root, sid, state.TaskRef); err != nil {
		// active-task-ref 失败非致命（state 已落盘），但 agent 后续 task status/gate 默认按 active
		// 推断会找不到——返回 error 让 agent 知道。
		return taskStartOutput{}, fmt.Errorf(`task created but failed to set active ref: %w`, err)
	}

	kind := state.Kind
	if kind == "" {
		kind = "code"
	}
	return taskStartOutput{
		TaskRef:         state.TaskRef,
		Branch:          state.Branch,
		Summary:         state.Summary,
		Kind:            kind,
		ExternalOrigin:  state.ExternalOrigin,
		AcceptanceCount: len(state.Acceptance),
		Created:         true,
	}, nil
}

// =====================================================================
// forge_task_proof —— proof-of-work 断言（task-complete pre-flight dry-run）
// =====================================================================

type taskProofInput struct {
	Ref string `json:"ref,omitempty" jsonschema:"任务 ref；空则取当前 session 活跃任务"`
}

type proofAcceptanceItem struct {
	Run    string `json:"run"`
	Passed bool   `json:"passed"`
	Fresh  bool   `json:"fresh"`            // v2 快路径命中：AcceptedHeadCommit == 当前 HEAD（信任快照，不重跑）
	Output string `json:"output,omitempty"` // 重跑输出或快照 Output（截断），供排查
}

type taskProofOutput struct {
	TaskRef     string                `json:"task_ref"`
	Kind        string                `json:"kind"`
	Done        bool                  `json:"done"`             // IsComplete AND 无 review drift AND acceptance 全过
	Reason      string                `json:"reason,omitempty"` // done=false 时的人话原因（指向下一步）
	IsComplete  bool                  `json:"is_complete"`      // 三门禁是否全过（generic 恒 true）
	ReviewDrift bool                  `json:"review_drift"`     // 审查通过后是否又改码
	Acceptance  []proofAcceptanceItem `json:"acceptance,omitempty"`
}

// taskProofCore 是 task-complete 门禁的 pre-flight dry-run（read-only）：不评分、不清 task、不写
// state。done = IsComplete AND review 未漂移 AND acceptance 全过。agent 在 forge_task_complete 前
// 调一次，预判会不会被 BLOCKED——把"声明 done"从 agent 自述变成 deterministic 断言。
//
// acceptance 双轨：
//   - v2 快路径：AcceptedHeadCommit == 当前 HEAD → 信任 verify-acceptance 实跑快照（Passed），不重跑。
//   - v1 重跑兜底：快照空/过期（!= HEAD）→ RunTestCommand 只读重跑判 passed。
//
// proof 不调 VerifyAcceptance（它会回写 state）——重跑用 RunTestCommand 只读判，磁盘零写。
// review drift fail-open（基线不可达算 amend/rebase，不算 drift），对齐 executor task-complete。
func taskProofCore(root string, in taskProofInput) (taskProofOutput, error) {
	state, err := loadTaskState(root, in.Ref)
	if err != nil {
		return taskProofOutput{}, err
	}
	if state == nil {
		return taskProofOutput{}, fmt.Errorf(`no active task (run 'forge task start' first)`)
	}

	kind := state.Kind
	if kind == "" {
		kind = "code"
	}
	out := taskProofOutput{TaskRef: state.TaskRef, Kind: kind}

	// acceptance 双轨（generic/code 都要）：先算每条，结果进 out.Acceptance + allPassed。
	head := taskpipeline.GetHeadCommit(root)
	allPassed := true
	for i := range state.Acceptance {
		c := state.Acceptance[i]
		item := proofAcceptanceItem{Run: c.Run}
		if head != "" && c.AcceptedHeadCommit == head {
			// v2 快路径：快照新鲜，信任 c.Passed（verify-acceptance 实跑结果），不重跑。
			item.Fresh = true
			item.Passed = c.Passed
			item.Output = c.Output
		} else {
			// v1 重跑兜底：快照空/过期 → 只读重跑判 passed。JudgeAcceptance 与 VerifyAcceptance 同源
			// 三态判定（历史 bug：曾只判退出码漏 Expected 子串 → 命令退出 0 但输出不含 Expected 时
			// 假绿 acceptance，击穿 proof 主张）。RunTestCommand 不回写 state，输出同源截断。
			item.Fresh = false
			ok, runOut := taskpipeline.RunTestCommand(root, c.Run)
			item.Passed = taskpipeline.JudgeAcceptance(ok, runOut, c.Expected)
			item.Output = taskpipeline.TruncateAcceptanceOutput(runOut)
		}
		if !item.Passed {
			allPassed = false
		}
		out.Acceptance = append(out.Acceptance, item)
	}

	done := true
	var reason string

	if kind == taskpipeline.TaskKindGeneric {
		// generic 不走门禁/review：done = acceptance 全过（空 acceptance 视为过）。
		// IsComplete 对 generic 无意义（无门禁 history），记 true 反映"无门禁阻断"。
		out.IsComplete = true
		if state.HasAcceptance() && !allPassed {
			done = false
			reason = `验收未全过（generic 任务，详见 acceptance）`
		}
	} else {
		out.IsComplete = state.IsComplete()
		if !out.IsComplete {
			done = false
			if next := state.NextGate(); next != "" {
				reason = fmt.Sprintf(`门禁未全过，下一道：%s`, next)
			} else {
				reason = `门禁未全过`
			}
		}
		// review drift：仅 ReviewPassed 时检查（对齐 executor task-complete fail-open 哲学）。
		if state.ReviewPassed && state.ReviewedHeadCommit != "" {
			if cur, _, derr := review.SourceChangesSince(root, state.ReviewedHeadCommit); derr == nil && cur != state.ReviewedChangeHash {
				out.ReviewDrift = true
				done = false
				reason = `审查通过后检测到源码变更（review drift）——重新派只读子 agent 审查后 forge review pass 刷新基线`
			}
			// derr != nil → fail-open（基线不可达，amend/rebase），不算 drift，对齐 executor。
		}
		if state.HasAcceptance() && !allPassed {
			done = false
			if reason == "" {
				reason = `验收未全过（详见 acceptance）`
			}
		}
	}

	out.Done = done
	out.Reason = reason
	return out, nil
}

// =====================================================================
// forge_task_complete —— 任务完成终点（评分 + Act + 清 active）。proof-of-work 闭环收口。
// =====================================================================

type taskCompleteInput struct {
	Ref string `json:"ref" jsonschema:"任务 ref（必填——MCP complete 强制显式 ref，避免误清非预期 active task）"`
}

type taskCompleteOutput struct {
	TaskRef      string   `json:"task_ref"`
	Kind         string   `json:"kind"`
	Completed    bool     `json:"completed"`
	IsComplete   bool     `json:"is_complete"` // 三门禁是否全过（complete 时刻断言）
	HasScore     bool     `json:"has_score"`   // 是否评分成功（generic / 评分失败时 false）
	ScoreOverall float64  `json:"score_overall,omitempty"`
	ScoreGrade   string   `json:"score_grade,omitempty"`
	Directive    string   `json:"directive,omitempty"` // Act 回顾指令（非空 = 有 RetrospectiveNudge）
	Warnings     []string `json:"warnings,omitempty"`  // 评分/Act/active-clear 失败等 warning（agent 可读）
}

// taskCompleteCore 复刻 cli runTaskComplete 的核心：评分 + Act 结论落盘 + 清 active task ref +
// post-complete grace。MCP 层最薄封装——调 taskpipeline 原语（ScoreTask/AppendConclusion 下沉后与
// CLI 共用同一真相源），不 shell-out forge CLI（避免文本解析脆弱）。
//
// ref 强制（与 start/proof 可空不同）：complete 是不可逆收口（清 active + 落 Score + 写 Act），显式
// ref 防止 spawn 式编排器误清非预期 active task。generic（调研/设计/接续）跳过门禁与评分，标 3 门禁
// passed + MarkComplete + 清 active（与 cli completeGenericTask 同语义）。code path 要求 IsComplete
// （三门禁全过），否则 error BLOCKED（带 missingGates 人话）——agent 应先 forge_task_proof 预判，
// 被 BLOCKED 即返工，不在 complete 里硬塞门禁。
func taskCompleteCore(root string, in taskCompleteInput) (taskCompleteOutput, error) {
	if in.Ref == "" {
		return taskCompleteOutput{}, fmt.Errorf(`ref is required (forge_task_complete mandates explicit ref — clearing an active task is irreversible)`)
	}
	state, err := taskpipeline.LoadTaskState(root, in.Ref)
	if err != nil {
		return taskCompleteOutput{}, err
	}
	if state == nil {
		return taskCompleteOutput{}, fmt.Errorf(`no task %q (run 'forge_task_start' first)`, in.Ref)
	}

	kind := state.Kind
	if kind == "" {
		kind = "code"
	}
	out := taskCompleteOutput{TaskRef: state.TaskRef, Kind: kind}
	var warnings []string

	if state.IsGeneric() {
		// generic：标 3 门禁 passed（History 完整供 list/dashboard）+ MarkComplete + 清 active。
		head := taskpipeline.GetHeadCommit(root)
		for _, g := range taskpipeline.DefaultGates() {
			state.RecordGateResult(g.ID, true, head)
		}
		state.MarkComplete()
		if err := taskpipeline.SaveTaskState(root, state); err != nil {
			return taskCompleteOutput{}, fmt.Errorf(`save task state: %w`, err)
		}
		if err := taskpipeline.ClearActiveTaskRef(root, taskpipeline.CurrentSessionID()); err != nil {
			warnings = append(warnings, fmt.Sprintf(`failed to clear active task ref: %v`, err))
		}
		out.IsComplete = true
		out.Completed = true
		out.Warnings = warnings
		return out, nil
	}

	// code path：门禁全过才允许 complete（与 cli runTaskComplete 一致——IsComplete 依赖 task-complete
	// 门禁 history，gate handler 已在通过时 MarkComplete 设好 CompletedAt）。
	out.IsComplete = state.IsComplete()
	if !out.IsComplete {
		return out, fmt.Errorf(`task not complete. Missing gates: %s`, missingTaskGates(state))
	}

	// 评分（落盘 Score）。失败非致命——warning，仍继续（Act 证据强度不依赖分数）。
	if err := taskpipeline.ScoreTask(root, state); err != nil {
		warnings = append(warnings, fmt.Sprintf(`scoring failed: %v`, err))
	}

	// Act 反馈臂：构建证据驱动结论落盘。directive 非空 = 有 RetrospectiveNudge。
	_, directive, cerr := taskpipeline.AppendConclusion(root, state)
	if cerr != nil {
		warnings = append(warnings, cerr.Error())
	}
	out.Directive = directive
	if state.Score != nil {
		out.HasScore = true
		out.ScoreOverall = state.Score.Overall
		out.ScoreGrade = state.Score.Grade
	}

	// 清 active task ref（session-scoped）+ post-complete grace sentinel（让紧随的 git commit 不被
	// file-sentinel quarantine 为"无 active task + 源码写"，与 cli runTaskComplete 同序）。
	if err := taskpipeline.ClearActiveTaskRef(root, taskpipeline.CurrentSessionID()); err != nil {
		warnings = append(warnings, fmt.Sprintf(`failed to clear active task ref: %v`, err))
	}
	if err := taskpipeline.MarkCompleteGrace(root, taskpipeline.CurrentSessionID()); err != nil {
		warnings = append(warnings, fmt.Sprintf(`failed to mark complete grace: %v`, err))
	}

	out.Completed = true
	out.Warnings = warnings
	return out, nil
}

// missingTaskGates 复刻 cli.missingGates（MCP 不依赖 cli 包）——用公开 CompletedGates +
// DefaultGates 投影未过门禁 ID。complete BLOCKED 时塞进 error 人话，指向 agent 下一步。
func missingTaskGates(state *taskpipeline.TaskState) string {
	completedMap := make(map[string]bool)
	for _, id := range state.CompletedGates() {
		completedMap[id] = true
	}
	var missing []string
	for _, g := range taskpipeline.DefaultGates() {
		if !completedMap[g.ID] {
			missing = append(missing, g.ID)
		}
	}
	return strings.Join(missing, ", ")
}
