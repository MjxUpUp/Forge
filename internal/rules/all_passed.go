package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AllGatesPassedEvaluator checks that all preceding gates have passed.
type AllGatesPassedEvaluator struct{}

func (e *AllGatesPassedEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	gatesDir := filepath.Join(ctx.ProjectRoot, ".forge", "gates")

	entries, err := os.ReadDir(gatesDir)
	if err != nil {
		return Result{
			Name:   "all_gates_passed",
			Passed: true,
			Detail: "no gates directory found",
		}
	}

	enabledSet := make(map[string]bool)
	for _, id := range ctx.EnabledGateIDs {
		enabledSet[id] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == ctx.GateID {
			continue
		}
		if !enabledSet[entry.Name()] {
			continue // skip disabled or unknown gates
		}

		statusPath := filepath.Join(gatesDir, entry.Name(), "status.json")
		data, err := os.ReadFile(statusPath)
		if err != nil {
			return Result{
				Name:    "all_gates_passed",
				Passed:  false,
				Detail:  fmt.Sprintf("gate %s has no status.json (never run)", entry.Name()),
				Message: fmt.Sprintf("Gate %s has not been executed", entry.Name()),
			}
		}

		var status struct {
			Passed bool `json:"passed"`
		}
		if err := json.Unmarshal(data, &status); err != nil {
			return Result{
				Name:    "all_gates_passed",
				Passed:  false,
				Detail:  fmt.Sprintf("gate %s has corrupted status.json: %v", entry.Name(), err),
				Message: fmt.Sprintf("Gate %s status file is corrupted", entry.Name()),
			}
		}
		if !status.Passed {
			return Result{
				Name:    "all_gates_passed",
				Passed:  false,
				Detail:  fmt.Sprintf("gate %s did not pass", entry.Name()),
				Message: fmt.Sprintf("Prerequisite gate %s did not pass", entry.Name()),
			}
		}
	}
	return Result{
		Name:   "all_gates_passed",
		Passed: true,
		Detail: "all gates passed",
	}
}
