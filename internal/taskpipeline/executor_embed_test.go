package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// TestRunEmbeddedHook_AutoCompileAdvisory_IgnoresTamper verifies the v0.25
// advisory auto-compile: (1) it NEVER runs the compiler, so a broken module
// still passes (compile self-check is delegated to the agent, tech-stack
// agnostic); (2) the A3 invariant holds — the gate runs the EMBEDDED hook, not
// .forge/hooks/*.sh on disk, so a tampered disk hook cannot change the verdict
// or leak its marker.
//
// Before v0.25 this planted a broken module + an always-passing disk hook and
// asserted the embed ran go build → FAIL. Advisory mode drops the build step,
// so the assertion flips to "passes without compiling".
func TestRunEmbeddedHook_AutoCompileAdvisory_IgnoresTamper(t *testing.T) {
	dir := t.TempDir()

	// A Go module that does NOT compile — advisory mode must not care.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module breaktest\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Deliberate syntax error — would fail go build, but advisory mode no longer runs it.
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package breaktest\n\nfunc Broken() { syntax error here }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Tampered disk hook: if the gate read this instead of the embed, the marker
	// would appear in output. The embed is canonical regardless of advisory mode.
	tampered := "#!/bin/bash\necho TAMPERED_DISK_HOOK_PASSES\nexit 0\n"
	hookDir := filepath.Join(dir, ".forge", "hooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "auto-compile.sh"), []byte(tampered), 0644); err != nil {
		t.Fatal(err)
	}

	passed, infra, output := runEmbeddedHook(dir, "auto-compile")

	// Advisory: passes without compiling — broken.go no longer fails the gate.
	if !passed {
		t.Fatalf("advisory auto-compile must PASS on a broken module (no compile enforced), got fail:\n%q", output)
	}
	if infra {
		t.Fatalf("a normally-executed hook is not an infrastructure failure, got infra=true:\n%q", output)
	}
	// A3 invariant: the embed ran, not the disk hook.
	if strings.Contains(output, "TAMPERED_DISK_HOOK_PASSES") {
		t.Fatalf("disk hook marker reached output — gate is executing .forge/hooks/*.sh instead of the embed:\n%q", output)
	}
	// Advisory: the real compiler must NOT have run, so no compiler FAIL in output.
	if strings.Contains(output, "FAIL") {
		t.Fatalf("advisory auto-compile must NOT run the compiler (unexpected FAIL in output):\n%q", output)
	}
}

// TestRunEmbeddedHook_AutoCompileAdvisory_PassesOnCleanModule confirms the
// advisory hook still reports PASS on a normal module. v0.25: PASS no longer
// means "build succeeded" — it means "advisory reminder / no-op, compile
// delegated to agent".
func TestRunEmbeddedHook_AutoCompileAdvisory_PassesOnCleanModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module cleantest\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	passed, _, output := runEmbeddedHook(dir, "auto-compile")
	if !passed {
		t.Fatalf("advisory auto-compile should PASS, got fail:\n%q", output)
	}
	if !strings.Contains(output, "PASS") {
		t.Fatalf("expected PASS in output, got:\n%q", output)
	}
}

// TestRunEmbeddedHook_UnknownHookFailsClosed verifies a typo'd hook name cannot
// silently pass: EmbeddedContent returns ok=false and runEmbeddedHook reports
// not-found with passed=false rather than skipping the check.
func TestRunEmbeddedHook_UnknownHookFailsClosed(t *testing.T) {
	passed, infra, output := runEmbeddedHook(t.TempDir(), "no-such-hook")
	if passed {
		t.Fatalf("unknown hook must fail closed (passed=false), got passed=true: %q", output)
	}
	if infra {
		t.Fatalf("unknown hook name is a programming error, not infrastructure (infra=false required): %q", output)
	}
	if !strings.Contains(output, "not found") {
		t.Fatalf("expected not-found detail for unknown hook, got: %q", output)
	}
}

// TestRunEmbeddedHook_AssertionCheck_ScrubsInheritedToolEnv pins the 2026-08-25
// review-minor fix: runEmbeddedHook must scrub the tool-input env vars inherited
// from the parent process. assertion-check picks batch mode (the gate's
// full-diff scan) by an EMPTY FORGE_FILE_PATH — an exported FORGE_FILE_PATH in
// the parent shell would silently drop the gate into per-edit analysis and the
// full scan would never run. Test data strings are assembled via concatenation
// so this file's own diff does not trip assertion-check on itself.
//
// TestRunEmbeddedHook_AssertionCheck_ScrubsInheritedToolEnv 钉住 2026-08-25
// review minor 修复：runEmbeddedHook 必须剔除从父进程继承的 tool-input env。
// assertion-check 以 FORGE_FILE_PATH 为空判别 batch 模式（门禁全量扫描）——
// 父 shell 里 export 的 FORGE_FILE_PATH 会把门禁静默降级成 per-edit 分析，全量
// 扫描永远不跑。测试数据字符串用拼接构造，避免本文件自身 diff 触发
// assertion-check。
func TestRunEmbeddedHook_AssertionCheck_ScrubsInheritedToolEnv(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "master")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")

	// Baseline with a real assertion, then a STAGED weakening (gate timing:
	// changes have landed) — batch mode must detect it.
	fatalCall := "t" + ".Fatalf"
	logCall := "t" + ".Log"
	old := "package x\nfunc TestFoo(t *testing.T) {\n\t" + fatalCall + "(\"expected 4\")\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "baseline")
	weakened := "package x\nfunc TestFoo(t *testing.T) {\n\t" + logCall + "(\"expected 4\")\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte(weakened), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "foo_test.go")

	// Polluted parent env: an exported FORGE_FILE_PATH must NOT reach the gate
	// script (it would flip batch mode into per-edit and skip the full scan).
	t.Setenv("FORGE_FILE_PATH", "foo_test.go")
	t.Setenv("FORGE_TOOL_NAME", "Edit")
	t.Setenv("FORGE_OLD_STRING", "x")
	t.Setenv("FORGE_NEW_STRING", "y")

	passed, infra, output := runEmbeddedHook(dir, "assertion-check")
	if infra {
		t.Fatalf("infrastructure failure must not occur in a normal repo: %q", output)
	}
	if !passed {
		t.Fatalf("assertion-check is advisory (always passes); got fail: %q", output)
	}
	if !strings.Contains(output, "疑似断言弱化") {
		t.Fatalf("batch mode must scan the staged diff despite inherited FORGE_FILE_PATH; got: %q", output)
	}
}

