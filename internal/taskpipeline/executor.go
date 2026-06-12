package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Harness/forge/internal/checklog"
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

	// Timing + work activity check for non-auto gates.
	// Gates must not be passed without real work happening between them.
	// This prevents agents from using sleep to bypass timing requirements.
	// Skip for: completed tasks (re-verification), final gate (no work phase after it).
	if !gate.Auto && state.CompletedAt == nil && len(state.History) > 0 && !isLastGate(gateID) {
		lastResult := state.History[len(state.History)-1]
		minInterval := getGateMinInterval()
		elapsed := time.Since(lastResult.CompletedAt)
		if elapsed < minInterval {
			return nil, fmt.Errorf(
				"gate %q passed too quickly after %q (%.0fs elapsed, minimum %v). "+
					"Each gate represents a distinct work phase — spend time on it before advancing",
				gateID, lastResult.Gate, elapsed.Seconds(), minInterval,
			)
		}

		// Work activity check: verify actual tool usage (Read, Write, Edit, Grep, etc.)
		// since the last gate. If only Bash/sleep was used, the agent didn't do real work.
		// Independent of minInterval â even with timing disabled, substance is still checked.
		if state.TaskRef != "" && !getDisableWorkActivity() {
			activity, _ := checklog.WorkActivity(root, state.TaskRef, lastResult.CompletedAt)
			if activity < 2 {
				return nil, fmt.Errorf(
					"gate %q passed without sufficient work activity since %q (%d tool uses, minimum 2). "+
						"Read files, explore code, or write design notes before advancing",
					gateID, lastResult.Gate, activity,
				)
			}
		}
	}

	return &ExecuteResult{
		GateID:  gateID,
		Passed:  true,
		Message: fmt.Sprintf("%s — passed (verified by AI agent)", gate.Name),
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
		return true // non-git repo — allow pass
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
		// Could not find any base branch — allow pass
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
			Message: fmt.Sprintf("编译失败: %s", string(output)),
		}, nil
	}

	// 2. Assertion weakening check (same script that Claude Code hook runs).
	assertHookPath := filepath.Join(root, ".forge", "hooks", "assertion-check.sh")
	if _, statErr := os.Stat(assertHookPath); statErr == nil {
		assertCmd := exec.Command("bash", assertHookPath, root)
		assertCmd.Dir = root
		// No per-file env vars → script runs in batch mode (checks all git diffs).
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
				Message: fmt.Sprintf("断言检查失败: %s", string(assertOutput)),
			}, nil
		}
	}

	// 3. Verify actual code changes exist (not just a pre-compiled base).
	if !hasCodeChanges(root, state) {
		return &ExecuteResult{
			GateID:  "task-implement",
			Passed:  false,
			Message: "未检测到代码变更 — 编译通过但未修改任何文件",
		}, nil
	}

	// 4. Verify code was written AFTER task-design was passed.
	// This prevents agents from writing all code first, then rushing through gates.
	if state != nil {
		designCommit := state.designGateCommit()
		if designCommit != "" {
			headCmd := exec.Command("git", "-C", root, "rev-parse", "HEAD")
			headOut, headErr := headCmd.Output()
			if headErr == nil {
				currentHead := strings.TrimSpace(string(headOut))
				if currentHead == designCommit {
					return &ExecuteResult{
						GateID:  "task-implement",
						Passed:  false,
						Message: "HEAD 未移动：代码在 task-design 通过后没有新提交。先写代码再过 task-implement gate",
					}, nil
				}
			}
		}
	}

	return &ExecuteResult{
		GateID:  "task-implement",
		Passed:  true,
		Message: "编译通过，断言检查通过",
	}, nil
}

// getGateMinInterval returns the minimum time required between consecutive
// non-auto gate passes. Configurable via FORGE_GATE_MIN_INTERVAL env var
// (e.g. "30s", "2m"). Default: 60 seconds.
func getGateMinInterval() time.Duration {
	if env := os.Getenv("FORGE_GATE_MIN_INTERVAL"); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			return d
		}
	}
	return 60 * time.Second
}

// getDisableWorkActivity returns whether work activity checking is disabled.
// Set FORGE_WORK_ACTIVITY=disable to skip the check (for testing only).
func getDisableWorkActivity() bool {
	return os.Getenv("FORGE_WORK_ACTIVITY") == "disable"
}

// isLastGate returns true if the given gate ID is the final gate in the pipeline.
// The final gate (task-complete) has no work phase after it, so timing and
// work activity checks are skipped — there's nothing to "spend time on".
func isLastGate(gateID string) bool {
	gates := DefaultGates()
	return len(gates) > 0 && gates[len(gates)-1].ID == gateID
}
