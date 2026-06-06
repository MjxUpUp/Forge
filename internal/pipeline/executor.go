package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Harness/forge/internal/rules"
)

// GateExecResult is the outcome of executing a single gate.
type GateExecResult struct {
	Status   *GateStatus
	Duration time.Duration
}

// ExecuteGate runs hooks + rule evaluation for a single gate.
// This is the ONLY place gate execution happens — no duplication.
func ExecuteGate(root string, gate *Gate, state *State, pipeline *Pipeline, force bool) (*GateExecResult, error) {
	gateDir := filepath.Join(root, ".forge", "gates", gate.ID)
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create gate dir: %w", err)
	}

	// 1. Check prerequisites (unless --force)
	if !force && len(gate.DependsOn) > 0 {
		if err := state.PrerequisitesPassed(gate.DependsOn); err != nil {
			return nil, fmt.Errorf("prerequisites not met: %w\nUse --force to skip", err)
		}
	}

	// 2. Mark gate as started
	state.MarkGateStarted(gate.ID)
	if force {
		state.AddOverride(gate.ID, "--force")
	}

	startTime := time.Now()

	// 3. Execute hooks
	hookErrors := executeHooks(root, gate)

	// 4. Build rule evaluation context
	enabledIDs := make([]string, 0)
	for _, g := range pipeline.EnabledGates() {
		enabledIDs = append(enabledIDs, g.ID)
	}
	ctx := rules.Context{
		GateDir:        gateDir,
		ProjectRoot:    root,
		GateID:         gate.ID,
		EnabledGateIDs: enabledIDs,
	}

	// 5. Evaluate checks
	ruleResults := rules.EvaluateChecks(ctx, gate.Checks)

	duration := time.Since(startTime)
	durationMs := duration.Milliseconds()

	// 6. Assemble status
	attempt := state.AttemptCount(gate.ID) + 1
	status := &GateStatus{
		Gate:            gate.ID,
		Attempt:         attempt,
		Timestamp:       time.Now(),
		DurationSeconds: duration.Seconds(),
		DurationMs:      durationMs,
		Mode:            state.Mode,
		InputArtifacts:  gate.Artifacts.Inputs,
		OutputArtifacts: gate.Artifacts.Outputs,
	}

	for _, r := range ruleResults {
		status.Checks = append(status.Checks, CheckResult{
			Name:   r.Name,
			Type:   r.Type,
			Passed: r.Passed,
			Detail: r.Detail,
		})
		if !r.Passed {
			status.Errors = append(status.Errors, CheckError{
				Check:   r.Name,
				Message: r.Message,
			})
		}
	}

	for _, e := range hookErrors {
		status.Errors = append(status.Errors, CheckError{
			Check:   "hook",
			Message: e.Error(),
		})
	}

	status.Passed = len(hookErrors) == 0 && len(status.Errors) == 0

	// 7. Save status.json
	if err := SaveStatus(root, gate.ID, status); err != nil {
		return nil, err
	}

	// 8. Update state.json
	state.MarkGateComplete(gate.ID, status.Passed, durationMs)
	if err := state.Save(root); err != nil {
		return nil, err
	}

	return &GateExecResult{Status: status, Duration: duration}, nil
}

// executeHooks runs all hook scripts for a gate.
func executeHooks(root string, gate *Gate) []error {
	var errs []error
	for _, hookName := range gate.Hooks {
		hookPath := filepath.Join(root, ".forge", "hooks", hookName)

		if _, err := os.Stat(hookPath); os.IsNotExist(err) {
			continue // Skip missing hooks silently
		}

		cmd := exec.Command(shell(), hookPath)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Errorf("hook '%s': %w\n%s", hookName, err, string(output)))
		}
	}
	return errs
}

// shell returns the shell command for the current platform.
func shell() string {
	if runtime.GOOS == "windows" {
		return "sh"
	}
	return "bash"
}
