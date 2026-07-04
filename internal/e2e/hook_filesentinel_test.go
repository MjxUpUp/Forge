package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestHook_FileSentinel_QuarantinesBashWrittenSource guards the PostToolUse
// file-sentinel quarantine loop: when source code appears on disk with no
// active task — the pattern left by an agent writing files via Bash (bypassing
// the Write/Edit tools and their task-guard / assertion-check gates) —
// file-sentinel must detect it against the PreToolUse bash-guard snapshot and
// move it to DataDir/quarantine/<session>/, never deleting it.
//
// The recovery ring under test: bash-guard (PreToolUse) only WARNs on the write
// but always records the snapshot; file-sentinel (PostToolUse) is what actually
// quarantines the unauthorized source afterward.
//
// Platform risk pinned here: NEW-changes is computed with bash process
// substitution `grep -Fxvf <(...)` (embed.go ~line 884). Should that fail on
// Windows Git Bash, NEW_CHANGES comes back empty and the quarantine silently
// does not fire — this test would then FAIL, surfacing the bug rather than
// masking it.
func TestHook_FileSentinel_QuarantinesBashWrittenSource(t *testing.T) {
	// No forge task start → FORGE_TASK_REF empty → source changes get quarantined
	// (file-sentinel line ~950: no task + source change => quarantine).
	dir := freshProject(t)
	const sid = "sess-filesentinel"
	// SHARED tmpdir: bash-guard's snapshot file must survive on disk for the
	// later file-sentinel invocation to read it. The shared forgeHook helper
	// allocates a fresh t.TempDir() per call, which would orphan the snapshot —
	// so this test runs the hook subprocess inline with one fixed TMPDIR.
	tmp := t.TempDir()
	binDir := filepath.Dir(forgeBin)

	runHook := func(hookName, stdinJSON string) (string, string, error) {
		cmd := exec.Command(forgeBin, "hook", hookName)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdinJSON)
		cmd.Env = append(os.Environ(),
			"TMPDIR="+tmp,
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		return out.String(), errBuf.String(), err
	}

	// 1. PreToolUse bash-guard: records the pre-write git snapshot. The command
	//    CARRIES a write pattern ("cat > src.go") so bash-guard sets the
	//    write-flag — this is what a real agent writing source via Bash looks
	//    like, and it is what lets file-sentinel's secondary gate tell this apart
	//    from a read-only command (which must fail-open, never quarantine).
	bashIn := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "cat > src.go",
	})
	if _, _, err := runHook("bash-guard", bashIn); err != nil {
		t.Fatalf("bash-guard snapshot step failed unexpectedly: %v", err)
	}

	// 2. Simulate the agent's write landing on disk (git sees an untracked .go).
	writeFile(t, dir, "src.go", "package main\n\nfunc main() {}\n")

	// 3. PostToolUse file-sentinel: src.go is a NEW source file vs the snapshot,
	//    the command was a write, and there is no active task → quarantined.
	sentIn := hookStdin(t, sid, "PostToolUse", "Bash", map[string]any{
		"command": "cat > src.go",
	})
	stdout, _, err := runHook("file-sentinel", sentIn)
	if err == nil {
		t.Fatal("file-sentinel must block (quarantine) unauthorized source write with no active task, got PASS")
	}
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("file-sentinel stdout missing decision=block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "file-sentinel") {
		t.Errorf("file-sentinel stdout missing guard identifier:\n%s", stdout)
	}

	// The written source must have been MOVED into DataDir/quarantine/<sid>/.
	quarantined := filepath.Join(forgedata.DataDirFor(dir), "quarantine", sid, "src.go")
	if _, qerr := os.Stat(quarantined); qerr != nil {
		t.Fatalf("expected quarantined file at %s, got %v\nstdout:\n%s", quarantined, qerr, stdout)
	}
	// And removed from the working tree (mv, not copy) — recovery is via the
	// quarantine dir, not the original location.
	if fileExists(t, dir, "src.go") {
		t.Errorf("src.go should have been moved out of the working tree, still present\nstdout:\n%s", stdout)
	}
}

