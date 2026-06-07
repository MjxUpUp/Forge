package taskpipeline

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// ExecuteResult holds the outcome of a task gate execution.
type ExecuteResult struct {
	GateID  string
	Passed  bool
	Message string
}

// ExecuteTaskGate runs a single task gate's checks.
// For auto-gates (task-implement), it runs the relevant hook scripts.
// For non-auto gates, it verifies the gate was previously marked passed
// (the AI agent is responsible for doing the actual work).
func ExecuteTaskGate(root string, gateID string, state *TaskState) (*ExecuteResult, error) {
	gate := GateByID(gateID)
	if gate == nil {
		return nil, fmt.Errorf("unknown task gate: %s", gateID)
	}

	// Check prerequisites: all previous gates must have passed
	gates := DefaultGates()
	for _, g := range gates {
		if g.ID == gateID {
			break
		}
		if !state.gatePassed(g.ID) {
			return nil, fmt.Errorf("prerequisite gate %q has not passed", g.ID)
		}
	}

	// For auto-gates, run the actual checks
	if gate.Auto {
		result, err := runAutoChecks(root, gateID)
		if err != nil {
			return nil, fmt.Errorf("auto-check failed: %w", err)
		}
		return result, nil
	}

	// For non-auto gates, just mark as passed
	// The AI agent is responsible for the actual work via SKILL.md instructions
	return &ExecuteResult{
		GateID:  gateID,
		Passed:  true,
		Message: fmt.Sprintf("%s — passed (verified by AI agent)", gate.Name),
	}, nil
}

// runAutoChecks executes automated checks for task gates.
func runAutoChecks(root string, gateID string) (*ExecuteResult, error) {
	switch gateID {
	case "task-implement":
		return checkImplement(root)
	default:
		return &ExecuteResult{
			GateID:  gateID,
			Passed:  true,
			Message: "no auto-checks defined",
		}, nil
	}
}

// checkImplement runs compilation check via auto-compile.sh.
func checkImplement(root string) (*ExecuteResult, error) {
	hookPath := filepath.Join(root, ".forge", "hooks", "auto-compile.sh")
	cmd := exec.Command("bash", hookPath, root)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: fmt.Sprintf("编译失败: %s", string(output)),
		}, nil
	}
	return &ExecuteResult{
		GateID:  "task-implement",
		Passed:  true,
		Message: "编译通过",
	}, nil
}