// --- Infra-failure classification (weekly-hardening 改动 3) ---
// The gate path used to treat every bash execution failure as a gate FAIL;
// 45/53 recorded FAILs were 'forge-gate-*.sh: No such file or directory' —
// infrastructure (bare PATH lookup resolving to WSL bash, which cannot see the
// Windows temp path), not a compile verdict. runEmbeddedHook now classifies
// spawn errors / exit 126/127 as infra; checkImplement records them with an
// INFRA: Detail prefix (Passed=false, so the infra rate stays countable) but
// does not fail the gate.

// TestRunEmbeddedHook_InfraFailure_SpawnError: a bash path that does not exist
// makes the spawn fail → passed=false, infra=true.
func TestRunEmbeddedHook_InfraFailure_SpawnError(t *testing.T) {
	old := findBashForHook
	t.Cleanup(func() { findBashForHook = old })
	findBashForHook = func() (string, error) {
		return filepath.Join(t.TempDir(), "no-such-bash-binary"), nil
	}

	passed, infra, output := runEmbeddedHook(t.TempDir(), "auto-compile")
	if passed {
		t.Fatalf("spawn failure must not pass: %q", output)
	}
	if !infra {
		t.Fatalf("spawn error must classify as infrastructure failure (infra=true), got infra=false: %q", output)
	}
}

// TestRunEmbeddedHook_InfraFailure_Exit127: bash exiting 127 (script file not
// found/readable — the WSL-bash-on-Windows signature) is infrastructure, not a
// gate verdict. Unix-only: the fake "bash" is a shebang script, which Go cannot
// spawn directly on Windows.
func TestRunEmbeddedHook_InfraFailure_Exit127(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang fake-bash cannot be spawned by Go on Windows — exit-code path covered on unix")
	}
	dir := t.TempDir()
	fakeBash := filepath.Join(dir, "fakebash")
	if err := os.WriteFile(fakeBash, []byte("#!/bin/bash\nexit 127\n"), 0755); err != nil {
		t.Fatal(err)
	}
	old := findBashForHook
	t.Cleanup(func() { findBashForHook = old })
	findBashForHook = func() (string, error) { return fakeBash, nil }

	passed, infra, output := runEmbeddedHook(t.TempDir(), "auto-compile")
	if passed {
		t.Fatalf("exit 127 must not pass: %q", output)
	}
	if !infra {
		t.Fatalf("bash exit 127 must classify as infrastructure failure (infra=true), got infra=false: %q", output)
	}
}

// TestCheckImplement_InfraFailureDoesNotFailGate: with bash unspawnable, BOTH
// embedded hooks report infrastructure failures — checkImplement must still
// pass the gate (fail-open, aligned with the write-time hook path's
// isHookInfraFailure philosophy) and the checklog entries must carry the
// INFRA: Detail prefix with Passed=false (infra-failure rate stays countable).
// The temp dir is not a git repo, so hasCodeChanges degrades gracefully to
// true — isolating the infra dimension under test.
func TestCheckImplement_InfraFailureDoesNotFailGate(t *testing.T) {
	dir := t.TempDir()
	old := findBashForHook
	t.Cleanup(func() { findBashForHook = old })
	findBashForHook = func() (string, error) {
		return filepath.Join(t.TempDir(), "no-such-bash-binary"), nil
	}

	result, err := checkImplement(dir, &TaskState{TaskRef: "feat/infra-test"})
	if err != nil {
		t.Fatalf("checkImplement returned error: %v", err)
	}
	if !result.Passed {
		t.Fatalf("infrastructure failure must NOT fail the gate (fail-open), got Passed=false: %s", result.Message)
	}

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var autoCompile, assertion *checklog.Entry
	for i := range entries {
		e := &entries[i]
		if e.TaskRef != "feat/infra-test" {
			continue
		}
		switch e.Check {
		case checklog.CheckAutoCompile:
			autoCompile = e
		case checklog.CheckAssertion:
			assertion = e
		}
	}
	for name, e := range map[string]*checklog.Entry{"auto-compile": autoCompile, "assertion-check": assertion} {
		if e == nil {
			t.Fatalf("%s checklog entry missing", name)
		}
		if e.Passed {
			t.Errorf("%s entry must keep Passed=false (infra-failure stats)", name)
		}
		if !strings.HasPrefix(e.Detail, "INFRA: ") {
			t.Errorf("%s entry Detail must carry the INFRA: prefix, got %q", name, e.Detail)
		}
	}
}
