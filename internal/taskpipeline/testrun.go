package taskpipeline

import (
	"os"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameTestRun is the checklog entry for the project test suite actually
// being executed by forge (deterministic source). test-coverage-gate only
// checks that changed files came with matching test files (writing a test ≠
// running it); test-capability-scan only reports that runnable tests EXIST.
// This entry records that the suite RAN and its real exit code — the strongest
// counter to the "agent claims tests pass without running them" blind spot,
// because forge itself observed the result (unforgeable, unlike an agent claim).
const CheckNameTestRun checklog.CheckName = "test-run"

// DetectTestCommand returns the best-guess test command for the project's
// detected stack (e.g. "go test ./...", "cargo test", "npm test"), or "" when
// no stack/runner is recognized. Thin wrapper over the capability scanner's
// manifest sniffing so callers (forge verify --run-tests) don't reimplement it.
func DetectTestCommand(root string) string {
	_, cmd := detectStackAndCmd(root)
	return cmd
}

// RunTestCommand executes a command string in root and returns whether it
// succeeded (exit 0) plus its combined stdout/stderr. Pure execution — does
// NOT record to checklog; the caller decides whether/where to record so this
// stays unit-testable without touching disk. The command string is split on
// whitespace: every test command detectStackAndCmd produces is space-separated
// with no quoted args (go test ./..., cargo test, npx vitest run, ...).
func RunTestCommand(root, cmdStr string) (passed bool, output string) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return false, "empty command"
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.Dir = root
	c.Env = os.Environ()
	out, err := c.CombinedOutput()
	return err == nil, strings.TrimSpace(string(out))
}
