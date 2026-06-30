package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	passed, output := runEmbeddedHook(dir, "auto-compile")

	// Advisory: passes without compiling — broken.go no longer fails the gate.
	if !passed {
		t.Fatalf("advisory auto-compile must PASS on a broken module (no compile enforced), got fail:\n%q", output)
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
	passed, output := runEmbeddedHook(dir, "auto-compile")
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
	passed, output := runEmbeddedHook(t.TempDir(), "no-such-hook")
	if passed {
		t.Fatalf("unknown hook must fail closed (passed=false), got passed=true: %q", output)
	}
	if !strings.Contains(output, "not found") {
		t.Fatalf("expected not-found detail for unknown hook, got: %q", output)
	}
}
