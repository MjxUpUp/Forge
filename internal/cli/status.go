package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/pipeline"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().Bool("json", false, "JSON 格式输出")
	statusCmd.Flags().Bool("system", false, "系统级健康检查")
	statusCmd.Flags().Bool("tasks", false, "显示任务级管道状态")
	statusCmd.Flags().Bool("agents", false, "显示检测到的 AI 编码工具")
}

var statusCmd = &cobra.Command{
	Use:   "status [--json] [--system] [--tasks]",
	Short: "查看管道当前状态",
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

	p, root, err := loadPipeline()
	if err != nil {
		return err
	}

	state, err := pipeline.LoadState(root)
	if err != nil {
		return err
	}

	// Load task state if requested
	var taskStates []*taskpipeline.TaskState
	if showTasks {
		taskStates, _ = taskpipeline.ListTaskStates(root)
	}

	if asJSON {
		output, _ := json.MarshalIndent(struct {
			Pipeline    *pipeline.Pipeline       `json:"pipeline"`
			State       *pipeline.State           `json:"state"`
			Tasks       []*taskpipeline.TaskState `json:"tasks,omitempty"`
		}{p, state, taskStates}, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	// Pretty print project pipeline
	fmt.Printf("Project: %s (mode: %s)\n", p.Project, p.Mode)
	fmt.Printf("Started: %s\n", state.StartedAt.Format("2006-01-02 15:04:05"))
	fmt.Println(strings.Repeat("─", 60))

	for _, g := range p.EnabledGates() {
		marker := "  "
		status := "pending"

		latest := pipeline.LatestGateResult(state, g.ID)
		if latest != nil {
			if latest.Passed {
				marker = "✅"
				status = fmt.Sprintf("passed (attempt %d)", latest.Attempt)
			} else {
				marker = "❌"
				status = fmt.Sprintf("failed (attempt %d)", latest.Attempt)
			}
		}

		if state.CurrentGate == g.ID {
			marker = "🚦"
			status = "running"
		}

		fmt.Printf("%s %-25s %s", marker, g.Name, status)
		if latest != nil {
			fmt.Printf(" (%s)", latest.CompletedAt.Format("15:04:05"))
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("─", 60))

	if len(state.Overrides) > 0 {
		fmt.Println("\nOverrides:")
		for _, o := range state.Overrides {
			fmt.Printf("  %s: %s\n", o.Gate, o.Reason)
		}
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

	// Show task pipeline status
	if showTasks && len(taskStates) > 0 {
		fmt.Println()
		fmt.Println("Tasks:")
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

	return nil
}
