package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Harness/forge/internal/pipeline"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().Bool("json", false, "JSON 格式输出")
	statusCmd.Flags().Bool("system", false, "系统级健康检查")
}

var statusCmd = &cobra.Command{
	Use:   "status [--json] [--system]",
	Short: "查看管道当前状态",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	asSystem, _ := cmd.Flags().GetBool("system")

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

	if asJSON {
		output, _ := json.MarshalIndent(struct {
			Pipeline *pipeline.Pipeline `json:"pipeline"`
			State    *pipeline.State    `json:"state"`
		}{p, state}, "", "  ")
		fmt.Println(string(output))
		return nil
	}

	// Pretty print
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

	return nil
}
