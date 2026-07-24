package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	workflowScaffoldCmd.Flags().String(`out`, ``, `输出文件路径（默认 stdout，便于预览/管道）`)
	workflowCmd.AddCommand(workflowScaffoldCmd)
	rootCmd.AddCommand(workflowCmd)
}

// workflowCmd 是 Forge workflow 资产命令组。spawn 式编排器（Symphony 类）按 per-issue workspace
// 驱动 Forge 闭环时，需要一份机器可读的接口描述——WORKFLOW.md scaffold 就是这个接口：编排器读完
// 知道每个外部 issue 如何经 Forge MCP 走 proof-of-work 闭环（start from_issue → 三门禁 → review →
// proof → complete）。mount 式（agent 手动 forge task ...）不需要本文件。
var workflowCmd = &cobra.Command{
	Use:   `workflow`,
	Short: `Forge workflow 资产（spawn 式编排器接口 scaffold）`,
}

// workflowScaffoldCmd 生成 WORKFLOW.md scaffold。内容描述 proof-of-work 闭环在 per-issue workspace
// 的驱动流程，供 spawn 式编排器读取自动驱动。--out 写文件（默认 ./WORKFLOW.md 习惯），省略走 stdout
// 便于预览/管道（CI 校验、diff）。scaffold 是起点，编排器可按需定制段落。
var workflowScaffoldCmd = &cobra.Command{
	Use:   `scaffold`,
	Short: `生成 WORKFLOW.md scaffold（spawn 式编排器接口）`,
	Long: `forge workflow scaffold 生成 WORKFLOW.md——描述 Forge proof-of-work 闭环在 per-issue
workspace 的驱动流程，供 spawn 式编排器（Symphony 类）读取并自动驱动。

每个外部 issue（linear/github）对应一次完整闭环：start from_issue 锚定 → 三门禁 → review →
proof 预判 → complete 收口。mount 式（agent 手动 forge task ...）不需要本文件。

--out 写文件，省略走 stdout（便于预览/管道）。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, _ := cmd.Flags().GetString(`out`)
		content := workflowScaffoldContent
		if out == "" {
			fmt.Fprint(cmd.OutOrStdout(), content)
			return nil
		}
		if err := os.WriteFile(out, []byte(content), 0644); err != nil {
			return fmt.Errorf(`write %s: %w`, out, err)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), `WORKFLOW.md scaffold →`, out)
		return nil
	},
}

// workflowScaffoldContent 是 WORKFLOW.md 的 scaffold 正文。无 markdown 代码围栏（避免 raw string
// 反引号冲突），tool 名纯文本。编排器读完即知：workspace 创建后 forge init，每个 issue 经 MCP
// 闭环走 proof-of-work。错误处理段把 BLOCKED/done=false/missingGates 的返工路径写明——编排器不猜。
const workflowScaffoldContent = `# Forge Workflow

本文件描述 Forge proof-of-work 闭环在 per-issue workspace 的驱动流程，供 spawn 式编排器（Symphony 类）
读取并自动驱动。每个外部 issue（linear/github）对应一次完整闭环。Forge 是 deterministic-quality
harness 的验证层 + 状态层：声明完成必须有 deterministic 证据（门禁/checklog/验收实跑），对冲 LLM-judge
看不出 agent 跳过前置就声明完成的盲区。

## after_create: forge init

workspace 创建后第一步：forge init 装好项目级 Forge（hooks + 质量协议 + MCP server 注册）。
验证：forge --version 可跑 + .forge/ 目录存在 + .claude/settings*.json 含 Forge hooks。
未 init 则后续 MCP 工具无项目根可解析。

## 闭环流程（per issue）

1. 锚定外部 issue —— forge_task_start（from_issue=<issue url>）
   把 issue 登记为 tracked task：记 HEAD / 验收标准 / ExternalOrigin。proof-of-work 入口。
   from_issue URL（linear: .../issue/FOO-1 / github: .../issues/42）解析为 ExternalOrigin，
   解耦 mount 式（agent 自起 task）与 spawn 式（编排器从外部 issue 起 task）的 origin。

2. 实现 —— agent 改码（Read/Edit/Bash）
   Forge hooks 自动追踪：checklog（编译/断言/测试覆盖实跑结果）、toollog（工具调用计数）。
   task-guard/read-before-edit/bash-guard/file-sentinel 在此阶段拦截未授权/盲改源码。

3. 三门禁（按序，经 forge_task_gate 推进）：
   - task-implement：编译通过 + 断言未弱化（auto-compile/assertion-check）
   - task-verify：测试伴随变更（改源码有对应 _test.go，test-coverage advisory）
   - task-complete：E2E 验证 + code-review-gate 硬前置（审查真的跑过 + 快照防审查后偷改）
   每道门禁 passed=false 即 BLOCKED（非 advisory），停，按 message 修复后重跑该门禁，不跳。

4. 审查 —— 派只读子 agent 审查 diff → forge review pass
   pass 绑代码快照（ReviewedHeadCommit/ReviewedChangeHash）。审查后改码 → drift，
   task-complete 门禁与 forge_task_proof 都会检测，强制复审。

5. 预判 —— forge_task_proof（ref=<task ref>）
   read-only dry-run，不评分不清 task。done = IsComplete AND 无 review drift AND 验收全过。
   acceptance 双轨：AcceptedHeadCommit==HEAD 信任快照（v2 快路径），否则只读重跑（v1 兜底）。
   done=false 按 reason 返工（补门禁/复审/补验收），绝不在此步声明完成。

6. 收口 —— forge_task_complete（ref=<task ref>）
   强制 ref（不可逆收口，防误清非预期 active task）。IsComplete 才放行，否则 BLOCKED 带缺失门禁。
   评分（ScoreTask，与 CLI 共用真相源）+ Act 结论落盘（证据驱动 directive）+ 清 active。
   complete 后紧随的 git commit 由 post-complete grace（5min）放行，不被 file-sentinel quarantine。

## 错误处理（编排器不猜，按信号返工）

- forge_task_gate 返回 passed=false（BLOCKED）：停，按 message 修复后重跑【该】门禁，不跳到下一道。
- forge_task_proof done=false：不调 complete。按 reason：
  - 门禁未全过 → 回 step 3 补对应门禁。
  - review drift → 重派只读子 agent 审查 → forge review pass 刷新基线。
  - 验收未过 → 补验收（实跑 Run 比对 Expected）。
- forge_task_complete 报 missingGates：回 step 3 补门禁（proof 应已预判，此处是兜底）。
- 评分 warning（scoring failed / act append failed）：不阻断 complete（completed=true），
  warning 进 result.Warnings，编排器记录但不重试（证据强度不依赖分数）。

## 可观测（编排器读状态决策）

- forge_task_status：门禁进度（completed/next/current）、是否完成。
- forge_task_resume：完整接续上下文（goal/plan/决策/阻塞/参与工具/git 已改）——冷启动恢复。
- forge_act_query：任务结论（score/grade/证据强度/验收通过率/低分维度/RetrospectiveNudge）。
- forge_health_query：项目级质量趋势（盲区率/复发低分维度）——跨 issue 聚合暴露系统性问题。
`
