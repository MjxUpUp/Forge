// Package mcpserver exposes Forge's quality-governance surface as an MCP server
// (stdio transport), so AI agents (Claude Code / Codex / Copilot) can drive
// gates, and inspect task/trace state
// through the Model Context Protocol instead of shelling out to the CLI.
//
// 这是 loop engineering 的「agent 可编程接口」层：把 forge 的验证+状态
// 能力暴露为 MCP 工具，让 agent 在 loop 里结构化调用，而非 parse CLI 文本输出。
// 复用 internal 包公开函数（最薄封装），不 shell-out forge 命令。
package mcpserver

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolDescriptions 集中维护工具描述（server.go 注册 + cli/mcp.go 帮助文档共用）。
// 改描述只改一处，避免注册和文档漂移。
var ToolDescriptions = map[string]string{
	"forge_task_status":       "查看任务状态：当前 session 活跃任务或指定 ref。返回门禁进度（completed/next/current）、是否完成。",
	"forge_task_gate":         "推进 task 级门禁（task-implement / task-verify / task-complete）。执行检查、记录结果、持久化 state。注意：不触发评分（评分用 forge task complete）。",
	"forge_task_resume":       "接续真相源入口：拉回任务完整接续上下文（goal/plan/decisions/next_steps/blockers/findings/artifacts + 参与工具 + 门禁进度 + git 已改未提交）。新会话冷启动调用即秒级恢复，抗压缩丢失。默认把当前 session 锚定到 task（多向锚定记录参与方），no_attach=true 仅读取。",
	"forge_task_decide":       "记录一条已确认决策到 task（持久化进 ~/.forge/projects/<key>/tasks/<ref>.json（DataDir），跨会话/跨工具不再推翻）。接手方 resume 即知已决定什么。",
	"forge_task_attach":       "把一个 session+工具锚定到 task（跨工具接续的多向锚定：pi 起、claude-code 接等）。任意接手方 resume 即知谁参与过、用什么工具。",
	"forge_trace_query":       "查询任务的完整质量事件时间线（checklog 检查 + toolusage 工具调用，按时间排序）+ 估算 token（loop 成本代理）。",
	"forge_act_query":         "查询任务结论（最新或指定 ref）：score/grade/证据强度（Strength: Strong/Weak/Unverified/NoData）/ratio/deterministic vs agent-claim 计数/验收通过率/低分维度/RetrospectiveNudge + Directive。Act 反馈臂的 agent 读端——读'这次完成声明有多少 deterministic 证据'，对冲 LLM-judge 看不出 agent 跳过前置就声明完成的盲区。",
	"forge_health_query":      "项目级质量趋势上卷（task→project 粒度）：任务数/均分/中位/grade 分布/证据强度分布/blind_spot_rate（完成声明主要靠 agent 自述的任务占比=项目级 LLM-judge 盲区率）/复发低分维度频次/趋势/phase_pass_rate（各环节设计产物审查通过率）。跨任务聚合暴露系统性问题（某维度反复低分、声明完成却没真验证）。无参数。",
	"forge_skill_eval_cases":  "生成 skill 的 eval case 集 + dispatch 指令包（agent 据此 dispatch fresh subagent 跑回归）。skill eval 闭环的读端。",
	"forge_skill_eval_submit": "整批回填一次 eval run（agent dispatch 跑完每个 prompt 后提交实际触发结果）。归一化 + 判定 + 算 health + append。skill eval 闭环的写端。",
	"forge_skill_eval_report": "比对 latest run vs baseline，输出回归报告（regression 三态 + pass-rate delta + 可比性）。skill eval 闭环的回归读端。",
}

