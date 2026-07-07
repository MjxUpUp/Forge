package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/health"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().Bool("json", false, "JSON 格式输出")
	statusCmd.Flags().Bool("system", false, "系统级健康检查")
	// --tasks 默认 true：任务列表是 status 的主体内容，默认显示。传 --tasks=false
	// 隐藏任务块（只看 Project 头 + 质量信号），让 flag 有真实语义而非 dead flag。
	statusCmd.Flags().Bool("tasks", true, "显示任务列表（默认开启；--tasks=false 隐藏）")
	statusCmd.Flags().Bool("agents", false, "显示检测到的 AI 编码工具")
}

var statusCmd = &cobra.Command{
	Use:   "status [--json] [--system] [--tasks] [--agents]",
	Short: "查看项目状态（任务管道 + 质量信号）",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	asSystem, _ := cmd.Flags().GetBool("system")
	showTasks, _ := cmd.Flags().GetBool("tasks")
	showAgents, _ := cmd.Flags().GetBool("agents")

	if asSystem {
		return runSystemStatus()
	}

	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	taskStates, _ := taskpipeline.ListTaskStates(root)

	// 项目级质量信号（task→project 上卷）：把证据盲区率/复发低分维度亮在 status 主入口。
	// 否则 deterministic 信号在 forge health 里算好了，但用户在 status（"项目在哪"主入口）
	// 看不到——可见性缺口。conclusions 为空时省略（项目还没完成任务）。
	var hs *health.Summary
	if proj, err := forgedata.ProjectFor(root); err == nil {
		if cs, err := act.LoadAll(proj); err == nil && len(cs) > 0 {
			s := health.Summarize(cs)
			hs = &s
		}
	}

	if asJSON {
		// Tasks 不加 omitempty：空任务列表（"tasks": []）也是有效状态，调用方（dashboard/
		// 脚本/测试）依赖该字段存在做解构；omitempty 会在无任务时吞掉整个字段，破坏契约。
		output, _ := json.MarshalIndent(struct {
			Tasks  []*taskpipeline.TaskState `json:"tasks"`
			Health *health.Summary           `json:"health,omitempty"`
		}{taskStates, hs}, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	// 默认显示任务管道状态（取代原 project pipeline 渲染——项目级管道已删除）。
	// 始终打印项目头：fresh 项目（无任务）跑 status 也不应输出空白——否则用户无法
	// 判断 forge 是否已就位。空任务列表显式提示，引导下一步。--tasks=false 隐藏任务块。
	fmt.Printf("Project: %s\n", filepath.Base(root))
	if showTasks {
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("Tasks:")
		if len(taskStates) == 0 {
			fmt.Println("  (no active tasks — `forge task start` to begin)")
		} else {
			for _, ts := range taskStates {
				fmt.Printf("  %s (%s) — ", ts.TaskRef, ts.Branch)
				if ts.CompletedAt != nil {
					fmt.Println("completed")
				} else {
					completed := len(ts.CompletedGates())
					total := len(taskpipeline.DefaultGates())
					fmt.Printf("%d/%d gates passed\n", completed, total)
				}
			}
		}
		fmt.Println(strings.Repeat("─", 60))
	}

	// Show detected agents
	if showAgents {
		agents := agentbridge.DetectAgents(root)
		fmt.Println()
		fmt.Println("Detected Agents:")
		if len(agents) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, a := range agents {
				fmt.Printf("  %s\n", a)
			}
		}
	}

	// 项目级质量信号（与 --json 的 Health 同源）。compact 一块，只在有完成任务时显示：
	// 盲区率是头条（项目级 LLM-judge 盲区），复发低分维度次之。forge health 看完整趋势。
	if hs != nil {
		fmt.Println()
		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf(`质量信号: %d 任务完成, 均分 %.0f, 证据盲区率 %.0f%%`+"\n",
			hs.TotalTasks, hs.AvgScore, hs.BlindSpotRate*100)
		if hs.BlindSpotRate >= 0.5 {
			fmt.Println(`  ⚠ 系统性盲区：过半完成声明缺 deterministic 证据——project 级该查验证为何没真跑`)
		}
		if len(hs.LowDims) > 0 {
			top := hs.LowDims[0]
			fmt.Printf(`  复发低分维度: %s ×%d（forge health 看全部）`+"\n", top.Dimension, top.Count)
		}
	}

	return nil
}
