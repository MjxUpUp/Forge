package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestToolTrackRecordsReadForGate verifies the tool-track hook restores Read
// recording so the read-before-edit gate at task-verify works again.
//
// Background: 644b142 removed the tool-track hook (alongside the untrusted
// tool-selection dimension). That hook was also the ONLY source of Read entries
// in toollog, so the gate (added in dcab4d4) lost its data and became
// always-failing on any task with edits. This test simulates a Read+Edit pair
// through the real hook dispatch and asserts the gate no longer reports
// "without reading any code".
func TestToolTrackRecordsReadForGate(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/tool-track-test")
	const sid = "sess-tooltrack"

	// All forge commands share CLAUDE_CODE_SESSION_ID so task state and hook
	// records resolve to the same active task (the per-session key).
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

	run("task", "start", "--ref", "TT", "--title", "tool-track test")

	// Simulate a Read — tool-track records it to toollog.
	forgeHook(t, dir, "tool-track", hookStdin(t, sid, "PostToolUse", "Read", map[string]any{
		"file_path": "main.go",
	}))
	// Simulate an Edit — auto-compile records the edit. This puts toollog in the
	// reads+edits>0 branch where the gate previously failed on reads==0.
	forgeHook(t, dir, "auto-compile", hookStdin(t, sid, "PostToolUse", "Edit", map[string]any{
		"file_path": "main.go",
		"content":   "package main\n",
	}))

	// task-verify gate must NOT report the read-check failure now.
	cmd := exec.Command(forgeBin, "task", "gate", "task-verify", "--ref", "TT")
	cmd.Dir = dir
	cmd.Env = envWith()
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "without reading any code") {
		t.Fatalf("task-verify still reports read-check failure after tool-track recorded Read:\n%s", out)
	}

	// Sanity: toollog actually holds a Read entry for this task (the data the
	// gate read). Confirms tool-track restored the capability 644b142 removed.
	// toollog migrated to user-level DataDir (refactor-data-home); the forge
	// subprocess (toolusage.Record) writes there for git projects.
	toollog, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "toollog.jsonl"))
	if err != nil {
		t.Fatalf("read toollog: %v", err)
	}
	if !strings.Contains(string(toollog), `"tool_name":"Read"`) {
		t.Errorf("toollog has no Read entry — tool-track did not record it:\n%s", toollog)
	}
}
