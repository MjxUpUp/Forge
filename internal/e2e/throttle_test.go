package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskVerifyThrottle verifies the task-verify hook collapses repeated
// PostToolUse triggers via a 60s stamp. A legacy settings.local.json that
// mis-binds the hook to a wide Bash|Read|Glob matcher can fire it 100+ times
// per session (4 subshells each); the throttle suppresses that storm while
// leaving the Stop-time advisory intact (Stop intervals >> 60s).
func TestTaskVerifyThrottle(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/throttle-test")

	// Start a task but do NOT pass its gates. This makes task-verify's
	// "[task-gate] ... not yet passed" advisory fire on a non-throttled run,
	// giving a signal to distinguish executed vs throttled invocations.
	forge(t, dir, "task", "start", "--ref", "THROTTLE", "--title", "throttle test")

	runHook := func() string {
		t.Helper()
		hookPath := filepath.Join(dir, ".forge", "hooks", "task-verify.sh")
		cmd := exec.Command("bash", hookPath)
		cmd.Dir = dir
		binDir := filepath.Dir(forgeBin)
		cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, _ := cmd.CombinedOutput()
		return string(out)
	}

	// Run 1: no stamp yet → full execution → advisory about the unpassed gate.
	run1 := runHook()
	if !strings.Contains(run1, "not yet passed") {
		t.Fatalf("run 1 (unthrottled) should surface the unpassed-gate advisory, got: %q", run1)
	}

	// Run 2: immediately after → stamp fresh (<60s) → throttled → no forge
	// subprocess calls, no advisory.
	run2 := runHook()
	if strings.Contains(run2, "not yet passed") {
		t.Fatalf("run 2 (throttled) must not re-run forge checks, got advisory: %q", run2)
	}
	if strings.TrimSpace(run2) != "PASS" {
		t.Fatalf("run 2 (throttled) should output only PASS, got: %q", run2)
	}
}
