package cli

import (
	"fmt"
	"os"

	"github.com/Harness/forge/internal/pipeline"
	"github.com/Harness/forge/internal/skillgen"
	"github.com/Harness/forge/internal/snapshot"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(snapshotCmd)
}

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "检测项目开发阶段，推断已完成的门禁",
	Long: `forge snapshot 扫描项目文件和 Git 历史，
推断哪些门禁已经完成，将推断结果写入 state.json。

适用于：
  - 已有项目首次使用 Forge（替代重新 forge init）
  - 更新 Forge 后补推断
  - 重新评估项目当前阶段

不会修改 pipeline.yml 或清除已有的执行历史。`,
	RunE: runSnapshot,
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Must be a forge project
	p, err := pipeline.Load(dir)
	if err != nil {
		return fmt.Errorf("not a forge project: %w\n\nRun 'forge init' first", err)
	}

	state, err := pipeline.LoadState(dir)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Take snapshot
	snap, err := snapshot.Take(dir)
	if err != nil {
		return fmt.Errorf("failed to scan project: %w", err)
	}

	// Infer completed gates
	inferredGates := snapshot.InferCompletedGates(snap, p)

	fmt.Println("Detected project signals:")
	fmt.Println(snapshot.FormatSignals(&snap.Signals))

	if len(inferredGates) == 0 {
		fmt.Println()
		fmt.Println("No gates inferred — pipeline starts from the beginning.")
		return nil
	}

	// Check which inferred gates are not already overridden or completed
	completed := state.CompletedGates()
	newCount := 0
	for _, ig := range inferredGates {
		// Skip if already overridden
		if state.IsOverridden(ig.GateID) {
			continue
		}
		// Skip if already passed in history
		if completed[ig.GateID] {
			continue
		}
		state.AddOverride(ig.GateID, "auto-detected: "+ig.Reason)
		newCount++
	}

	// Update snapshot data
	inferredIDs := make([]string, 0)
	for _, ig := range inferredGates {
		inferredIDs = append(inferredIDs, ig.GateID)
	}
	state.Snapshot = &pipeline.SnapshotData{
		TakenAt:       snap.TakenAt,
		InferredGates: inferredIDs,
	}

	// Save state
	if err := state.Save(dir); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Regenerate skill with artifact fallback
	if err := skillgen.GenerateSkill(dir, p, inferredIDs); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to regenerate skill: %v\n", err)
	}

	fmt.Println()
	fmt.Println("Inferred completed gates:")
	fmt.Println(snapshot.FormatInferred(inferredGates))

	// Find starting gate
	allCompleted := state.CompletedGates()
	for _, ig := range inferredGates {
		allCompleted[ig.GateID] = true
	}
	nextGate := p.NextReadyGate(allCompleted)
	fmt.Println()
	if nextGate != "" {
		if gate, err := p.GetGate(nextGate); err == nil {
			fmt.Printf("Pipeline starts from: %s (%s)\n", nextGate, gate.Name)
		} else {
			fmt.Printf("Pipeline starts from: %s\n", nextGate)
		}
	} else {
		fmt.Println("All gates inferred as completed — pipeline is fully done.")
	}

	if newCount > 0 {
		fmt.Println()
		fmt.Printf("Updated state.json (%d new overrides added)\n", newCount)
	}

	return nil
}
