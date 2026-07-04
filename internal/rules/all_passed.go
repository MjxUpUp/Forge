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
	gatesDir := ctx.GatesDir
	if gatesDir == "" {
		// Defensive: callers should fill GatesDir (DataDir/gates). Empty means
		// a caller hasn't migrated — fall back so we don't silently pass, but
		// emit an observable stderr warning so the missing field isn't hidden
		// (a silent fallback here masked the status.go path divergence in C1).
		fmt.Fprintf(os.Stderr, "[rules/all_gates_passed] warning: Context.GatesDir empty (caller forgot to set it); falling back to %s. Gate status may be read from the wrong location after the data-home migration.\n", filepath.Join(ctx.ProjectRoot, ".forge", "gates"))
		gatesDir = filepath.Join(ctx.ProjectRoot, ".forge", "gates")
	}

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
