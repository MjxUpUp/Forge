// Package mcpserver exposes Forge's quality-governance surface as an MCP server
// (stdio transport), so AI agents (Claude Code / Codex / Copilot) can drive
// gates, inspect task/trace state, and read the cross-project knowledge base
// through the Model Context Protocol instead of shelling out to the CLI.
//
// 这是 loop engineering 的「agent 可编程接口」层：把 forge 的验证+状态+学习
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
	"forge_gate_run":           "运行项目级管道的一道门禁（pipeline.yml 定义）。执行 hooks + 评估 checks + 写 status.json。返回 passed/errors/duration。",
	"forge_task_status":        "查看任务状态：当前 session 活跃任务或指定 ref。返回门禁进度（completed/next/current）、是否完成。",
	"forge_task_gate":          "推进 task 级门禁（task-implement / task-verify / task-complete）。执行检查、记录结果、持久化 state。注意：不触发评分（评分用 forge task complete）。",
	"forge_experience_search":  "搜索 task 派生的经验提案（.forge/experience/proposed/）。按关键词/状态过滤。经验闭环的读端。",
	"forge_experience_propose": "提议一条新经验（写入 proposed/，status=proposed 待审）。经验闭环的写端——把 loop 中发现的坑沉淀成可复用知识。",
	"forge_trace_query":        "查询任务的完整质量事件时间线（checklog 检查 + toolusage 工具调用，按时间排序）+ 估算 token（loop 成本代理）。",
	"forge_knowledge_lookup":   "查询跨项目知识库（~/.forge/knowledge/，全局共享）。按关键词/类别（gotchas/patterns/apis）过滤。",
	"forge_skill_eval_cases":   "生成 skill 的 eval case 集 + dispatch 指令包（agent 据此 dispatch fresh subagent 跑回归）。skill eval 闭环的读端。",
	"forge_skill_eval_submit":  "整批回填一次 eval run（agent dispatch 跑完每个 prompt 后提交实际触发结果）。归一化 + 判定 + 算 health + append。skill eval 闭环的写端。",
	"forge_skill_eval_report":  "比对 latest run vs baseline，输出回归报告（regression 三态 + pass-rate delta + 可比性）。skill eval 闭环的回归读端。",
}

// New 构建 Forge MCP server 并注册全部 10 个工具。
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
		Name:        "forge_gate_run",
		Description: ToolDescriptions["forge_gate_run"],
	}, withRoot(gateRunCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_task_status",
		Description: ToolDescriptions["forge_task_status"],
	}, withRoot(taskStatusCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_task_gate",
		Description: ToolDescriptions["forge_task_gate"],
	}, withRoot(taskGateCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_experience_search",
		Description: ToolDescriptions["forge_experience_search"],
	}, withRoot(experienceSearchCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_experience_propose",
		Description: ToolDescriptions["forge_experience_propose"],
	}, withRoot(experienceProposeCore))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_trace_query",
		Description: ToolDescriptions["forge_trace_query"],
	}, withRoot(traceQueryCore))

	// 全局工具（知识库在 ~/.forge/knowledge/，不依赖项目根）
	mcp.AddTool(s, &mcp.Tool{
		Name:        "forge_knowledge_lookup",
		Description: ToolDescriptions["forge_knowledge_lookup"],
	}, withoutRoot(knowledgeLookupCore))

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
