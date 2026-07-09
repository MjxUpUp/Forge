package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskVerifyHook_SurfacesTestDisciplineAdvisory dogfood 4.2 + 2.1 reach test:
// task-verify Stop hook must surface the gate's test-coverage advisory (now carrying
// test-discipline guidance) on its stderr, rather than swallowing it. The four
// earlier string-literal tests nailed intent but missed the wiring: the gate
// subprocess's stderr was discarded by >/dev/null 2>&1 in the hook, so the
// formatMissing text (with test-discipline pointer) never entered MESSAGES.
// This test exercises the real path — forge init writes the hook from embed.go,
// task start, untested source, Read+Edit to satisfy activity check, pass
// task-implement prereq, then run the hook bash directly — and asserts the
// advisory actually reaches the hook's user-visible output.
//
// Why this channel matters (honest scope): Stop hook exit-0 stderr is shown to
// the user and recorded in checklog (forge trace), but is NOT agent-forced on
// exit-0 (the death-loop the team deliberately avoided). The DOMINANT path by
// which agents actually see test-discipline is the manual gate (forge task gate
// task-verify), where they run the command themselves and see the stderr. This
// test verifies the hook channel is at least complete (consistent with
// act-nudge), not that it force-injects; the manual-gate force is T3's
// formatMissing change which the gate already exercises.
func TestTaskVerifyHook_SurfacesTestDisciplineAdvisory(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/test-discipline-reach")
	const sid = "sess-td-reach"

	// Same env as tool_track_test: all forge/hook calls share session id so
	// task state and toollog resolve to the same active task.
	envWith := func() []string {
		return append(os.Environ(),
			"CLAUDE_CODE_SESSION_ID="+sid,
			"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(forgeBin, args...)
		cmd.Dir = dir
		cmd.Env = envWith()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("forge %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("task", "start", "--ref", "TD-REACH", "--title", "test-discipline hook reach")

	// Untested non-whitelisted source file. Valid Go so the project still
	// compiles. Must be git-tracked (git add) — task-implement's auto-checks
	// look at `git diff` for code changes; an untracked file is invisible to
	// that check and the gate fails with "no code changes detected".
	writeFile(t, dir, "internal/widget/click.go", "package widget\n\nfunc Click() {}\n")
	git(t, dir, "add", "internal/widget/click.go")

	// Populate toollog Read+Edit so task-verify clears the read-before-edit and
	// activity checks (lines executor.go:158-200). Without this, the gate fails
	// with "insufficient work activity" or "without reading any code" BEFORE
	// reaching CheckTestCoverage, and the test-discipline advisory never fires.
	forgeHook(t, dir, "tool-track", hookStdin(t, sid, "PostToolUse", "Read", map[string]any{
		"file_path": "internal/widget/click.go",
	}))
	forgeHook(t, dir, "auto-compile", hookStdin(t, sid, "PostToolUse", "Edit", map[string]any{
		"file_path": "internal/widget/click.go",
		"content":   "package widget\n\nfunc Click() {}\n",
	}))

	// Explicit prerequisite pass. tool_track_test relies on implicit
	// state that isn't documented; making this explicit keeps the test
	// self-sufficient and proofs against refactors of the auto-gate path.
	run("task", "gate", "task-implement", "--ref", "TD-REACH")

	// Run the task-verify Stop hook script directly. first invocation: no
	// throttle stamp → full execution → gate runs CheckTestCoverage → formatMissing
	// emits test-discipline to stderr → hook's new capture greps it into MESSAGES
	// → final stderr "[task-verify] Advisory (non-blocking): ...test-discipline...".
	hookPath := filepath.Join(dir, ".forge", "hooks", "task-verify.sh")
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLAUDE_CODE_SESSION_ID="+sid,
		"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("task-verify hook: %v\n%s", err, out)
	}

	// Reach assertion: the advisory (routed via MESSAGES) must contain the
	// skill pointer formatMissing injects. This is the behavioral contract
	// the four string-literal tests couldn't prove — the substring only
	// reaches this output if the hook captures stderr correctly.
	if !strings.Contains(string(out), "test-discipline") {
		t.Fatalf("task-verify hook must surface test-coverage advisory (containing test-discipline) on its output.\n"+
			"The hook is swallowing the gate's stderr — fix capture in embed.go TaskVerifyHook.\n---HOOK OUTPUT---\n%s", out)
	}
}
