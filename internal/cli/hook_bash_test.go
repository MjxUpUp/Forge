package cli

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The WSL-launcher recognition and bash-resolution tests (TestIsWSLBash /
// TestFindBash_SkipsWSL / TestFindBash_GitDerived) moved to
// internal/shellexec/bash_test.go together with the implementation when the
// resolution logic was shared with the gate path (taskpipeline.runEmbeddedHook).
// What remains here pins the cli-side wrappers and the fail-open output contract.

// exitErrorOf runs a trivial command that exits with the given code, producing a real
// *exec.ExitError for isHookInfraFailure tests.
func exitErrorOf(t *testing.T, code int) error {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit", strconv.Itoa(code))
	} else {
		cmd = exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
	}
	err := cmd.Run()
	if err == nil {
		t.Fatalf("command with exit %d returned nil error", code)
	}
	return err
}

// TestIsHookInfraFailure pins the fail-open boundary: spawn errors and bash 126/127 are
// infrastructure (fail open); script exit 1 (gate FAIL) and exit 2 (review-stop) remain
// gate verdicts.
func TestIsHookInfraFailure(t *testing.T) {
	if isHookInfraFailure(nil) {
		t.Error("nil error is not an infra failure")
	}
	if !isHookInfraFailure(exec.ErrNotFound) {
		t.Error("spawn error (non-ExitError) must be infra failure")
	}
	for _, code := range []int{126, 127} {
		if !isHookInfraFailure(exitErrorOf(t, code)) {
			t.Errorf("exit %d must be infra failure", code)
		}
	}
	for _, code := range []int{1, 2} {
		if isHookInfraFailure(exitErrorOf(t, code)) {
			t.Errorf("exit %d (script gate verdict) must NOT be infra failure", code)
		}
	}
}

// TestEmitInfraAllow pins the fail-open output contract: kimi gets plain stdout text +
// nil error (exit 0); claude gets an approve JSON envelope whose hookSpecificOutput
// carries the event name — without it Claude drops additionalContext and the warning
// would be silently lost.
func TestEmitInfraAllow(t *testing.T) {
	stdout, _, err := captureOutput(t, func() error {
		return emitInfraAllow("kimi", "PreToolUse", "[forge] hook x 基础设施失败，fail-open 放行")
	})
	if err != nil {
		t.Fatalf("kimi infra allow must return nil (exit 0), got %v", err)
	}
	if !strings.Contains(stdout, "基础设施失败") {
		t.Errorf("kimi warning must go to stdout, got %q", stdout)
	}

	stdout, _, err = captureOutput(t, func() error {
		return emitInfraAllow("", "PreToolUse", "warn")
	})
	if err != nil {
		t.Fatalf("claude infra allow must return nil, got %v", err)
	}
	var out HookOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("claude path must emit valid HookOutput JSON: %v\n%s", err, stdout)
	}
	if out.Decision != "approve" {
		t.Errorf("decision = %q, want approve", out.Decision)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookSpecificOutput.hookEventName missing/wrong: %+v", out.HookSpecificOutput)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.AdditionalContext != "warn" {
		t.Errorf("additionalContext missing: %+v", out.HookSpecificOutput)
	}
}