// TestHook_FileSentinel_PassesWithActiveTask guards the other side of the
// line-950 branch: when an active task IS present, source changes written via
// Bash are tracked (allowed) — file-sentinel must NOT quarantine them. This
// prevents the sentinel from fighting legitimate in-task Bash edits.
func TestHook_FileSentinel_PassesWithActiveTask(t *testing.T) {
	dir := freshProject(t)
	const sid = "sess-filesentinel-tasked"
	tmp := t.TempDir()
	binDir := filepath.Dir(forgeBin)

	// Start an active task so FORGE_TASK_REF resolves non-empty for this session.
	forge(t, dir, "task", "start", "--ref", "feat/sentinel-guard", "--title", "guard test")

	runHook := func(hookName, stdinJSON string) (string, string, error) {
		cmd := exec.Command(forgeBin, "hook", hookName)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdinJSON)
		cmd.Env = append(os.Environ(),
			"TMPDIR="+tmp,
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		return out.String(), errBuf.String(), err
	}

	bashIn := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "echo editing in task",
	})
	if _, _, err := runHook("bash-guard", bashIn); err != nil {
		t.Fatalf("bash-guard snapshot step failed unexpectedly: %v", err)
	}

	writeFile(t, dir, "src.go", "package main\n\nfunc main() {}\n")

	sentIn := hookStdin(t, sid, "PostToolUse", "Bash", map[string]any{
		"command": "echo editing in task",
	})
	stdout, _, err := runHook("file-sentinel", sentIn)
	if err != nil {
		t.Fatalf("file-sentinel must allow source changes under an active task, got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("file-sentinel stdout missing decision=approve:\n%s", stdout)
	}
	// Source stays in the working tree, nothing quarantined.
	if !fileExists(t, dir, "src.go") {
		t.Errorf("src.go must remain in the working tree under an active task\nstdout:\n%s", stdout)
	}
	qdir := filepath.Join(forgedata.DataDirFor(dir), "quarantine", sid)
	if _, qerr := os.Stat(qdir); qerr == nil {
		t.Errorf("no quarantine dir should be created under an active task, found %s", qdir)
	}
}

// TestHook_FileSentinel_GateStatusBeyondGitDiff 钉死 refactor-data-home commit D
// 的已知防护缺口：gate verdict（all_gates_passed）迁到用户级 DataDir/gates/<id>/
// status.json 后，file-sentinel 基于 git diff 检测——DataDir 在 ~/.forge 不在 git
// 仓库，git diff 永远不返 DataDir 路径，file-sentinel 管不到。
//
// 迁移前 A6（守 .forge/gates/status.json 不被 Bash 篡改 flip verdict）随 gates 进
// DataDir 失效——这是 git-diff 维度拦截器对 git 仓库外路径的固有限制。本负向测试
// 固化该契约：构造 DataDir/gates/status.json 篡改，断言 file-sentinel 不 quarantine
// （PASS，文件原位），防止未来误以为守住了。缺口由 forge 自身完整性校验补（commit E
// 或后续），不靠 file-sentinel。
func TestHook_FileSentinel_GateStatusBeyondGitDiff(t *testing.T) {
	dir := freshProject(t)
	const sid = "sess-gates-datadir"
	tmp := t.TempDir()
	binDir := filepath.Dir(forgeBin)
	forge(t, dir, "task", "start", "--ref", "feat/gate-guard", "--title", "gate guard")

	runHook := func(hookName, stdinJSON string) (string, string, error) {
		cmd := exec.Command(forgeBin, "hook", hookName)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdinJSON)
		cmd.Env = append(os.Environ(),
			"TMPDIR="+tmp,
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		return out.String(), errBuf.String(), err
	}

	// PreToolUse bash-guard: 建 snapshot（git diff 基线）。
	bashIn := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "echo flipping gate verdict externally",
	})
	if _, _, err := runHook("bash-guard", bashIn); err != nil {
		t.Fatalf("bash-guard snapshot step failed unexpectedly: %v", err)
	}

	// 篡改 gate verdict 的实际位置：DataDir/gates/<id>/status.json（git 不可见）。
	dataDir := forgedata.DataDirFor(dir)
	gateDir := filepath.Join(dataDir, "gates", "task-verify")
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		t.Fatal(err)
	}
	tampered := `{"gate":"task-verify","all_gates_passed":true,"forged":true}` + "\n"
	tamperedPath := filepath.Join(gateDir, "status.json")
	if err := os.WriteFile(tamperedPath, []byte(tampered), 0644); err != nil {
		t.Fatal(err)
	}

	// PostToolUse file-sentinel: git diff 不含 DataDir 路径 → NEW_CHANGES 空 → PASS。
	// 缺口：file-sentinel 无法检测 DataDir/gates 篡改（git-diff 维度的固有限制）。
	sentIn := hookStdin(t, sid, "PostToolUse", "Bash", map[string]any{
		"command": "echo flipping gate verdict externally",
	})
	stdout, _, err := runHook("file-sentinel", sentIn)
	if err != nil {
		t.Fatalf("file-sentinel must PASS (DataDir/gates beyond git-diff reach — known gap), got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("file-sentinel stdout missing decision=approve:\n%s", stdout)
	}
	// 篡改文件仍在原位（file-sentinel 管不到 DataDir）。
	if _, qerr := os.Stat(tamperedPath); qerr != nil {
		t.Errorf("DataDir/gates/status.json should remain untouched (file-sentinel cannot reach DataDir — known gap), got: %v\nstdout:\n%s", qerr, stdout)
	}
}

