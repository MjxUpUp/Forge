package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startTestServer 起一对 in-memory transport 连接的 server+client session。
// 端到端测 MCP 协议层（注册/调用/序列化），证明工具真能通过 MCP 调到——
// 而非只测 core 函数（那不能证明 AddTool handler 签名/注册正确）。
func startTestServer(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := New("test")
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestServer_ListToolsRegistersAll：10 个工具必须都注册到 MCP server。
// 协议层契约——漏注册一个，agent 就调不到对应能力；多注册说明有 stray。
func TestServer_ListToolsRegistersAll(t *testing.T) {
	cs := startTestServer(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	want := []string{
		"forge_gate_run", "forge_task_status", "forge_task_gate",
		"forge_experience_search", "forge_experience_propose",
		"forge_trace_query", "forge_act_query", "forge_health_query",
		"forge_knowledge_lookup",
		"forge_skill_eval_cases", "forge_skill_eval_submit", "forge_skill_eval_report",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("工具 %q 未注册（已注册: %v）", w, got)
		}
	}
	if len(res.Tools) != len(want) {
		t.Errorf("工具数 = %d，want %d", len(res.Tools), len(want))
	}
}

// TestServer_CallKnowledgeLookup_EmptyHome：端到端 CallTool——knowledge_lookup
// 在空知识库（隔离 home）下返回 count=0，证明 handler→core→MCP JSON 序列化链路通。
// 选 knowledge_lookup 做端到端是因为它不依赖项目根（cwd 无关），可隔离 home 干净测。
func TestServer_CallKnowledgeLookup_EmptyHome(t *testing.T) {
	// 隔离 home：Linux 的 os.UserHomeDir 读 HOME、Windows 读 USERPROFILE，双设才跨平台干净。
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	cs := startTestServer(t)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forge_knowledge_lookup",
		Arguments: map[string]any{"query": "anything"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("knowledge_lookup IsError=true（空库应返回 count:0）: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("无 content 返回")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] 不是 TextContent: %T", res.Content[0])
	}
	var out knowledgeLookupOutput
	if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
		t.Fatalf("反序列化失败 err=%v（text=%q）", err, tc.Text)
	}
	if out.Count != 0 {
		t.Errorf("Count=%d want 0（隔离 home 的空知识库）", out.Count)
	}
}

// =====================================================================
// core 单元测试（temp root 注入，覆盖逻辑正确性 + 错误路径）
// =====================================================================

