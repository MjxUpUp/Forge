package mcpserver

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/experience"
	"github.com/MjxUpUp/Forge/internal/health"
	"github.com/MjxUpUp/Forge/internal/knowledge"
	"github.com/MjxUpUp/Forge/internal/pipeline"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// 本文件是 7 个 MCP 工具的核心逻辑。每个工具有三部分：
//   - xxxInput / xxxOutput：typed schema struct（go-sdk 自动推断 JSON Schema）
//   - xxxCore：纯逻辑函数（接收已解析的 root），可独立单测
//   - handler 包装在 server.go，注入 root 解析 + 适配 mcp.AddTool 签名
//
// 工具直接复用 internal 包的公开函数，不 shell-out forge CLI——MCP 层是
// agent 可编程接口，应是最薄封装，避免文本解析的脆弱性。

// =====================================================================
// forge_gate_run —— 运行项目级管道门禁（pipeline.yml 定义）
// =====================================================================

type gateRunInput struct {
	GateID string `json:"gate_id" jsonschema:"门禁 id（pipeline.yml 定义，如 gate-1-prd）"`
	Force  bool   `json:"force,omitempty" jsonschema:"跳过前置条件检查"`
}

type gateRunOutput struct {
	Gate            string   `json:"gate"`
	Passed          bool     `json:"passed"`
	DurationSeconds float64  `json:"duration_seconds"`
	Errors          []string `json:"errors,omitempty"`
}

func gateRunCore(root string, in gateRunInput) (gateRunOutput, error) {
	p, err := pipeline.Load(root)
	if err != nil {
		return gateRunOutput{}, fmt.Errorf("load pipeline: %w", err)
	}
	gate, err := p.GetGate(in.GateID)
	if err != nil {
		return gateRunOutput{}, err
	}
	if !gate.Enabled {
		return gateRunOutput{}, fmt.Errorf("gate %q is disabled", in.GateID)
	}
	state, err := pipeline.LoadState(root)
	if err != nil {
		return gateRunOutput{}, fmt.Errorf("load state: %w", err)
	}
	res, err := pipeline.ExecuteGate(root, gate, state, p, in.Force)
	if err != nil {
		return gateRunOutput{}, err
	}
	if res.Status == nil {
		return gateRunOutput{}, fmt.Errorf("gate %q returned no status", in.GateID)
	}
	out := gateRunOutput{
		Gate:            in.GateID,
		Passed:          res.Status.Passed,
		DurationSeconds: res.Duration.Seconds(),
	}
	for _, e := range res.Status.Errors {
		out.Errors = append(out.Errors, e.Check+": "+e.Message)
	}
	return out, nil
}

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
// forge_experience_search —— 搜索 task 派生的经验提案（.forge/experience/）
// =====================================================================

type experienceSearchInput struct {
	Query  string `json:"query,omitempty" jsonschema:"关键词（匹配 title/description/patterns，空则全列）"`
	Status string `json:"status,omitempty" jsonschema:"状态过滤：proposed / accepted / rejected，空则全部"`
	Limit  int    `json:"limit,omitempty" jsonschema:"最多返回数（默认 20）"`
}

type experienceItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
	Status      string   `json:"status"`
	Description string   `json:"description"`
	Patterns    []string `json:"patterns,omitempty"`
}

type experienceSearchOutput struct {
	Results []experienceItem `json:"results"`
	Count   int              `json:"count"`
}

