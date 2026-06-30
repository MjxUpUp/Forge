package mcpserver

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/experience"
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
