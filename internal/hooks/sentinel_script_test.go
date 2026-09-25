package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// hookEnvPath converts a test-generated path into the form the embedded bash
// hook scripts can actually use on Windows. t.TempDir() yields backslash paths
// (C:\Users\...), and backslashes in a bash GLOB pattern are escape characters —
// file-sentinel's snapshot discovery (`for f in "${TMPDIR}/forge-snapshot-..."-*`)
// matched nothing, silently degrading every Windows run to no-baseline (fail-open),
// and the cfg-sidecar race shim never fired (TestSentinelScripts_CfgSidecarRaceSkipsComparison
// "shim 未触发" on windows-latest). Forward-slash C:/... works in cygwin for both
// glob and open/stat, and stays valid for the Go-side os.Stat calls.
//
// hookEnvPath 把测试生成的路径转成内嵌 bash hook 脚本在 Windows 上真正可用的形态。
// t.TempDir() 产出反斜杠路径（C:\Users\...），而反斜杠在 bash GLOB 模式里是转义符——
// file-sentinel 的快照发现（`for f in "${TMPDIR}/forge-snapshot-..."-*`）因此匹配不到
// 任何文件，Windows 上所有 run 静默退化为无基线（fail-open），cfg sidecar 竞态 shim
// 也因此从不触发（windows-latest 上 TestSentinelScripts_CfgSidecarRaceSkipsComparison
// 报「shim 未触发」）。正斜杠 C:/... 在 cygwin 下 glob 与 open/stat 都可用，
// 且对 Go 侧 os.Stat 同样合法。
func hookEnvPath(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(p, `\`, `/`)
	}
	return p
}

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
		"FORGE_SESSION_ID": sid,
		"FORGE_TASK_REF":   "",
		"TMPDIR":           hookEnvPath(tmpdir),
		// 把 DataDir env 钉为关（空串在脚本 marker 根里回落 TMPDIR）——宿主
		// 泄漏的 FORGE_DATA_DIR 会改道 forge-cmd 标记，让「必落 tmpdir」断言
		// 假红。
		"FORGE_DATA_DIR":       "",
		"FORGE_QUARANTINE_DIR": hookEnvPath(qdir),
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
	// The staged entry must NOT survive: staged-new (absent in HEAD) → dropped
	// from the index, and the worktree file stays gone (it is in quarantine).
	// Before the restore fix, "git checkout --" resurrected the staged content
	// into the worktree and kept the index entry — quarantine undone.
	if _, statErr := os.Stat(target); statErr == nil {
		t.Errorf("staged-new staged.go must NOT reappear in the worktree (quarantine would be undone):\n%s", out)
	}
	if staged := gitCachedNames(t, dir); staged != "" {
		t.Errorf("git diff --cached --name-only must be empty after quarantine, got %q", staged)
	}
}

// gitCachedNames returns `git diff --cached --name-only` output in dir.
func gitCachedNames(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git diff --cached: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestSentinelScripts_StagedModificationRestoredToHead (task 2, HEAD-restore
// half): a TRACKED file modified and staged in one command. Quarantine must
// restore the worktree to the HEAD version AND clear the staged entry — plain
// "git checkout --" restored from the INDEX, putting the staged violation right
// back and keeping the index entry (quarantine silently undone).
func TestSentinelScripts_StagedModificationRestoredToHead(t *testing.T) {
	dir := sentinelRepo(t)
	const sid = "sess-staged-mod"
	tmp := t.TempDir()
	qdir := t.TempDir()

	original := "package main\n\nfunc v1() {}\n"
	target := filepath.Join(dir, "tracked.go")
	if err := os.WriteFile(target, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "tracked.go")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	sentinelCommit(t, dir)

	env := sentinelEnv(t, sid, tmp, qdir)
	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND=cat > tracked.go && git add tracked.go")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}

	// Modify + stage in one go — the write-then-git-add escape form.
	if err := os.WriteFile(target, []byte("package main\n\nfunc v2() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	add2 := exec.Command("git", "add", "tracked.go")
	add2.Dir = dir
	if out, err := add2.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	out, err := runSentinelScript(t, FileSentinelHook, dir, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on staged modification with no task, got PASS:\n%s", out)
	}
	// Quarantine preserves the staged (v2) content for recovery.
	qdata, qerr := os.ReadFile(filepath.Join(qdir, sid, "tracked.go"))
	if qerr != nil {
		t.Fatalf("staged tracked.go must be quarantined: %v\noutput:\n%s", qerr, out)
	}
	if !strings.Contains(string(qdata), "v2") {
		t.Errorf("quarantined content must be the staged v2, got %q", string(qdata))
	}
	// Worktree restored to the HEAD version (v1) — not to the staged v2.
	data, statErr := os.ReadFile(target)
	if statErr != nil {
		t.Fatalf("tracked.go must be restored from HEAD: %v\noutput:\n%s", statErr, out)
	}
	if got, want := strings.ReplaceAll(string(data), "\r\n", "\n"), original; got != want {
		t.Errorf("worktree must be the HEAD version: got %q want %q (restore-from-index would resurrect the staged v2)", got, want)
	}
	// Staged entry cleared.
	if staged := gitCachedNames(t, dir); staged != "" {
		t.Errorf("git diff --cached --name-only must be empty after quarantine, got %q", staged)
	}
}

// TestSentinelScripts_SnapshotGitFailureFailsOpen (task 3 hardened): the .ok
// completion marker must mean "git enumeration SUCCEEDED", not "bash-guard
// reached the marker line". With git failing at snapshot time (invalid GIT_DIR)
// the marker must stay absent — and file-sentinel (git healthy again) must take
// its snapshot-failed fail-open WARN branch instead of reading the entire
// working tree as new violations (the mass-quarantine P0 scenario).
func TestSentinelScripts_SnapshotGitFailureFailsOpen(t *testing.T) {
	dir := sentinelRepo(t)
	const sid = "sess-gitfail"
	tmp := t.TempDir()
	qdir := t.TempDir()

	// Pre-existing uncommitted source that must never be touched.
	victim := filepath.Join(dir, "existing.go")
	if err := os.WriteFile(victim, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Snapshot with git broken (invalid GIT_DIR): every enumeration fails →
	// snapshot file exists but NO .ok marker may be written.
	badEnv := append(sentinelEnv(t, sid, tmp, qdir),
		"GIT_DIR="+filepath.ToSlash(filepath.Join(dir, "no-such-git-dir")),
		"FORGE_COMMAND=cat > existing.go")
	if out, err := runSentinelScript(t, BashGuardHook, dir, badEnv); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}
	okMarkers, err := filepath.Glob(filepath.Join(tmp, "forge-snapshot-"+sid+"-*.ok"))
	if err != nil {
		t.Fatal(err)
	}
	if len(okMarkers) != 0 {
		t.Fatalf(".ok marker must NOT be written when git enumeration failed, found %v", okMarkers)
	}
	snaps, err := filepath.Glob(filepath.Join(tmp, "forge-snapshot-"+sid+"-*"))
	if err != nil {
		t.Fatal(err)
	}
	hasSnap := false
	for _, s := range snaps {
		if !strings.HasSuffix(s, ".ok") && !strings.HasSuffix(s, ".cfg") {
			hasSnap = true
		}
	}
	if !hasSnap {
		t.Fatalf("per-invocation snapshot file must exist (empty) even when git failed, found %v", snaps)
	}

	// Sentinel with git healthy again: empty baseline + non-empty tree + NO
	// marker → fail-open WARN, never mass quarantine.
	out, err := runSentinelScript(t, FileSentinelHook, dir, sentinelEnv(t, sid, tmp, qdir))
	if err != nil {
		t.Fatalf("file-sentinel must fail-open (PASS) on failed snapshot, got FAIL:\n%s", out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("fail-open path must WARN, got:\n%s", out)
	}
	if _, statErr := os.Stat(victim); statErr != nil {
		t.Errorf("existing.go must remain in place (no mass quarantine), was quarantined: %v", statErr)
	}
}

// TestSentinelScripts_ConfigForgeSubstringBypassBlocked (exemption tightening):
// the CONFIG-quarantine exemption must require the WHOLE command to be a forge
// invocation. `echo evil > .claude/settings.local.json && forge --version`
// contains the substring " forge " but is a config rewrite — it must NOT be
// exempted. This bites now that the .cfg manifest covers .claude/settings*.
func TestSentinelScripts_ConfigForgeSubstringBypassBlocked(t *testing.T) {
	dir := sentinelRepo(t)
	const sid = "sess-forge-bypass"
	tmp := t.TempDir()
	qdir := t.TempDir()

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
	bypass := "echo evil > .claude/settings.local.json && forge --version"
	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND="+bypass)); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}
	// The old substring match would have planted the forge-cmd marker here.
	if _, err := os.Stat(filepath.Join(tmp, "markers", "forge-cmd-"+sid)); err == nil {
		t.Fatal("forge-cmd marker must NOT be planted for a compound command merely containing ' forge '")
	}

	if err := os.WriteFile(settings, []byte(`{"hooks":{"PreToolUse":[]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := runSentinelScript(t, FileSentinelHook, dir, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on config rewrite behind a ' forge ' substring, got PASS:\n%s", out)
	}
	if _, qerr := os.Stat(filepath.Join(qdir, sid, ".claude", "settings.local.json")); qerr != nil {
		t.Errorf("settings.local.json must be quarantined (substring exemption bypass): %v\noutput:\n%s", qerr, out)
	}

	// Other side: a genuine whole-command forge invocation IS still exempted.
	const sid2 = "sess-forge-legit"
	tmp2 := t.TempDir()
	env2 := sentinelEnv(t, sid2, tmp2, qdir)
	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env2, "FORGE_COMMAND=forge task status")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(tmp2, "markers", "forge-cmd-"+sid2)); err != nil {
		t.Errorf("whole-command 'forge task status' must plant the forge-cmd marker: %v", err)
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
	// Plant an empty per-invocation snapshot with NO .ok marker — the failure
	// signature (bash-guard writes neither content nor marker when git fails).
	if err := os.WriteFile(filepath.Join(tmp, "forge-snapshot-"+sid+"-4242"), []byte(""), 0644); err != nil {
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

// --- forge self-deploy exemption (weekly-hardening 改动 2) ---
// The 2026-08-02 self-injury incident: a monitored non-forge Bash command fired
// the hook chain while a forge subprocess's autoSync rewrote project-level
// .forge/hooks/*.sh; the .cfg manifest saw the drift with IS_FORGE_CMD=0 and the
// whole directory was quarantined. The Go side now writes a
// <DataDir>/stamps/hook-deploy marker ("<epoch> <tag>") before rewriting hooks;
// the CONFIG branch exempts hooks-only drift inside the 120s grace window.

// setupHooksDeployRepo builds a repo with a gitignored .forge/hooks/task-guard.sh
// — the layout of team-mode / legacy projects whose project-level hook copies the
// sentinel manifest watches.
func setupHooksDeployRepo(t *testing.T) (dir, hooksFile string) {
	t.Helper()
	dir = sentinelRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".forge/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hooksDir := filepath.Join(dir, ".forge", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hooksFile = filepath.Join(hooksDir, "task-guard.sh")
	if err := os.WriteFile(hooksFile, []byte("#!/bin/bash\necho v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sentinelCommit(t, dir)
	return dir, hooksFile
}

// writeDeployStamp writes a <dataDir>/stamps/hook-deploy marker with the given
// epoch and tag (mirrors hooks.WriteHookDeployStamp's format).
func writeDeployStamp(t *testing.T, dataDir string, epoch int64, tag string) {
	t.Helper()
	stampsDir := filepath.Join(dataDir, "stamps")
	if err := os.MkdirAll(stampsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("%d %s\n", epoch, tag)
	if err := os.WriteFile(filepath.Join(stampsDir, "hook-deploy"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// runHooksDrift drives one bash-guard snapshot + a .forge/hooks rewrite + one
// file-sentinel run, returning the sentinel output and error.
func runHooksDrift(t *testing.T, dir, hooksFile, sid string, env []string) (string, error) {
	t.Helper()
	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND=cat > .forge/hooks/task-guard.sh")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}
	// Forge's own deploy write (autoSync / init team-mode rewriting the copies).
	if err := os.WriteFile(hooksFile, []byte("#!/bin/bash\necho v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return runSentinelScript(t, FileSentinelHook, dir, env)
}

// TestSentinelScripts_HooksDeployMarkerExempts: a FRESH deploy marker +
// hooks-only drift → PASS without quarantine (forge self-deploy).
func TestSentinelScripts_HooksDeployMarkerExempts(t *testing.T) {
	dir, hooksFile := setupHooksDeployRepo(t)
	const sid = "sess-deploy-fresh"
	tmp := t.TempDir()
	qdir := t.TempDir()
	dataDir := t.TempDir()
	writeDeployStamp(t, dataDir, time.Now().Unix(), "tag-abc")

	env := append(sentinelEnv(t, sid, tmp, qdir), "FORGE_DATA_DIR="+dataDir)
	out, err := runHooksDrift(t, dir, hooksFile, sid, env)
	if err != nil {
		t.Fatalf("file-sentinel must PASS within the deploy grace window, got FAIL:\n%s", out)
	}
	if !strings.Contains(out, "deploy window") {
		t.Errorf("output must identify the deploy-window exemption, got:\n%s", out)
	}
	if _, statErr := os.Stat(hooksFile); statErr != nil {
		t.Errorf("deployed hook file must stay in place (no quarantine): %v", statErr)
	}
	if entries, _ := filepath.Glob(filepath.Join(qdir, sid, ".forge", "hooks", "*")); len(entries) != 0 {
		t.Errorf("nothing may be quarantined inside the deploy window, found %v", entries)
	}
}

// TestSentinelScripts_HooksDeployMarkerStaleQuarantines: an EXPIRED marker
// (older than the 120s window) must NOT exempt — the drift is quarantined as
// before. Also covers the no-marker case (second half).
func TestSentinelScripts_HooksDeployMarkerStaleQuarantines(t *testing.T) {
	dir, hooksFile := setupHooksDeployRepo(t)
	const sid = "sess-deploy-stale"
	tmp := t.TempDir()
	qdir := t.TempDir()
	dataDir := t.TempDir()
	writeDeployStamp(t, dataDir, time.Now().Unix()-600, "tag-abc")

	env := append(sentinelEnv(t, sid, tmp, qdir), "FORGE_DATA_DIR="+dataDir)
	out, err := runHooksDrift(t, dir, hooksFile, sid, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on hooks drift with an expired deploy marker, got PASS:\n%s", out)
	}
	if _, qerr := os.Stat(filepath.Join(qdir, sid, ".forge", "hooks", "task-guard.sh")); qerr != nil {
		t.Errorf("task-guard.sh must be quarantined when the marker is stale: %v\noutput:\n%s", qerr, out)
	}

	// No marker at all → same strict behavior.
	const sid2 = "sess-deploy-none"
	tmp2 := t.TempDir()
	dataDir2 := t.TempDir()
	env2 := append(sentinelEnv(t, sid2, tmp2, qdir), "FORGE_DATA_DIR="+dataDir2)
	out, err = runHooksDrift(t, dir, hooksFile, sid2, env2)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on hooks drift without a deploy marker, got PASS:\n%s", out)
	}
	if _, qerr := os.Stat(filepath.Join(qdir, sid2, ".forge", "hooks", "task-guard.sh")); qerr != nil {
		t.Errorf("task-guard.sh must be quarantined without a marker: %v\noutput:\n%s", qerr, out)
	}
}

// TestSentinelScripts_HooksDeployTagMismatchQuarantines: a fresh marker whose
// project tag does not match FORGE_PROJECT_TAG must NOT exempt (cross-project
// anti-forgery check). A matching tag exempts.
func TestSentinelScripts_HooksDeployTagMismatchQuarantines(t *testing.T) {
	dir, hooksFile := setupHooksDeployRepo(t)
	qdir := t.TempDir()

	const sid = "sess-deploy-tagbad"
	tmp := t.TempDir()
	dataDir := t.TempDir()
	writeDeployStamp(t, dataDir, time.Now().Unix(), "tag-abc")
	env := append(sentinelEnv(t, sid, tmp, qdir),
		"FORGE_DATA_DIR="+dataDir, "FORGE_PROJECT_TAG=tag-other")
	out, err := runHooksDrift(t, dir, hooksFile, sid, env)
	if err == nil {
		t.Fatalf("file-sentinel must FAIL on tag-mismatched deploy marker, got PASS:\n%s", out)
	}

	const sid2 = "sess-deploy-tagok"
	tmp2 := t.TempDir()
	env2 := append(sentinelEnv(t, sid2, tmp2, qdir),
		"FORGE_DATA_DIR="+dataDir, "FORGE_PROJECT_TAG=tag-abc")
	out, err = runHooksDrift(t, dir, hooksFile, sid2, env2)
	if err != nil {
		t.Fatalf("file-sentinel must PASS on tag-matched fresh deploy marker, got FAIL:\n%s", out)
	}
}

// TestSentinelScripts_CfgSidecarRaceSkipsComparison pins the .cfg sidecar race
// fix: a concurrent file-sentinel (parallel Bash tool calls in one session both
// glob the newest snapshot) can run its cleanup rm between file-sentinel's -f
// test and the manifest read.
//
// TestSentinelScripts_CfgSidecarRaceSkipsComparison 钉住 .cfg sidecar 竞态修复：
// 并发的 file-sentinel（同会话并行 Bash 调用同时 glob 最新快照）可能在
// file-sentinel 的 -f 测试与 manifest 读取之间执行 cleanup rm。2026-08-17 实证
// （checklog 11:59:32）：cat 向 stderr 打出 "No such file or directory"，且喂给
// diff 的 manifest 为空——其对称差会把全部当前 config 文件判为 drift（假隔离
// 方向）。用 `cat` shim 在读取的精确时刻删掉 sidecar，确定性复现该交错；修复后
// 的脚本必须跳过比较（PASS、无 stderr 噪音、零隔离）。
func TestSentinelScripts_CfgSidecarRaceSkipsComparison(t *testing.T) {
	dir, hooksFile := setupHooksDeployRepo(t)
	const sid = "sess-cfg-race"
	tmp := t.TempDir()
	qdir := t.TempDir()
	env := sentinelEnv(t, sid, tmp, qdir)

	// 只读命令：bash-guard 写下快照三件套。仓库带一个 gitignored 的
	// .forge/hooks 文件，manifest 才有真实内容——否则半读的 manifest 是
	// 空 vs 空，测不出 drift 与 no-drift 的区别。
	if out, err := runSentinelScript(t, BashGuardHook, dir, append(env, "FORGE_COMMAND=ls")); err != nil {
		t.Fatalf("bash-guard failed: %v\n%s", err, out)
	}
	cfgs, err := filepath.Glob(filepath.Join(tmp, "forge-snapshot-"+sid+"-*.cfg"))
	if err != nil || len(cfgs) == 0 {
		t.Fatalf("bash-guard must write the .cfg sidecar (glob: %v, err: %v)", cfgs, err)
	}

	// cat shim：在 file-sentinel 正要读 .cfg 的精确时刻删掉它（并发 cleanup
	// 交错），留 marker 让 shim 未触发时测试响亮失败，所有调用透传真 cat
	// （BEFORE_ALL 读快照本尊不受影响）。
	// BASH_ENV 函数覆盖，不是 PATH shim：MSYS2 runtime 在进程启动时会重排
	// Windows PATH（自己的 /usr/bin 排最前），prepend 的 shim 目录会输——
	// 2026-08-17 Windows CI 探针实证 command -v cat = /usr/bin/cat。BASH_ENV
	// 被每个非交互 bash（hook 本体及其命令替换子 shell）source，shell 函数
	// 无条件优先于 PATH 查找。函数体内 `command cat` 绕过覆盖读真 cat。
	fired := filepath.Join(t.TempDir(), "shim-fired")
	shimLog := filepath.Join(t.TempDir(), "shim.log")
	bashEnv := filepath.Join(t.TempDir(), "bashenv.sh")
	shim := "# shim: intercept cat to delete the .cfg sidecar at the exact read moment\n" +
		"cat() {\n" +
		"  echo \"SHIM CALLED: $*\" >> \"$FORGE_SHIM_LOG\" 2>&1 || true\n" +
		"  local a\n" +
		"  for a in \"$@\"; do\n" +
		"    case \"$a\" in\n" +
		"      *forge-snapshot-*.cfg) rm -f \"$a\"; : > \"$FORGE_SHIM_FIRED\" ;;\n" +
		"    esac\n" +
		"  done\n" +
		"  command cat \"$@\"\n" +
		"}\n"
	if err := os.WriteFile(bashEnv, []byte(shim), 0644); err != nil {
		t.Fatal(err)
	}
	raceEnv := append(append([]string{}, env...),
		"BASH_ENV="+hookEnvPath(bashEnv),
		"FORGE_SHIM_FIRED="+hookEnvPath(fired),
		"FORGE_SHIM_LOG="+hookEnvPath(shimLog))

	// 无真实 drift：hooksFile 未动。只有竞态注入的 sidecar 消失，不得伪造出
	// config drift。
	out, err := runSentinelScript(t, FileSentinelHook, dir, raceEnv)
	if err != nil {
		t.Fatalf("sidecar 竞态下 file-sentinel 应 PASS（跳过比较），got FAIL:\n%s", out)
	}
	if strings.Contains(out, "No such file or directory") {
		t.Errorf("sidecar 消失不应产生 cat 报错噪音:\n%s", out)
	}
	if _, statErr := os.Stat(fired); statErr != nil {
		// 失败时诊断（Windows CI 是唯一复现环境）：cat 函数覆盖是否生效、
		// 快照 glob 在该 TMPDIR 下是否匹配、shim 看到了什么。探针另写一个
		// 匹配模式的新文件——真快照已被 file-sentinel cleanup 消费。
		probe := filepath.Join(tmp, "forge-snapshot-"+sid+"-probe")
		if werr := os.WriteFile(probe, []byte("x"), 0644); werr != nil {
			t.Fatal(werr)
		}
		probeScript := `echo "WHICH_CAT=$(command -v cat)"
echo "TMPDIR=$TMPDIR"
for f in "${TMPDIR}/forge-snapshot-${FORGE_SESSION_ID}"-*; do echo "GLOB_MATCH=$f"; done
cat "${TMPDIR}/forge-snapshot-${FORGE_SESSION_ID}-probe" >/dev/null 2>&1
echo "PROBE_CAT_RC=$?"
echo "FIRED_PATH=$FORGE_SHIM_FIRED"`
		pcmd := exec.Command("bash", "-c", probeScript)
		pcmd.Dir = dir
		pcmd.Env = raceEnv
		probeOut, _ := pcmd.CombinedOutput()
		logData, _ := os.ReadFile(shimLog)
		t.Fatalf("shim 未触发——竞态路径未被覆盖（vacuous pass）: %v\n--- probe ---\n%s--- shim log ---\n%s--- hook output ---\n%s", statErr, probeOut, logData, out)
	}
	if _, statErr := os.Stat(hooksFile); statErr != nil {
		t.Errorf("无真实 drift 时 hooksFile 不应被隔离: %v\noutput:\n%s", statErr, out)
	}
	// quarantine_files 保留相对路径（mv "$f" "${qdir}/${f}"）——断言嵌套位置
	// 才有检出力：drift 检测会落在 qdir/<sid>/.forge/hooks/task-guard.sh，
	// 而非 qdir/<sid>/。
	if _, statErr := os.Stat(filepath.Join(qdir, sid, ".forge", "hooks", "task-guard.sh")); statErr == nil {
		t.Errorf("quarantine 不应出现 .forge/hooks/task-guard.sh（假隔离方向）:\n%s", out)
	}
}