// TestExperiencePropose_ThenSearch：经验闭环——propose 写入后 search 能命中。
// loop engineering 经验沉淀的核心往返；守护 propose 的 ID 生成 + search 的关键词匹配。
func TestExperiencePropose_ThenSearch(t *testing.T) {
	root := t.TempDir()
	out, err := experienceProposeCore(root, experienceProposeInput{
		Title:       "测试经验标题",
		Description: "这是一个测试描述",
		Category:    "gotchas",
		Severity:    "warning",
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if out.ID == "" {
		t.Fatal("propose 未生成 ID")
	}
	if out.Status != "proposed" {
		t.Errorf("Status=%q want proposed", out.Status)
	}

	res, err := experienceSearchCore(root, experienceSearchInput{Query: "测试"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count=%d want 1（proposed 应被 search 命中）: %+v", res.Count, res.Results)
	}
	if res.Results[0].ID != out.ID {
		t.Errorf("命中 ID=%q want %q", res.Results[0].ID, out.ID)
	}
}

// TestExperiencePropose_InvalidCategory：非法 category 报错——防 agent 把垃圾知识
// 写进知识库（category 是知识分类的骨架，必须守 gotchas/patterns/apis）。
func TestExperiencePropose_InvalidCategory(t *testing.T) {
	root := t.TempDir()
	_, err := experienceProposeCore(root, experienceProposeInput{
		Title: "x", Description: "y", Category: "bogus",
	})
	if err == nil {
		t.Fatal("非法 category 应报错")
	}
}

// TestTraceQuery_AggregatesEventsAndTokens：注入 1 条 checklog + 1 条 toollog，
// trace 应聚合为 2 个 event + 累计 est_tokens。联合 token 计量（#3）与 trace 的契约。
func TestTraceQuery_AggregatesEventsAndTokens(t *testing.T) {
	root := t.TempDir()
	if err := checklog.Record(root, &checklog.Entry{
		Check: checklog.CheckAutoCompile, Passed: true, ToolName: "Edit",
		TaskRef: "feat/x", Detail: "compiled OK",
	}); err != nil {
		t.Fatalf("checklog.Record: %v", err)
	}
	if err := toolusage.Record(root, &toolusage.ToolCall{
		ToolName: "Edit", TaskRef: "feat/x", EstTokens: 42,
	}); err != nil {
		t.Fatalf("toolusage.Record: %v", err)
	}

	out, err := traceQueryCore(root, traceQueryInput{Ref: "feat/x"})
	if err != nil {
		t.Fatalf("traceQuery: %v", err)
	}
	if out.Checks != 1 || out.ToolCalls != 1 {
		t.Errorf("Checks=%d ToolCalls=%d want 1/1", out.Checks, out.ToolCalls)
	}
	if len(out.Events) != 2 {
		t.Errorf("Events=%d want 2（1 check + 1 tool）", len(out.Events))
	}
	if out.EstTokens != 42 {
		t.Errorf("EstTokens=%d want 42", out.EstTokens)
	}
}

// TestActQuery_LatestAndByRef：act_query 读最新结论 + 按 ref 定位 + Directive 带回。
// 守护 Act 反馈臂的 MCP 读端——agent 据证据强度决定是否复核完成声明。
func TestActQuery_LatestAndByRef(t *testing.T) {
	root := t.TempDir()
	// 两个结论：feat/a 早、feat/b 晚（Latest 应取 feat/b）。feat/b 标 Unverified→nudge→Directive 非空。
	concs := []act.Conclusion{
		{TaskRef: "feat/a", Grade: "A", Strength: "Strong", Score: 95, CompletedAt: time.Now().Add(-time.Hour)},
		{TaskRef: "feat/b", Grade: "A", Strength: "Unverified", Score: 95, RetrospectiveNudge: true, CompletedAt: time.Now()},
	}
	for i := range concs {
		if err := act.Append(root, &concs[i]); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// 默认（无 ref）= 最新 = feat/b
	latest, err := actQueryCore(root, actQueryInput{})
	if err != nil {
		t.Fatalf("actQuery latest: %v", err)
	}
	if latest.TaskRef != "feat/b" {
		t.Errorf("latest TaskRef=%q want feat/b", latest.TaskRef)
	}
	if latest.Strength != "Unverified" {
		t.Errorf("Strength=%q want Unverified", latest.Strength)
	}
	if latest.Directive == "" {
		t.Error("RetrospectiveNudge 时 Directive 应非空（喂 session-retrospective）")
	}

	// 按 ref 定位 feat/a
	byRef, err := actQueryCore(root, actQueryInput{Ref: "feat/a"})
	if err != nil {
		t.Fatalf("actQuery by ref: %v", err)
	}
	if byRef.TaskRef != "feat/a" || byRef.Strength != "Strong" {
		t.Errorf("byRef=%+v want feat/a Strong", byRef)
	}
	if byRef.Directive != "" {
		t.Errorf("Strong+高分 Directive=%q want 空（静默）", byRef.Directive)
	}

	// 不存在的 ref 报错，不静默返回空
	if _, err := actQueryCore(root, actQueryInput{Ref: "feat/none"}); err == nil {
		t.Error("未知 ref 应报错")
	}
}

// TestActQuery_NoConclusions：无结论时报错（提示 complete 产出），不返回零值误导。
func TestActQuery_NoConclusions(t *testing.T) {
	root := t.TempDir()
	if _, err := actQueryCore(root, actQueryInput{}); err == nil {
		t.Error("无结论应报错（agent 据此知尚无完成）")
	}
}

// TestHealthQuery_Aggregates：health_query 把结论上卷成项目趋势（盲区率/复发低分维度）。
// 守护 task→project 粒度联动的 MCP 读端。
func TestHealthQuery_Aggregates(t *testing.T) {
	root := t.TempDir()
	concs := []act.Conclusion{
		{TaskRef: "feat/a", Grade: "A", Strength: "Strong", Score: 95, LowDimensions: []string{"scope"}, CompletedAt: time.Now().Add(-time.Hour)},
		{TaskRef: "feat/b", Grade: "D", Strength: "Unverified", Score: 60, LowDimensions: []string{"scope"}, RetrospectiveNudge: true, CompletedAt: time.Now()},
	}
	for i := range concs {
		if err := act.Append(root, &concs[i]); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	out, err := healthQueryCore(root, healthQueryInput{})
	if err != nil {
		t.Fatalf("healthQuery: %v", err)
	}
	if out.TotalTasks != 2 {
		t.Errorf("TotalTasks=%d want 2", out.TotalTasks)
	}
	// 1/2 Unverified → 盲区率 0.5
	if out.BlindSpotCount != 1 || out.BlindSpotRate != 0.5 {
		t.Errorf("BlindSpot=%d/%v want 1/0.5", out.BlindSpotCount, out.BlindSpotRate)
	}
	// scope 跨 2 任务复发
	if len(out.LowDims) != 1 || out.LowDims[0].Dimension != "scope" || out.LowDims[0].Count != 2 {
		t.Errorf("LowDims=%+v want scope×2", out.LowDims)
	}
}

// TestHealthQuery_NoConclusions：无结论时返回零值 Summary（total=0），不报错——
// 与 act_query 区分：项目级"暂无数据"是合法状态。
func TestHealthQuery_NoConclusions(t *testing.T) {
	out, err := healthQueryCore(t.TempDir(), healthQueryInput{})
	if err != nil {
		t.Fatalf("无结论不应报错（合法空状态）: %v", err)
	}
	if out.TotalTasks != 0 {
		t.Errorf("TotalTasks=%d want 0", out.TotalTasks)
	}
}

// TestTaskGate_UnknownGate：未知 gate id 报错（含合法值列表），不静默通过。
func TestTaskGate_UnknownGate(t *testing.T) {
	root := t.TempDir()
	// 先存一个 task state，让 loadTaskState(Ref) 能通过到 GateByID 校验那一步
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{TaskRef: "feat/x"}); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	_, err := taskGateCore(root, taskGateInput{GateID: "bogus-gate", Ref: "feat/x"})
	if err == nil {
		t.Fatal("未知 gate 应报错")
	}
}

// TestTaskStatus_NoActiveTask：无任务时明确报错，而非返回空值/nil panic。
func TestTaskStatus_NoActiveTask(t *testing.T) {
	root := t.TempDir()
	_, err := taskStatusCore(root, taskStatusInput{})
	if err == nil {
		t.Fatal("无活跃任务应报错（不应返回空）")
	}
}

// =====================================================================
// skill eval 闭环：端到端（cases→submit 经 MCP 协议层）+ core 闭环（回归）
// =====================================================================

// writeSkillForMCP 在 canonical 下造一个带 SKILL.md 的 skill 目录（2 trigger + 1 skip）。
func writeSkillForMCP(t *testing.T, canonical, name, desc string) {
	t.Helper()
	sd := filepath.Join(canonical, name)
	if err := os.MkdirAll(sd, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// evalSkillDesc 2 trigger（or 分隔）+ 1 skip，均 >3 rune。
const evalSkillDesc = "Use when: 编写 React 组件 or 实现前端布局 SKIP: 选择技术栈"

// TestServer_CallSkillEval_CasesAndSubmit：端到端——cases 工具拿 case 集 + dispatch
// 协议，submit 工具整批回填。证明 3 个新工具真能经 MCP 调到（注册 + handler 签名 +
// JSON 序列化链路通），不止 core 函数正确。隔离 home + canonical。
func TestServer_CallSkillEval_CasesAndSubmit(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	canonical := t.TempDir()
	writeSkillForMCP(t, canonical, "eval-skill", evalSkillDesc)
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)

	cs := startTestServer(t)

	// cases
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forge_skill_eval_cases",
		Arguments: map[string]any{"skill": "eval-skill"},
	})
	if err != nil {
		t.Fatalf("cases CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("cases IsError: %+v", res.Content)
	}
	var casesOut skillEvalCasesOutput
	parseToolText(t, res, &casesOut)
	if len(casesOut.Cases) != 3 {
		t.Fatalf("cases=%d want 3", len(casesOut.Cases))
	}
	if casesOut.DispatchProtocol == "" || casesOut.DescHash == "" {
		t.Fatal("缺 dispatch_protocol / desc_hash")
	}

	// 构造全对 results → submit
	results := make([]map[string]any, 0, len(casesOut.Cases))
	for _, c := range casesOut.Cases {
		act := ""
		if c.Kind == "trigger" {
			act = "eval-skill"
		}
		results = append(results, map[string]any{"case_id": c.ID, "actual_triggered": act})
	}
	res2, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "forge_skill_eval_submit",
		Arguments: map[string]any{"skill": "eval-skill", "agent_model": "sonnet", "results": results},
	})
	if err != nil {
		t.Fatalf("submit CallTool: %v", err)
	}
	if res2.IsError {
		t.Fatalf("submit IsError: %+v", res2.Content)
	}
	var subOut skillEvalSubmitOutput
	parseToolText(t, res2, &subOut)
	if subOut.RunID == "" {
		t.Fatal("submit 缺 run_id")
	}
	if subOut.HealthScore != 100 {
		t.Fatalf("health=%.2f want 100（首次全对）", subOut.HealthScore)
	}
	if subOut.HasBaseline {
		t.Fatal("首次 run 不应有 baseline")
	}
}

// TestSkillEval_FullRegressionLoop：core 闭包级完整闭环——cases→submit(全对)→
// SetBaseline→submit(1 regression)→report，断言 NetRegressions=1、HasBaseline、
// Comparable（version/model 一致）。覆盖回归三态 + version 戳记。
func TestSkillEval_FullRegressionLoop(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	canonical := t.TempDir()
	writeSkillForMCP(t, canonical, "eval-skill", evalSkillDesc)
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)
	dir, _ := skillseval.EvalDir()

	casesOut, err := skillEvalCasesCore("v1")(skillEvalCasesInput{Skill: "eval-skill"})
	if err != nil {
		t.Fatalf("cases: %v", err)
	}

	// run1 全对。
	allRight := make([]skillEvalSubmitResult, 0, len(casesOut.Cases))
	for _, c := range casesOut.Cases {
		act := ""
		if c.Kind == "trigger" {
			act = "eval-skill"
		}
		allRight = append(allRight, skillEvalSubmitResult{CaseID: c.ID, ActualTriggered: act})
	}
	sub1, err := skillEvalSubmitCore("v1")(skillEvalSubmitInput{Skill: "eval-skill", AgentModel: "sonnet", Results: allRight})
	if err != nil {
		t.Fatalf("submit1: %v", err)
	}

	// baseline 显式设定（MCP 无 baseline 工具，直接调数据层，模拟 CLI eval-baseline）。
	if err := skillseval.SetBaseline(dir, "eval-skill", sub1.RunID, "test"); err != nil {
		t.Fatal(err)
	}

	// run2：第一个 trigger case 误路由 → regression。
	bad := make([]skillEvalSubmitResult, len(allRight))
	copy(bad, allRight)
	first := true
	for i, c := range casesOut.Cases {
		if c.Kind == "trigger" && first {
			bad[i].ActualTriggered = "wrong-skill"
			first = false
		}
	}
	sub2, err := skillEvalSubmitCore("v1")(skillEvalSubmitInput{Skill: "eval-skill", AgentModel: "sonnet", Results: bad})
	if err != nil {
		t.Fatalf("submit2: %v", err)
	}
	if !sub2.HasBaseline {
		t.Fatal("run2 应锚定 baseline")
	}
	if sub2.NetRegressions != 1 {
		t.Fatalf("submit2 net_regressions=%d want 1", sub2.NetRegressions)
	}
	if !sub2.Comparable {
		t.Fatal("应可比（version/model 一致）")
	}

	// report 读端。
	repOut, err := skillEvalReportCore(skillEvalReportInput{Skill: "eval-skill"})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if !repOut.Report.HasBaseline {
		t.Fatal("report 应锚定 baseline")
	}
	if repOut.Report.NetRegressions != 1 {
		t.Fatalf("report net_regressions=%d want 1", repOut.Report.NetRegressions)
	}
}

// parseToolText 从 CallToolResult 的首个 TextContent 反序列化到 out。
func parseToolText(t *testing.T, res *mcp.CallToolResult, out any) {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("无 content 返回")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] 不是 TextContent: %T", res.Content[0])
	}
	if err := json.Unmarshal([]byte(tc.Text), out); err != nil {
		t.Fatalf("反序列化失败 err=%v（text=%q）", err, tc.Text)
	}
}
