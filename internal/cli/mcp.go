package cli

import (
	"context"

	"github.com/MjxUpUp/Forge/internal/mcpserver"
	"github.com/spf13/cobra"
)

// forge mcp —— MCP server，把 Forge 的质量治理能力暴露为 agent 可编程接口。
// loop engineering 的「让 agent 结构化调用验证+状态」层，替代 parse CLI 文本。
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server（agent 可编程接口）",
	Long: `forge mcp 把 Forge 的质量治理能力暴露为 MCP 工具，供 Claude Code / Codex / Copilot
等 agent 在 loop 里结构化调用——验证、状态查询的 agent 可编程接口层。`,
}

func init() {
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "在 stdio 上运行 Forge MCP server",
		Long: `forge mcp serve 在 stdin/stdout 上服务 MCP 协议（stdio transport）。

工具（14 个）：
  forge_task_start          启动任务（登记 TaskState + HEAD + 验收 + 外部 issue origin）
  forge_task_status         查看任务状态（活跃 / 指定 ref）
  forge_task_gate           推进 task 门禁（implement/verify/complete）
  forge_task_proof          proof-of-work 断言（complete 前 dry-run：done=IsComplete+无review漂移+验收过）
  forge_task_complete       任务完成终点（评分+Act结论落盘+清active；强制ref，IsComplete才放行否则BLOCKED）
  forge_task_resume         拉回任务接续上下文（目标/计划/决策/阻塞/参与工具+git 已改）
  forge_task_decide         记录已确认决策（持久化，跨会话/跨工具不再推翻）
  forge_task_attach         锚定 session+工具到 task（跨工具多向锚定）
  forge_trace_query         查询任务质量事件时间线 + 估算 token
  forge_act_query           查询任务结论（证据强度/score/验收/低分维度）+ 回顾指令
  forge_health_query        项目级质量趋势上卷（盲区率/复发低分维度，task→project）
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