// New 构建 Forge MCP server 并注册全部 MCP 工具。
// version 注入到 MCP server handshake（forge 二进制版本）。
func New(ver string) *mcp.Server {
	if ver == "" {
		ver = "dev"
	}
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "forge",
		Version: ver,
	}, nil)

	// 项目级工具（需解析 .forge/ 项目根）
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_task_status",
		Description: ToolDescriptions["forge_task_status"],
	}, withRoot(taskStatusCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_task_gate",
		Description: ToolDescriptions["forge_task_gate"],
	}, withRoot(taskGateCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_task_resume",
		Description: ToolDescriptions["forge_task_resume"],
	}, withRoot(taskResumeCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_task_decide",
		Description: ToolDescriptions["forge_task_decide"],
	}, withRoot(taskDecideCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_task_attach",
		Description: ToolDescriptions["forge_task_attach"],
	}, withRoot(taskAttachCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_trace_query",
		Description: ToolDescriptions["forge_trace_query"],
	}, withRoot(traceQueryCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_act_query",
		Description: ToolDescriptions["forge_act_query"],
	}, withRoot(actQueryCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_health_query",
		Description: ToolDescriptions["forge_health_query"],
	}, withRoot(healthQueryCore))

	// skill eval 闭环工具（canonical 全局、EvalDir 在 ~/，不依赖项目根）。
	// cases/submit 用闭包捕获 ver（canonical embed fallback + run 版本戳记）。
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_skill_eval_cases",
		Description: ToolDescriptions["forge_skill_eval_cases"],
	}, withoutRoot(skillEvalCasesCore(ver)))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_skill_eval_submit",
		Description: ToolDescriptions["forge_skill_eval_submit"],
	}, withoutRoot(skillEvalSubmitCore(ver)))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_skill_eval_report",
		Description: ToolDescriptions["forge_skill_eval_report"],
	}, withoutRoot(skillEvalReportCore))

	return s
}

// Run 在 stdio 上服务 MCP 协议（Claude Code/Codex/Copilot 的 stdio transport）。
// 阻塞直到 client 断开（stdin EOF）。
func Run(ctx context.Context, ver string) error {
	err := New(ver).Run(ctx, &mcp.StdioTransport{})
	if err == nil {
		return nil
	}
	// stdin 关闭（client 断开）是 MCP stdio server 的正常生命周期结束。
	// go-sdk 把底层 EOF/连接关闭作为 error 返回，但对 CLI 调用方这是正常退出——
	// 若原样返回，cobra 会打印 "Error: server is closing: EOF" + Usage 到 stderr，
	// 污染 MCP client 的 stderr 通道。这里识别关闭类 error 并当作正常。
	if errors.Is(err, io.EOF) || errors.Is(err, mcp.ErrConnectionClosed) {
		return nil
	}
	// 文本兜底：go-sdk 部分关闭路径的 error 未必 errors.Is 可提取。只匹配已知的
	// 关闭短语——不匹配裸 "EOF"，避免误吞含 EOF 的真实错误（如 "unexpected EOF in payload"）。
	msg := err.Error()
	if strings.Contains(msg, "server is closing") || strings.Contains(msg, "connection closed") {
		return nil
	}
	return err
}

// withRoot 把一个「接收 root + In → Out」的 core 函数适配成 mcp.AddTool 的
// handler 签名 `func(ctx, *CallToolRequest, In) (*CallToolResult, Out, error)`，
// 并在每次调用时解析项目根。错误（不在 forge 项目内）走 MCP 的 IsError 通道。
func withRoot[I any, O any](core func(string, I) (O, error)) func(context.Context, *mcp.CallToolRequest, I) (*mcp.CallToolResult, O, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, O, error) {
		root, err := findProjectRoot()
		if err != nil {
			var zero O
			return nil, zero, err
		}
		out, err := core(root, in)
		return nil, out, err
	}
}

// withoutRoot 同 withRoot，但 core 不需要项目根（全局工具，如知识库查询）。
func withoutRoot[I any, O any](core func(I) (O, error)) func(context.Context, *mcp.CallToolRequest, I) (*mcp.CallToolResult, O, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in I) (*mcp.CallToolResult, O, error) {
		out, err := core(in)
		return nil, out, err
	}
}

// findProjectRoot 委托到 internal/projectroot —— 与 cli 共用同一份"向上找 .forge/"逻辑，
// 避免两份复制漂移（mcpserver 不能 import cli，但都能 import 中立的 projectroot 包）。
func findProjectRoot() (string, error) {
	return projectroot.Find()
}
