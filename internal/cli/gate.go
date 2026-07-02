package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/artifact"
	"github.com/MjxUpUp/Forge/internal/pipeline"
	"github.com/spf13/cobra"
)

// gateCircuitBreakerThreshold 是同一 gate 累计失败次数的 circuit breaker 阈值（advisory）。
// loop engineering 成本上限——达到即警示 loop 疑似卡死。不硬阻断（避免卡死用户无法推进），
// 但提示换策略而非闷头重试烧 token。5 次覆盖"修了再试"的正常往返，超出多为无效循环。
const gateCircuitBreakerThreshold = 5

func init() {
	rootCmd.AddCommand(gateCmd)
	gateCmd.Flags().Bool("force", false, "跳过前置条件检查")
	gateCmd.Flags().Bool("retry", false, "重新执行上一次失败的 gate")
	gateCmd.Flags().Bool("silent", false, "仅输出状态码（Hook 集成用）")
	gateCmd.Flags().Bool("current", false, "运行当前活跃的门禁（从 state.json 读取）")
}

var gateCmd = &cobra.Command{
	Use:   "gate <gate-id> [--force] [--retry] [--silent] [--current]",
	Short: "运行单道门禁（验证产出物 + 执行检查规则）",
	Long: `forge gate 执行 pipeline.yml 中指定的一道门禁。

不做 AI 调用——只执行 hooks、评估 checks、写 status.json、更新 state.json。
AI 执行由 Claude Code Skill 通过 subagent 驱动。

--force   跳过前置条件检查（记录到 overrides）
--retry   重新执行上一次失败的 gate
--silent  静默模式（hook 集成用，只返回退出码）
--current 运行 state.json 中 current_gate 指定的门禁（无活跃门禁时静默退出）`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGate,
}

func runGate(cmd *cobra.Command, args []string) error {
	currentFlag, _ := cmd.Flags().GetBool("current")

	var gateID string
	if currentFlag {
		// Read current_gate from state.json
		root, err := findProjectRoot()
		if err != nil {
			// 非 forge 项目：无活跃 gate 可运行，静默退出。Stop hook
			// (forge gate --current --silent) 在任意 cwd 触发，非 forge 项目
			// 报错会污染 hook 输出——与下方 state 缺失 / 无活跃 gate 的静默处理
			// 一致（hook 兼容）。手动跑 forge gate <id> 仍会经 loadPipeline 报错。
			return nil
		}
		state, err := pipeline.LoadState(root)
		if err != nil {
			// Graceful degradation: if state.json is missing or corrupt,
			// there's no active gate — silent exit for hook compatibility
			return nil
		}
		if state.CurrentGate == "" {
			// No active gate — silent exit (for Stop hook compatibility)
			return nil
		}
		gateID = state.CurrentGate
	} else {
		if len(args) == 0 {
			return fmt.Errorf("requires a gate ID argument or --current flag")
		}
		gateID = args[0]
	}

	force, _ := cmd.Flags().GetBool("force")
	retry, _ := cmd.Flags().GetBool("retry")
	silent, _ := cmd.Flags().GetBool("silent")

	p, root, err := loadPipeline()
	if err != nil {
		return err
	}

	gate, err := p.GetGate(gateID)
	if err != nil {
		return err
	}

	if !gate.Enabled {
		return fmt.Errorf("gate '%s' is disabled in pipeline.yml", gateID)
	}

	state, err := pipeline.LoadState(root)
	if err != nil {
		return err
	}

	// Iteration circuit breaker：同一 gate 累计失败 ≥阈值则警示（advisory，不硬阻断）。
	// 反复失败说明 loop 卡死，闷头重试只烧 token 不解决问题——提示换策略而非继续硬重试。
	if fails := pipeline.GateFailures(state, gateID); fails >= gateCircuitBreakerThreshold {
		fmt.Fprintf(os.Stderr, "⚠️ [breaker] gate '%s' 已失败 %d 次（≥%d）——loop 疑似卡死。\n", gateID, fails, gateCircuitBreakerThreshold)
		fmt.Fprintf(os.Stderr, "   闷头重试只烧 token。考虑：查 root cause / 拆小步验证 / 换实现路径。\n")
	}

	// Handle --retry
	if retry {
		prev := pipeline.LatestGateResult(state, gateID)
		if prev == nil {
			return fmt.Errorf("gate '%s' has never been executed — nothing to retry", gateID)
		}
		if prev.Passed {
			if !silent {
				fmt.Printf("Gate '%s' already passed — nothing to retry\n", gateID)
			}
			return nil
		}
		if !silent {
			fmt.Printf("Retrying failed gate '%s'...\n", gateID)
		}
	}

	if !silent {
		fmt.Printf("Running %s (%s)...\n", gate.Name, gateID)
	}

	// Unified execution
	result, err := pipeline.ExecuteGate(root, gate, state, p, force)
	if err != nil {
		return err
	}

	// Feishu publish
	if gate.AutoPublishFeishu && result.Status.Passed {
		cfg := artifact.DefaultFeishuConfig()
		if cfg.Enabled {
			artifact.PublishAllOutputs(cfg, gate.ID, gate.Artifacts.Outputs, root)
		}
	}

	if !silent {
		if result.Status.Passed {
			fmt.Printf("  passed (%.1fs)\n", result.Duration.Seconds())
		} else {
			fmt.Printf("  FAILED (%.1fs)\n", result.Duration.Seconds())
			for _, e := range result.Status.Errors {
				fmt.Printf("    - %s: %s\n", e.Check, e.Message)
			}
		}
	}

	// Handle on_failure
	if !result.Status.Passed && gate.OnFailure == "abort" {
		return fmt.Errorf("gate '%s' failed with on_failure=abort", gateID)
	}

	return nil
}

// Gate 7: auto-generate .forge/CLAUDE.md with lessons (kept for backward compat)
func generateCLAUDE(p *pipeline.Pipeline, state *pipeline.State, dir, lessonsContent string) error {
	forgeDir := filepath.Join(dir, ".forge")
	content := fmt.Sprintf(`# 本项目 AI 协作规则

## 项目信息
- 管道模式：%s
- 当前 Gate：%s
- 最后更新：%s

## 本次开发经验
%s

## 技术栈
- 语言：待补充（请根据项目实际技术栈更新）
- 框架：待补充
- 构建工具：待补充

## 已知问题
- 见 gate-6-acceptance/gap-analysis.json

---
_由 Forge 自动生成_
`, p.Mode, state.CurrentGate, time.Now().Format("2006-01-02"), lessonsContent)

	path := filepath.Join(forgeDir, "CLAUDE.md")
	return os.WriteFile(path, []byte(content), 0644)
}