// TestHook_FileSentinel_FailOpenOnEmptySnapshot reproduces the P0 DevWorkbench
// incident: bash-guard's PreToolUse snapshot came back EMPTY (git failed
// silently under 2>/dev/null — cwd drift, index.lock, Windows newline) while the
// working tree already held the user's uncommitted source. The OLD file-sentinel
// treated the entire working tree as NEW_CHANGES (the else-branch when
// BEFORE_ALL is empty) and quarantined + git-checkout-restored every source
// file, destroying the user's work. The fix must FAIL-OPEN: PASS, no quarantine.
func TestHook_FileSentinel_FailOpenOnEmptySnapshot(t *testing.T) {
	dir := freshProject(t)
	const sid = "sess-empty-snap"
	tmp := t.TempDir()
	binDir := filepath.Dir(forgeBin)

	// The user's pre-existing uncommitted source — must never be touched.
	writeFile(t, dir, "existing.go", "package main\n\nfunc main() {}\n")

	runHook := func(hookName, stdinJSON string) (string, string, error) {
		cmd := exec.Command(forgeBin, "hook", hookName)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdinJSON)
		cmd.Env = append(os.Environ(),
			"TMPDIR="+tmp,
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		return out.String(), errBuf.String(), err
	}

	// bash-guard writes a snapshot; we then simulate the failure mode by
	// truncating it to empty — exactly what happens when its git commands fail
	// under 2>/dev/null.
	bashIn := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "git diff --stat",
	})
	if _, _, err := runHook("bash-guard", bashIn); err != nil {
		t.Fatalf("bash-guard failed unexpectedly: %v", err)
	}
	snap := filepath.Join(tmp, "forge-snapshot-"+sid)
	if err := os.WriteFile(snap, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// file-sentinel: empty snapshot + working-tree source + read-only command.
	// MUST fail-open (PASS) and leave the user's source in place.
	sentIn := hookStdin(t, sid, "PostToolUse", "Bash", map[string]any{
		"command": "git diff --stat",
	})
	stdout, _, err := runHook("file-sentinel", sentIn)
	if err != nil {
		t.Fatalf("file-sentinel must fail-open (PASS) on empty snapshot with existing work, got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("file-sentinel stdout missing decision=approve:\n%s", stdout)
	}
	if !fileExists(t, dir, "existing.go") {
		t.Errorf("existing.go must remain in working tree (fail-open), was quarantined\nstdout:\n%s", stdout)
	}
	qdir := filepath.Join(forgedata.DataDirFor(dir), "quarantine", sid)
	if _, qerr := os.Stat(qdir); qerr == nil {
		t.Errorf("no quarantine dir should be created on fail-open, found %s", qdir)
	}
}

// TestHook_FileSentinel_FailOpenOnReadOnlyCommand guards the secondary gate:
// even with a non-empty (but unreliable) snapshot, a READ-ONLY Bash command
// cannot produce source changes. Source changes seen under a read-only command
// mean external interference (another editor, partial snapshot), not a
// Bash-written violation — file-sentinel must fail-open, not quarantine.
func TestHook_FileSentinel_FailOpenOnReadOnlyCommand(t *testing.T) {
	dir := freshProject(t)
	const sid = "sess-readonly-gate"
	tmp := t.TempDir()
	binDir := filepath.Dir(forgeBin)

	runHook := func(hookName, stdinJSON string) (string, string, error) {
		cmd := exec.Command(forgeBin, "hook", hookName)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdinJSON)
		cmd.Env = append(os.Environ(),
			"TMPDIR="+tmp,
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		return out.String(), errBuf.String(), err
	}

	// bash-guard with a READ-ONLY command: writes snapshot + empty write-flag.
	bashIn := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "ls -la",
	})
	if _, _, err := runHook("bash-guard", bashIn); err != nil {
		t.Fatalf("bash-guard failed unexpectedly: %v", err)
	}
	// Verify bash-guard recorded an EMPTY write-flag (read-only command).
	wflag := filepath.Join(tmp, "forge-write-"+sid)
	if info, err := os.Stat(wflag); err != nil {
		t.Fatalf("bash-guard must create write-flag file for secondary gate, missing: %v", err)
	} else if info.Size() != 0 {
		t.Fatalf("read-only command must produce EMPTY write-flag, got size %d", info.Size())
	}

	// Source appears AFTER the snapshot (external editor / another process) —
	// makes NEW_CHANGES non-empty, exercising the source-quarantine branch.
	writeFile(t, dir, "external.go", "package main\n")

	// file-sentinel with the SAME read-only command → IS_WRITE_CMD=0 → fail-open.
	sentIn := hookStdin(t, sid, "PostToolUse", "Bash", map[string]any{
		"command": "ls -la",
	})
	stdout, _, err := runHook("file-sentinel", sentIn)
	if err != nil {
		t.Fatalf("file-sentinel must fail-open (PASS) for read-only command, got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("file-sentinel stdout missing decision=approve:\n%s", stdout)
	}
	if !fileExists(t, dir, "external.go") {
		t.Errorf("external.go must remain in working tree (read-only fail-open), was quarantined\nstdout:\n%s", stdout)
	}
	qdir := filepath.Join(forgedata.DataDirFor(dir), "quarantine", sid)
	if _, qerr := os.Stat(qdir); qerr == nil {
		t.Errorf("no quarantine dir should be created on read-only fail-open, found %s", qdir)
	}
}
