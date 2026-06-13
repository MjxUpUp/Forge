package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Harness/forge/internal/checklog"
	"github.com/Harness/forge/internal/toolusage"
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
		result, err := runAutoChecks(root, gateID, state)
		if err != nil {
			return nil, fmt.Errorf("auto-check failed: %w", err)
		}
		return result, nil
	}

	// For non-auto gates, just mark as passed
	// The AI agent is responsible for the actual work via SKILL.md instructions

	// Work activity check for non-auto gates.
	// Gates must not be passed without real work happening between them.
	// Skip for: completed tasks (re-verification) and the final gate (no work phase after it).
	// Note: we intentionally do NOT skip after auto gates. In the 3-gate pipeline,
	// task-verify follows the auto task-implement, and the implement→verify span is exactly
	// where read-before-edit must be enforced. Skipping after auto gates was a 5-gate-era
	// rule that left this check dead in the 3-gate flow (activity never ran).
	if !gate.Auto && state.CompletedAt == nil && len(state.History) > 0 && !isLastGate(gateID) {
		// Measure activity across the whole task span (since task start), not just
		// since the last gate. In the 3-gate pipeline the prior gate (task-implement)
		// is auto and instantaneous, so "since last gate" would see zero activity
		// even when the agent did substantial work earlier in the task.
		since := state.StartedAt

		if state.TaskRef != "" && !getDisableWorkActivity() {
			reads, edits, rerr := toolusage.ReadEditCounts(root, state.TaskRef, since)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "[forge] warning: activity check failed: %v\n", rerr)
			} else if reads+edits > 0 {
				// toollog has data — require at least one Read: the agent must have
				// understood the code before modifying it. Pure edit-without-read is
				// the failure mode. Edit-heavy work is allowed; the read/edit RATIO
				// is surfaced as an advisory WARN by the read-check hook, not a gate
				// (a strict ratio would block normal edit-heavy tasks).
				if reads == 0 {
					return nil, fmt.Errorf(
						"gate %q passed without reading any code during this task (edits=%d). "+
							"Read and understand the code before modifying it",
						gateID, edits,
					)
				}
			} else {
				// toollog empty (older project without tool-track) — fall back to checklog.
				activity, err := checklog.WorkActivity(root, state.TaskRef, since)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[forge] warning: WorkActivity check failed: %v\n", err)
				} else if activity < 1 {
					return nil, fmt.Errorf(
						"gate %q passed without sufficient work activity during this task (%d tool uses, minimum 1). "+
							"Read files, explore code, or write design notes before advancing",
						gateID, activity,
					)
				}
			}
		}
	}

	return &ExecuteResult{
		GateID:  gateID,
		Passed:  true,
		Message: fmt.Sprintf("%s - passed (verified by AI agent)", gate.Name),
	}, nil
}

// runAutoChecks executes automated checks for task gates.
func runAutoChecks(root string, gateID string, state *TaskState) (*ExecuteResult, error) {
	switch gateID {
	case "task-implement":
		return checkImplement(root, state)
	default:
		return &ExecuteResult{
			GateID:  gateID,
			Passed:  true,
			Message: "no auto-checks defined",
		}, nil
	}
}

// hasCodeChanges checks whether there are actual code changes since the task started.
// It checks working-tree changes and, on feature branches, new commits beyond the base branch.
// Gracefully degrades in non-git repos (returns true to avoid false positives).
func hasCodeChanges(root string, state *TaskState) bool {
	// Check 1: working-tree changes (including staged but uncommitted)
	cmd := exec.Command("git", "-C", root, "diff", "--stat", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return true // non-git repo - allow pass
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true
	}

	// Check 2: new commits on feature branch beyond base
	if state != nil && state.Branch != "" && state.Branch != "main" && state.Branch != "master" {
		for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
			cmd = exec.Command("git", "-C", root, "rev-list", "--count", base+"..HEAD")
			out, err = cmd.Output()
			if err == nil {
				return strings.TrimSpace(string(out)) != "0"
			}
		}
		// Could not find any base branch - allow pass
		return true
	}

	// On main/master with no uncommitted changes
	return false
}

// checkImplement runs compilation check via auto-compile.sh,
// assertion check via assertion-check.sh, records results to checklog,
// and verifies code changes exist.
func checkImplement(root string, state *TaskState) (*ExecuteResult, error) {
	taskRef := ""
	if state != nil {
		taskRef = state.TaskRef
	}

	// 1. Compilation check
	hookPath := filepath.Join(root, ".forge", "hooks", "auto-compile.sh")
	cmd := exec.Command("bash", hookPath, root)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	compilePassed := err == nil

	checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckAutoCompile,
		Passed:  compilePassed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  fmt.Sprintf("auto-compile.sh: %s", strings.TrimSpace(string(output))),
	})

	if !compilePassed {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: fmt.Sprintf("build failed: %s", string(output)),
		}, nil
	}

	// 2. Assertion weakening check (same script that Claude Code hook runs).
	assertHookPath := filepath.Join(root, ".forge", "hooks", "assertion-check.sh")
	if _, statErr := os.Stat(assertHookPath); statErr == nil {
		assertCmd := exec.Command("bash", assertHookPath, root)
		assertCmd.Dir = root
		// No per-file env vars - script runs in batch mode (checks all git diffs).
		assertOutput, assertErr := assertCmd.CombinedOutput()
		assertPassed := assertErr == nil

		checklog.Record(root, &checklog.Entry{
			Check:   checklog.CheckAssertion,
			Passed:  assertPassed,
			Checked: true,
			TaskRef: taskRef,
			Detail:  fmt.Sprintf("assertion-check.sh: %s", strings.TrimSpace(string(assertOutput))),
		})

		if !assertPassed {
			return &ExecuteResult{
				GateID:  "task-implement",
				Passed:  false,
				Message: fmt.Sprintf("assertion check failed: %s", string(assertOutput)),
			}, nil
		}
	}

	// 3. Verify actual code changes exist (not just a pre-compiled base).
	if !hasCodeChanges(root, state) {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: "no code changes detected - build passed but no files modified",
		}, nil
	}

	return &ExecuteResult{
		GateID:  "task-implement",
		Passed:  true,
		Message: "build passed, assertion check passed",
	}, nil
}

// getDisableWorkActivity returns whether work activity checking is disabled.
// Set FORGE_WORK_ACTIVITY=disable to skip the check (for testing only).
func getDisableWorkActivity() bool {
	return os.Getenv("FORGE_WORK_ACTIVITY") == "disable"
}

// isPreviousGateAuto returns true if the most recently passed gate is auto.
// Auto gates (e.g. task-implement) are instantaneous system checks - the next
// gate should not require work activity checks since no "work phase" elapsed.
func isPreviousGateAuto(state *TaskState) bool {
	if len(state.History) == 0 {
		return false
	}
	last := state.History[len(state.History)-1]
	g := GateByID(last.Gate)
	return g != nil && g.Auto
}

// isLastGate returns true if the given gate ID is the final gate in the pipeline.
// The final gate (task-complete) has no work phase after it, so
// work activity checks are skipped - there's nothing to "spend time on".
func isLastGate(gateID string) bool {
	gates := DefaultGates()
	return len(gates) > 0 && gates[len(gates)-1].ID == gateID
}
