package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// forgeHook runs `forge hook <name>` as a subprocess, feeding the given stdin
// JSON — exactly what Claude Code does when it invokes a configured hook. This
// lets E2E tests exercise the real intercept path (runHook → embedded bash
// script → structured decision JSON) without a live Claude Code. The session_id
// is carried inside stdinJSON (see hookStdin) and scopes the hook's per-session
// state (active-task lookup, snapshot files); keep it unique per test.
//
// Returns (stdout, stderr, exitErr). It does NOT fatal — the caller decides
// whether a non-zero exit is expected (block) or a test failure.
func forgeHook(t *testing.T, dir, hookName, stdinJSON string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(forgeBin, "hook", hookName)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdinJSON)
	// Isolate TMPDIR so bash-guard/file-sentinel snapshot files don't collide
	// across tests or leak into the host /tmp.
	tmp := t.TempDir()
	binDir := filepath.Dir(forgeBin)
	cmd.Env = append(os.Environ(),
		"TMPDIR="+tmp,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// hookStdin builds the Claude Code HookInput JSON for a hook invocation.
// toolInput is marshalled into the tool_input field (file_path/content/command).
func hookStdin(t *testing.T, sessionID, eventName, toolName string, toolInput map[string]any) string {
	t.Helper()
	ti, _ := json.Marshal(toolInput)
	in := map[string]any{
		"session_id":      sessionID,
		"hook_event_name": eventName,
		"tool_name":       toolName,
		"tool_input":      json.RawMessage(ti),
	}
	b, _ := json.Marshal(in)
	return string(b)
}

// TestHook_TaskGuard_BlocksForgeManagedFile verifies the self-protection
// contract: task-guard must BLOCK any direct write to Forge-managed files
// (.forge/* except protocol/pipeline.yml, and .claude/settings*). This is the
// innermost safety ring — without it, an agent could disable its own oversight
// by editing state.json. No prior test exercised this via the real subprocess
// path (internal/cli/hook_test.go covers the JSON protocol in-process only).
func TestHook_TaskGuard_BlocksForgeManagedFile(t *testing.T) {
	dir := freshProject(t) // .forge/state.json exists after init

	in := hookStdin(t, "sess-selfprotect", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, ".forge", "state.json"),
		"content":   `{"hacked":true}`,
	})

	stdout, _, err := forgeHook(t, dir, "task-guard", in)

	// task-guard FAILs the managed-file write → runHook returns error → non-zero exit.
	if err == nil {
		t.Fatal("task-guard should block write to .forge/state.json, got exit 0")
	}
	// And emit the structured block decision Claude Code acts on.
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("task-guard stdout missing decision=block:\n%s", stdout)
	}
	// The block reason must identify the guard so the agent knows what tripped.
	if !strings.Contains(stdout, "task-guard") {
		t.Errorf("task-guard stdout missing guard identifier in additionalContext:\n%s", stdout)
	}
}

// TestHook_HazardGuard_BlocksHazardousCommand verifies the on-demand-guards
// auto-tier: hazard-guard must BLOCK destructive commands (rm -rf / git push
// --force / DROP TABLE / kubectl delete / DELETE without WHERE) and emit the
// HITL guidance pointing at `forge hazard confirm` as the escape hatch. bash-guard
// only watches for write-via-shell patterns and is blind to these — hazard-guard
// closes that gap.
func TestHook_HazardGuard_BlocksHazardousCommand(t *testing.T) {
	dir := freshProject(t)
	const hazardous = "rm -rf /tmp/forge-hazard-test"

	in := hookStdin(t, "sess-hazard-block", "PreToolUse", "Bash", map[string]any{
		"command": hazardous,
	})

	stdout, _, err := forgeHook(t, dir, "hazard-guard", in)

	if err == nil {
		t.Fatal("hazard-guard should block 'rm -rf', got exit 0")
	}
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("expected decision=block, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "hazard-guard") {
		t.Errorf("stdout missing hazard-guard identifier:\n%s", stdout)
	}
	// HITL guidance must point the agent at the escape hatch — without this the
	// agent has no way to proceed on a legitimately-needed destructive op.
	if !strings.Contains(stdout, "forge hazard confirm") {
		t.Errorf("stdout missing HITL confirm guidance:\n%s", stdout)
	}
	// Fingerprint-drift warning (ce9b2410 lesson): agents retry with && echo / && ls
	// verification suffixes, which rewrites the command string → new hash → re-blocked
	// despite confirm. The block hint must warn "逐字重试" (verbatim retry). Anchored on
	// "逐字" not "&& echo" because stdout is JSON-encoded (the & literal is escaped),
	// and a plain keyword won't false-fail on rewording.
	if !strings.Contains(stdout, "逐字") {
		t.Errorf("stdout missing fingerprint-drift warning (verbatim retry hint):\n%s", stdout)
	}
}

