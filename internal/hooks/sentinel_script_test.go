package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Script-level tests for the bash-guard → file-sentinel pair: the embedded
// scripts are written to temp files and executed with real bash against a real
// git repo, following the mcpscan_test pattern. No forge binary is needed —
// quarantine is redirected via FORGE_QUARANTINE_DIR and the grace branch's
// `forge data-dir` fails silently when absent (or finds no marker when present).
//
// Covered fixes:
//   - task1: gitignored config (.forge/hooks/*, .claude/settings*) protected via
//     the git-independent .cfg manifest.
//   - task2: staged blind spot — `write && git add` no longer escapes.
//   - task3: .ok completion marker — clean baseline proceeds, markerless
//     snapshot fails open.
//   - task4: mv-failure in quarantine still restores a deleted tracked file.

func sentinelEnv(t *testing.T, sid, tmpdir, qdir string) []string {
	t.Helper()
	// Base env minus the keys we override, then the overrides (so bash/git keep
	// working while FORGE_*/TMPDIR are pinned).
	over := map[string]string{
		"FORGE_SESSION_ID":     sid,
		"FORGE_TASK_REF":       "",
		"TMPDIR":               tmpdir,
		"FORGE_QUARANTINE_DIR": qdir,
	}
	out := []string{}
	for _, kv := range os.Environ() {
		k := kv[:strings.Index(kv, "=")]
		if _, dup := over[k]; dup {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range over {
		out = append(out, k+"="+v)
	}
	return out
}

// runSentinelScript executes one embedded hook script in repoDir.
func runSentinelScript(t *testing.T, script, repoDir string, env []string) (string, error) {
	t.Helper()
	f, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	if _, err := f.WriteString(script); err != nil {
		t.Fatalf("write script: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	cmd := exec.Command("bash", f.Name())
	cmd.Dir = repoDir
	cmd.Env = env
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

func sentinelRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH — skipping script-level sentinel test")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH — skipping script-level sentinel test")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init")
	git("add", ".")
	return dir
}

func sentinelCommit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init", "--allow-empty")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// TestSentinelScripts_StagedWriteDoesNotEscape (task 2): before the fix, the
// snapshot and the current-state collection only used `git diff` (unstaged) +
// untracked files, so `write && git add` in one command escaped detection
// entirely. Both sides now include `git diff --cached --name-only` and the
// staged write must be quarantined.
func TestSentinelScripts_StagedWriteDoesNotEscape(t *testing.T) {
	dir := sentinelRepo(t)
	const sid = "sess-staged"
	tmp := t.TempDir()
	qdir := t.TempDir()
	env := sentinelEnv(t, sid, tmp, qdir)

	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND=cat > staged.go && git add staged.go")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}

	target := filepath.Join(dir, "staged.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "staged.go")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	out, err := runSentinelScript(t, FileSentinelHook, dir, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on staged source write with no task, got PASS:\n%s", out)
	}
	if !strings.Contains(out, "staged.go") {
		t.Errorf("file-sentinel output must name the staged file:\n%s", out)
	}
	if _, qerr := os.Stat(filepath.Join(qdir, sid, "staged.go")); qerr != nil {
		t.Errorf("staged.go must be quarantined (staged blind spot fix): %v\noutput:\n%s", qerr, out)
	}
}

// TestSentinelScripts_GitignoredConfigDetected (task 1): .claude/settings* and
// .forge/hooks/* are gitignored and untracked, so the git-based lists never
// reported Bash rewrites of them — the CONFIG quarantine branch was blind on
// exactly its primary targets. The git-independent .cfg manifest must surface
// the rewrite and route it to the CONFIG quarantine branch.
func TestSentinelScripts_GitignoredConfigDetected(t *testing.T) {
	dir := sentinelRepo(t)
	const sid = "sess-gitignored-cfg"
	tmp := t.TempDir()
	qdir := t.TempDir()

	// gitignore the self-protection targets — exactly the real-world layout.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".forge/\n.claude/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, ".claude", "settings.local.json")
	if err := os.WriteFile(settings, []byte(`{"hooks":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	sentinelCommit(t, dir)

	env := sentinelEnv(t, sid, tmp, qdir)
	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND=cat > .claude/settings.local.json")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}

	// Bash rewrites the gitignored config — invisible to git diff/ls-files.
	if err := os.WriteFile(settings, []byte(`{"hooks":{"PreToolUse":[]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := runSentinelScript(t, FileSentinelHook, dir, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on gitignored config rewrite, got PASS:\n%s", out)
	}
	if !strings.Contains(out, "config") {
		t.Errorf("file-sentinel output must route through the CONFIG branch:\n%s", out)
	}
	if _, qerr := os.Stat(filepath.Join(qdir, sid, ".claude", "settings.local.json")); qerr != nil {
		t.Errorf("gitignored settings.local.json must be quarantined via the .cfg manifest: %v\noutput:\n%s", qerr, out)
	}
}

// TestSentinelScripts_CleanBaselineFirstWriteQuarantined (task 3): a clean
// working tree yields a legitimately EMPTY snapshot. Before the .ok marker,
// "empty snapshot + non-empty current" was always treated as a silently failed
// snapshot and fail-opened — so the FIRST Bash source write in a clean repo
// was never quarantined. With the completion marker, the normal NEW_CHANGES
// flow must run.
func TestSentinelScripts_CleanBaselineFirstWriteQuarantined(t *testing.T) {
	dir := sentinelRepo(t)
	sentinelCommit(t, dir) // clean tree: HEAD exists, nothing uncommitted
	const sid = "sess-clean-baseline"
	tmp := t.TempDir()
	qdir := t.TempDir()
	env := sentinelEnv(t, sid, tmp, qdir)

	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND=cat > new.go")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}
	// Snapshot must be empty (clean tree) AND carry the completion marker.
	entries, err := filepath.Glob(filepath.Join(tmp, "forge-snapshot-"+sid+"-*"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("per-invocation snapshot not found in %s", tmp)
	}
	var snap, okMarker string
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e, ".ok"):
			okMarker = e
		case strings.HasSuffix(e, ".cfg"):
		default:
			snap = e
		}
	}
	if okMarker == "" {
		t.Errorf("bash-guard must write the .ok completion marker next to the snapshot")
	}
	if data, _ := os.ReadFile(snap); len(data) != 0 {
		t.Fatalf("clean-tree snapshot must be empty, got %q", data)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := runSentinelScript(t, FileSentinelHook, dir, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on first source write from a clean baseline (empty-snapshot misdiagnosis), got PASS:\n%s", out)
	}
	if _, qerr := os.Stat(filepath.Join(qdir, sid, "new.go")); qerr != nil {
		t.Errorf("new.go must be quarantined: %v\noutput:\n%s", qerr, out)
	}
}

// TestSentinelScripts_MarkerlessSnapshotFailsOpen (task 3, other side): an
// empty snapshot WITHOUT the .ok marker means the snapshot failed silently (or
// was planted) — fail-open WARN, never quarantine the user's existing work.
func TestSentinelScripts_MarkerlessSnapshotFailsOpen(t *testing.T) {
	dir := sentinelRepo(t)
	const sid = "sess-markerless"
	tmp := t.TempDir()
	qdir := t.TempDir()

	// Pre-existing uncommitted source that must never be touched.
	if err := os.WriteFile(filepath.Join(dir, "existing.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Plant an empty snapshot with NO .ok marker — the failure signature.
	if err := os.WriteFile(filepath.Join(tmp, "forge-snapshot-"+sid), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	env := sentinelEnv(t, sid, tmp, qdir)
	out, err := runSentinelScript(t, FileSentinelHook, dir, env)
	if err != nil {
		t.Fatalf("file-sentinel must fail-open (PASS) on markerless empty snapshot, got FAIL:\n%s", out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("fail-open path must WARN, got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "existing.go")); statErr != nil {
		t.Errorf("existing.go must remain in place (fail-open), was quarantined: %v", statErr)
	}
}

// TestSentinelScripts_DeletedTrackedFileRestored (task 4): when the Bash
// command deleted a tracked file, quarantine's mv fails (nothing to move) —
// but restore is an independent action: the file must still be recovered from
// HEAD, and the output must report the restore.
func TestSentinelScripts_DeletedTrackedFileRestored(t *testing.T) {
	dir := sentinelRepo(t)
	const sid = "sess-deleted"
	tmp := t.TempDir()
	qdir := t.TempDir()

	original := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "tracked.go"), []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "tracked.go")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	sentinelCommit(t, dir)

	env := sentinelEnv(t, sid, tmp, qdir)
	// "mv" is a detected write command (rm is not — the write flag keys on
	// writers, not deleters); moving the file away produces the same
	// "file missing at quarantine time" mv-failure path.
	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND=mv tracked.go tracked.go.bak")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}
	if err := os.Rename(filepath.Join(dir, "tracked.go"), filepath.Join(dir, "tracked.go.bak")); err != nil {
		t.Fatal(err)
	}

	out, err := runSentinelScript(t, FileSentinelHook, dir, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on tracked source deletion with no task, got PASS:\n%s", out)
	}
	data, statErr := os.ReadFile(filepath.Join(dir, "tracked.go"))
	if statErr != nil {
		t.Fatalf("deleted tracked.go must be restored from HEAD (mv-failure restore fix): %v\noutput:\n%s", statErr, out)
	}
	// git checkout may restore with CRLF on Windows (core.autocrlf) — compare
	// with line endings normalized.
	if got, want := strings.ReplaceAll(string(data), "\r\n", "\n"), original; got != want {
		t.Errorf("restored tracked.go content mismatch: got %q want %q", got, want)
	}
	if !strings.Contains(out, "Restored from HEAD") {
		t.Errorf("output must report the restore, got:\n%s", out)
	}
}

// TestBashGuardHook_WritePatternContract (task 5/6, content pins): the write
// detector matches tokens by basename (absolute paths like /bin/cp), covers
// archive extractors, and disables globbing for the token scan; the pairing
// files are per-invocation ($$-keyed).
func TestBashGuardHook_WritePatternContract(t *testing.T) {
	for _, want := range []string{
		"${tok##*/}", // basename matching: /bin/cp → cp
		"tar|unzip",  // archive extractors write trees without redirects
		"set -f",     // noglob: stray * in the command must not glob-expand
		"-$$",        // per-invocation snapshot/write-flag pairing
		"识别不到 ⇒ 放行",  // documented known-gap contract
	} {
		if !strings.Contains(BashGuardHook, want) {
			t.Errorf("BashGuardHook missing %q (write-pattern / pairing contract)", want)
		}
	}
}

// TestSentinelScripts_SymmetryAndMarkers (tasks 1/2/3, content pins): both
// sides of the pairing must stay symmetric on staged collection, and the
// marker/manifest sidecars must be wired on both ends.
func TestSentinelScripts_SymmetryAndMarkers(t *testing.T) {
	if !strings.Contains(BashGuardHook, "git diff --cached --name-only") {
		t.Error("bash-guard snapshot must include staged files (git diff --cached)")
	}
	if !strings.Contains(FileSentinelHook, "git diff --cached --name-only") {
		t.Error("file-sentinel current-state must include staged files (git diff --cached, symmetric with bash-guard)")
	}
	for _, want := range []string{"forge_cfg_manifest", ".cfg"} {
		if !strings.Contains(BashGuardHook, want) || !strings.Contains(FileSentinelHook, want) {
			t.Errorf("cfg manifest must be wired on BOTH sides, missing %q", want)
		}
	}
	if !strings.Contains(FileSentinelHook, "SNAPSHOT_OK") {
		t.Error("file-sentinel must check the .ok completion marker")
	}
	if !strings.Contains(FileSentinelHook, "QUARANTINE_RESTORED") {
		t.Error("file-sentinel must report restored-from-HEAD files")
	}
}

// TestWorkflowTestGuardHook_ToolFaultSkips (task 7, content pins): the guard
// must skip (PASS), not FAIL-block, on tool faults — go missing or package
// unresolvable. Coverage is any project with internal/ci (opt-in via directory
// presence, per e2e setupGuardProject), NOT restricted to the forge repo.
func TestWorkflowTestGuardHook_ToolFaultSkips(t *testing.T) {
	for _, want := range []string{
		"command -v go",                       // go missing → skip, not exit-127 FAIL
		"no required module provides package", // package-resolution errors skip
	} {
		if !strings.Contains(WorkflowTestGuardHook, want) {
			t.Errorf("WorkflowTestGuardHook missing %q (tool-fault skip contract)", want)
		}
	}
	if strings.Contains(WorkflowTestGuardHook, "module github.com/MjxUpUp/Forge") {
		t.Error("WorkflowTestGuardHook must not restrict to the forge repo — guard is opt-in via internal/ci presence")
	}
}