func experienceSearchCore(root string, in experienceSearchInput) (experienceSearchOutput, error) {
	props, err := experience.ListProposals(root, experience.PropStatus(in.Status))
	if err != nil {
		return experienceSearchOutput{}, fmt.Errorf("list proposals: %w", err)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	q := strings.ToLower(in.Query)
	out := experienceSearchOutput{}
	for _, p := range props {
		if q != "" && !proposalMatches(p, q) {
			continue
		}
		out.Results = append(out.Results, experienceItem{
			ID:          p.ID,
			Title:       p.Title,
			Category:    p.Category,
			Severity:    p.Severity,
			Status:      string(p.Status),
			Description: p.Description,
			Patterns:    p.Patterns,
		})
		if len(out.Results) >= limit {
			break
		}
	}
	out.Count = len(out.Results)
	return out, nil
}

// matchesText 是 proposalMatches / entryMatches 的共用子串匹配——两者字段相同
// （Title/Description/Patterns），只是承载结构不同，抽出来去重。
func matchesText(title, desc string, patterns []string, q string) bool {
	if strings.Contains(strings.ToLower(title), q) {
		return true
	}
	if strings.Contains(strings.ToLower(desc), q) {
		return true
	}
	for _, pat := range patterns {
		if strings.Contains(strings.ToLower(pat), q) {
			return true
		}
	}
	return false
}

func proposalMatches(p *experience.ExperienceProposal, q string) bool {
	return matchesText(p.Title, p.Description, p.Patterns, q)
}

// =====================================================================
// forge_experience_propose —— 提议新经验（写入 .forge/experience/proposed/）
// =====================================================================

type experienceProposeInput struct {
	Title       string   `json:"title" jsonschema:"经验标题"`
	Description string   `json:"description" jsonschema:"经验描述（踩了什么坑 / 什么模式 / 什么 API 用法）"`
	Category    string   `json:"category,omitempty" jsonschema:"类别：gotchas / patterns / apis（默认 gotchas）"`
	Severity    string   `json:"severity,omitempty" jsonschema:"error / warning / info（默认 warning）"`
	Patterns    []string `json:"patterns,omitempty" jsonschema:"正则模式（供 violation 扫描用）"`
}

type experienceProposeOutput struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func experienceProposeCore(root string, in experienceProposeInput) (experienceProposeOutput, error) {
	cat := in.Category
	if cat == "" {
		cat = "gotchas"
	}
	if !knowledge.ValidCategories[cat] {
		return experienceProposeOutput{}, fmt.Errorf("invalid category %q (valid: gotchas, patterns, apis)", cat)
	}
	sev := in.Severity
	if sev == "" {
		sev = "warning"
	}
	if in.Title == "" || in.Description == "" {
		return experienceProposeOutput{}, fmt.Errorf("title and description are required")
	}
	p := &experience.ExperienceProposal{
		Category:    cat,
		Title:       in.Title,
		Description: in.Description,
		Patterns:    in.Patterns,
		Severity:    sev,
		Status:      experience.PropProposed,
	}
	if err := experience.SaveProposal(root, p); err != nil {
		return experienceProposeOutput{}, fmt.Errorf("save proposal: %w", err)
	}
	return experienceProposeOutput{ID: p.ID, Status: string(p.Status)}, nil
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
	sort.Slice(evs, func(i, j int) bool { return evs[i].t.Before(evs[j].t) })
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
// forge_knowledge_lookup —— 跨项目知识库查询（~/.forge/knowledge/，全局）
// =====================================================================

type knowledgeLookupInput struct {
	Query    string `json:"query" jsonschema:"关键词（匹配 title/description/patterns）"`
	Category string `json:"category,omitempty" jsonschema:"gotchas / patterns / apis，空则全部"`
	Limit    int    `json:"limit,omitempty" jsonschema:"最多返回数（默认 20）"`
}

type knowledgeItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
	Description string   `json:"description"`
	Patterns    []string `json:"patterns,omitempty"`
}

type knowledgeLookupOutput struct {
	Results []knowledgeItem `json:"results"`
	Count   int             `json:"count"`
}

// knowledgeLookupCore 不接收 root——知识库是全局的（~/.forge/knowledge/），
// 跨项目共享，与项目位置无关。这是它和其余 6 个项目级工具的关键区别。
func knowledgeLookupCore(in knowledgeLookupInput) (knowledgeLookupOutput, error) {
	idx, err := knowledge.LoadIndex()
	if err != nil {
		return knowledgeLookupOutput{}, fmt.Errorf("load knowledge index: %w", err)
	}
	entries := idx.ListEntries(in.Category)
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	q := strings.ToLower(in.Query)
	out := knowledgeLookupOutput{}
	for _, e := range entries {
		if q != "" && !entryMatches(e, q) {
			continue
		}
		out.Results = append(out.Results, knowledgeItem{
			ID:          e.ID,
			Title:       e.Title,
			Category:    e.Category,
			Severity:    e.Severity,
			Description: e.Description,
			Patterns:    e.Patterns,
		})
		if len(out.Results) >= limit {
			break
		}
	}
	out.Count = len(out.Results)
	return out, nil
}

func entryMatches(e knowledge.Entry, q string) bool {
	return matchesText(e.Title, e.Description, e.Patterns, q)
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
	if in.Ref != "" {
		cs, err := act.LoadAll(root)
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
	c, err := act.Latest(root)
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
	cs, err := act.LoadAll(root)
	if err != nil {
		return healthQueryOutput{}, fmt.Errorf("load conclusions: %w", err)
	}
	// 无结论时 Summarize 返回零值 Summary（total_tasks=0）——有效输出，非错误。
	return health.Summarize(cs), nil
}
