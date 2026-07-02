package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runForgeStreams runs the pre-built forge binary capturing stdout and stderr
// SEPARATELY. The package-level runForge uses cmd.CombinedOutput, which merges
// the streams and can't tell them apart — so it cannot verify the contract that
// the non-git warning goes to stderr (keeping --json stdout machine-parseable).
func runForgeStreams(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(forgeExe, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return out.String(), errb.String(), exitErr.ExitCode()
		}
		return out.String(), errb.String() + err.Error(), -1
	}
	return out.String(), errb.String(), 0
}

// TestTaskStart_NonGitWarningOnStderr guards the degraded-mode signal that a
// stranded code-knowledge-base-style session lacked. In a non-git project,
// `task start` must print the non-git warning to stderr so --json output on
// stdout stays machine-parseable. The warning must name the dimensions that
// lose fidelity (scope) and the abort escape hatch, so an agent
// doesn't read neutral scores as a broken pipeline.
func TestTaskStart_NonGitWarningOnStderr(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	tmpDir := t.TempDir()
	// Deliberately NO git init — this is the non-git case.

	// forge init creates .forge/ without requiring git.
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	stdout, stderr, code := runForgeStreams(t, tmpDir, "task", "start", "--ref", "feat/nongit", "--title", "non-git probe")
	if code != 0 {
		t.Fatalf("forge task start failed: %s", stderr)
	}

	for _, want := range []string{"不是 git 仓库", "scope", "git init", "forge task abort"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("non-git task start stderr missing %q\nstderr: %s", want, stderr)
		}
	}
	// The warning must not leak into stdout (would corrupt --json consumers).
	if strings.Contains(stdout, "不是 git 仓库") {
		t.Errorf("non-git warning leaked into stdout (corrupts --json): %s", stdout)
	}

	// Cleanup so the state file doesn't linger.
	runForge(t, tmpDir, "task", "abort", "--ref", "feat/nongit")
}

// TestTaskStart_NonGitJSONStdoutClean confirms the warning goes to stderr even
// in --json mode, leaving stdout valid JSON. This is the contract programmatic
// callers depend on.
func TestTaskStart_NonGitJSONStdoutClean(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	tmpDir := t.TempDir()

	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	stdout, stderr, code := runForgeStreams(t, tmpDir, "task", "start", "--ref", "feat/jsonclean", "--json")
	if code != 0 {
		t.Fatalf("forge task start --json failed: %s", stderr)
	}
	// stdout must be parseable JSON, not warning text.
	if !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Errorf("--json stdout is not JSON (warning leaked?): %q", stdout)
	}
	if !strings.Contains(stderr, "不是 git 仓库") {
		t.Errorf("non-git warning missing from stderr in --json mode: %s", stderr)
	}

	runForge(t, tmpDir, "task", "abort", "--ref", "feat/jsonclean")
}

// TestTaskStart_GitRepoNoWarning confirms a real git project does NOT emit the
// non-git warning — the signal is reserved for the degraded case.
func TestTaskStart_GitRepoNoWarning(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "t@t.com")
	runGit(t, tmpDir, "config", "user.name", "T")
	runGit(t, tmpDir, "commit", "--allow-empty", "-m", "init")
	runGit(t, tmpDir, "checkout", "-b", "feat/gitcase")

	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	_, stderr, code := runForgeStreams(t, tmpDir, "task", "start", "--ref", "feat/gitcase", "--title", "git project")
	if code != 0 {
		t.Fatalf("forge task start failed: %s", stderr)
	}
	if strings.Contains(stderr, "不是 git 仓库") {
		t.Errorf("git project emitted non-git warning (false positive): %s", stderr)
	}
	// Sanity: the .forge state dir exists (forge init ran).
	if _, err := os.Stat(filepath.Join(tmpDir, ".forge")); err != nil {
		t.Fatalf(".forge dir missing after init: %v", err)
	}
}

// TestNonGitTaskWarning_ContainsKeyInfo guards the warning text against losing
// the information an agent needs in degraded mode: which dimensions lose
// fidelity, how to get full quality (git init), and how to bail (abort).
func TestNonGitTaskWarning_ContainsKeyInfo(t *testing.T) {
	w := nonGitTaskWarning()
	for _, want := range []string{"git", "scope", "git init", "forge task abort"} {
		if !strings.Contains(w, want) {
			t.Errorf("nonGitTaskWarning() missing %q", want)
		}
	}
}
