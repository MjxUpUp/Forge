package cli

import (
	"context"

	"github.com/MjxUpUp/Forge/internal/mcpserver"
	"github.com/spf13/cobra"
)

// forge mcp —— MCP server，把 Forge 的质量治理能力暴露为 agent 可编程接口。
// loop engineering 的「让 agent 结构化调用验证+状态+学习」层，替代 parse CLI 文本。
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server（agent 可编程接口）",
	Long: `forge mcp 把 Forge 的质量治理能力暴露为 MCP 工具，供 Claude Code / Codex / Copilot
等 agent 在 loop 里结构化调用——验证、状态、经验学习的 agent 可编程接口层。`,
}

func init() {
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "在 stdio 上运行 Forge MCP server",
		Long: `forge mcp serve 在 stdin/stdout 上服务 MCP 协议（stdio transport）。

工具（12 个）：
  forge_gate_run            运行项目级管道门禁（pipeline.yml）
  forge_task_status         查看任务状态（活跃 / 指定 ref）
  forge_task_gate           推进 task 门禁（implement/verify/complete）
  forge_trace_query         查询任务质量事件时间线 + 估算 token
  forge_act_query           查询任务结论（证据强度/score/验收/低分维度）+ 回顾指令
  forge_health_query        项目级质量趋势上卷（盲区率/复发低分维度，task→project）
  forge_experience_search   搜索经验提案
  forge_experience_propose  提议新经验
  forge_knowledge_lookup    查询跨项目知识库（~/.forge/knowledge/）
  forge_skill_eval_cases    生成 skill eval case 集
  forge_skill_eval_submit   回填一次 eval run
  forge_skill_eval_report   eval 回归报告

配置（Claude Code 项目级 .mcp.json 或用户级 ~/.claude.json）：
  {
    "mcpServers": {
      "forge": { "command": "forge", "args": ["mcp", "serve"] }
    }
  }`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcpserver.Run(context.Background(), rootCmd.Version)
		},
	}
	mcpCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(mcpCmd)
}
