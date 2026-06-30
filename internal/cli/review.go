package cli

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/review"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// forge review 让 code-review-gate 从"靠人手动喊"变成"门禁/hook 自动挡"。
//
// 双路径触发（用户 2026-06-27 point 4 设计）：
//   - task 流程：`forge review pass` 写 TaskState.ReviewPassed，由 task-complete
//     门禁强制（见 executor.go 硬前置）。Stop hook 在 task 模式不拦（避免每次改代码都拦）。
//   - 非 task 流程：Stop hook 调 `forge review gate` 判定，未审源码变更则 block。
//
// gate 是判定引擎（纯逻辑 + exit code）；review-stop hook 脚本只做 Claude Code
// Stop 协议适配（见 hooks/embed.go ReviewStopHook）。

func init() {
	rootCmd.AddCommand(reviewCmd)
	reviewCmd.AddCommand(reviewPassCmd)
	reviewCmd.AddCommand(reviewGateCmd)
	reviewCmd.AddCommand(reviewStatusCmd)
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "代码审查通过标记与门禁",
	Long: `forge review 管理 code-review-gate 的"审查通过"状态，支撑自动触发。

两条路径：
  - task 流程：审查通过标记写入任务状态，task-complete 门禁强制（提交前必审）。
  - 非 task 流程：Stop hook 自动拦截未审的源码变更。

子命令：
  pass    标记当前变更已通过 code-review-gate（task 模式写任务状态，否则写分支 stamp）
  gate    判定当前是否需要审查（Stop hook 调用；exit 0=放行，1=需审 block）
  status  显示当前审查状态`,
}

var reviewPassCmd = &cobra.Command{
	Use:   "pass",
	Short: "标记当前变更已通过 code-review-gate",
	RunE:  runReviewPass,
}

var reviewGateCmd = &cobra.Command{
	Use:   "gate",
	Short: "判定当前是否需要审查（Stop hook 调用）",
	Long: `判定引擎：task 模式放行（审查由 task-complete 门禁管）；非 task 模式按 diff
stamp 决策。输出 PASS/ADVISORY/FAIL 到 stdout，exit 0=放行、1=需审 block。
评估失败 fail-open 放行（forge 安全原则）。`,
	RunE: runReviewGate,
}

var reviewStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "显示当前审查状态",
	RunE:  runReviewStatus,
}

// gateGuidance 是 gate 判定 NEEDS_REVIEW 时输出给 agent 的 Stop additionalContext——
// 指引它加载 skill、派独立子 agent 审查、通过后标记。这是"自动挡"的核心闭环指令。
// 用双引号拼接而非 raw string：内部的反引号（`forge review pass`）在 raw string 里
// 会提前终止字符串（forge-security-hook-fail-open 记的坑），双引号串里则是普通字符。
const gateGuidance = "检测到未审查的源码变更。请按序完成：\n" +
	"1. 加载 code-review-gate skill（/code-review-gate 或读 skills/code-review-gate/SKILL.md）\n" +
	"2. 按其策略派【只读】子 agent 审查当前 diff——独立上下文是底线，不可自审绕过\n" +
	"3. 审查通过后运行 `forge review pass` 标记，再结束会话\n" +
	"注：同一 diff 反复未审最多 block 3 次后 advisory 放行（防 Stop 死循环）。"

func runReviewPass(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	// task 模式：写任务状态字段，由 task-complete 门禁消费
	state, _ := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if state != nil {
		state.MarkReviewPassed()
		if err := taskpipeline.SaveTaskState(root, state); err != nil {
			return fmt.Errorf("failed to save task state: %w", err)
		}
		fmt.Printf("✅ task %s: code-review-gate 已通过（task-complete 门禁前置满足）\n", state.TaskRef)
		return nil
	}

	// 非 task 模式：写分支 stamp
	if err := review.MarkPassed(root); err != nil {
		return fmt.Errorf("failed to mark review passed: %w", err)
	}
	fmt.Println("✅ 当前 diff: code-review-gate 已通过")
	return nil
}

func runReviewGate(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		// 非 forge 项目：无 hook 语境意义，放行
		fmt.Println("PASS 非项目根，放行")
		return nil
	}

	// task 模式：审查由 task-complete 门禁强制（ReviewPassed 硬前置），Stop 不拦——
	// 否则 task 流程里每次改代码都被拦，与门禁重复且扰人。
	state, _ := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if state != nil {
		passed := "未通过"
		if state.ReviewPassed {
			passed = "已通过"
		}
		fmt.Printf("PASS task 模式（%s: ReviewPassed=%s）——审查由 task-complete 门禁强制\n", state.TaskRef, passed)
		return nil
	}

	// 非 task 模式：diff stamp 决策
	dec, reason, err := review.Evaluate(root)
	if err != nil {
		// fail-open：评估失败不阻塞会话（forge 安全 hook 原则）
		fmt.Printf("PASS 评估失败放行：%v\n", err)
		return nil
	}
	switch dec {
	case review.DecisionPass:
		fmt.Printf("PASS %s\n", reason)
		return nil
	case review.DecisionPassAdvisory:
		fmt.Printf("ADVISORY %s\n", reason)
		return nil
	case review.DecisionNeedReview:
		fmt.Printf("FAIL %s\n", reason)
		fmt.Println()
		fmt.Println(gateGuidance)
		// exit 1 = block（Stop hook 据此 decision:block）；用 os.Exit 绕过 cobra 的
		// "Error:" stderr 噪声，保证 stdout 干净供 hook 当 additionalContext。
		os.Exit(1)
	}
	return nil
}

func runReviewStatus(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	return renderReviewStatus(root)
}

// renderReviewStatus is the root-injected core of `forge review status`, kept
// separate so the task-mode evidence-strength rendering is unit-testable on a
// temp project without findProjectRoot / cwd dependence.
func renderReviewStatus(root string) error {
	state, _ := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if state != nil {
		fmt.Println("Mode:         task")
		fmt.Printf("Task:         %s\n", state.TaskRef)
		passed := "false"
		if state.ReviewPassed {
			passed = "true"
		}
		fmt.Printf("ReviewPassed: %s\n", passed)
		fmt.Println()
		if state.ReviewPassed {
			fmt.Println("→ task-complete 门禁的 review 前置已满足")
		} else {
			fmt.Println("→ 未通过：task-complete 前会要求 code-review-gate；运行 `forge review pass` 标记")
		}
		// 证据强度（deterministic 占比）——把 ratio 从可观测升级为驱动 review 校准。
		// Weak/Unverified 时给 reviewer 注入指令：核验声称的验证是否真跑过，对冲 agent
		// 跳过前置就声明完成的盲区。Strong 时静默只报数字（避免噪声）。
		if ec, err := checklog.ForTask(root, state.TaskRef); err == nil && ec.Total() > 0 {
			fmt.Println(fmt.Sprintf(`证据强度:     ratio=%.2f %s（deterministic=%d agent-claim=%d）`,
				ec.Ratio(), ec.Strength(), ec.Deterministic, ec.AgentClaim))
			switch ec.Strength() {
			case checklog.Unverified:
				fmt.Println(`→ ⚠ 完成声明无 deterministic 证据：review 必须核验声称的验证是否真发生过（test-run / gate 实跑条目），不能只信 agent 自述`)
			case checklog.Weak:
				fmt.Println(`→ ⚠ deterministic 占比低：review 重点核验声称的验证是否真跑过，对冲 agent 跳过前置就声明完成的盲区`)
			}
		}
		return nil
	}

	fmt.Println("Mode:         non-task (branch stamp)")
	out, err := review.CurrentState(root)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