// TestHook_HazardGuard_ConfirmReleases verifies the human-in-the-loop loop
// closes: a blocked command is unblocked after `forge hazard confirm` registers
// the 5-min mark. This is the "confirm → retry → pass" path that makes the gate
// HITL rather than a hard wall.
func TestHook_HazardGuard_ConfirmReleases(t *testing.T) {
	dir := freshProject(t)
	const hazardous = "git push --force origin main"

	in := hookStdin(t, "sess-hazard-confirm", "PreToolUse", "Bash", map[string]any{
		"command": hazardous,
	})

	// 1. Pre-confirm: blocked.
	if stdout, _, err := forgeHook(t, dir, "hazard-guard", in); err == nil {
		t.Fatalf("hazard-guard should block 'git push --force' pre-confirm, got exit 0\n%s", stdout)
	}

	// 2. Register the HITL confirmation (the escape hatch the guidance points at).
	confirm := exec.Command(forgeBin, "hazard", "confirm", hazardous)
	confirm.Dir = dir
	if out, err := confirm.CombinedOutput(); err != nil {
		t.Fatalf("forge hazard confirm failed: %v\n%s", err, out)
	}

	// 3. Retry the same command: now passes within the 5-min window.
	stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
	if err != nil {
		t.Fatalf("hazard-guard should pass post-confirm, got error. stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("expected decision=approve post-confirm, got:\n%s", stdout)
	}
}

// TestHook_HazardGuard_FingerprintReleases verifies the --fingerprint path the hook
// guidance now points at: hook echoes a hex fingerprint, the agent confirms by
// fingerprint (not command string) and retries. This is the robust path for commands
// containing quotes (e.g. SQL `mysql -e 'DROP TABLE t'`) — a command-string confirm
// would have its quotes eaten by the agent's shell re-parsing, diverging from the
// hook's fingerprint and leaving the command blocked.
func TestHook_HazardGuard_FingerprintReleases(t *testing.T) {
	dir := freshProject(t)
	const hazardous = "mysql -e 'DROP TABLE users'" // contains single quotes

	in := hookStdin(t, "sess-hazard-fp", "PreToolUse", "Bash", map[string]any{
		"command": hazardous,
	})

	// 1. Pre-confirm: blocked; guidance must point at --fingerprint (hex, no quote loss).
	stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
	if err == nil {
		t.Fatalf("hazard-guard should block SQL DROP pre-confirm, got exit 0\n%s", stdout)
	}
	if !strings.Contains(stdout, "forge hazard confirm --fingerprint") {
		t.Fatalf("guidance must point at --fingerprint, got:\n%s", stdout)
	}

	// 2. Compute the same fingerprint the hook uses (forge hazard fingerprint <cmd>).
	fpOut, err := exec.Command(forgeBin, "hazard", "fingerprint", hazardous).Output()
	if err != nil {
		t.Fatalf("forge hazard fingerprint: %v", err)
	}
	fp := strings.TrimSpace(string(fpOut))

	// 3. Agent confirms by fingerprint — exactly what the hook told it to do.
	confirm := exec.Command(forgeBin, "hazard", "confirm", "--fingerprint", fp)
	confirm.Dir = dir
	if out, err := confirm.CombinedOutput(); err != nil {
		t.Fatalf("forge hazard confirm --fingerprint failed: %v\n%s", err, out)
	}

	// 4. Retry: passes within the 5-min window.
	stdout, _, err = forgeHook(t, dir, "hazard-guard", in)
	if err != nil {
		t.Fatalf("hazard-guard should pass post-confirm, got error. stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("expected decision=approve post-confirm, got:\n%s", stdout)
	}
}
