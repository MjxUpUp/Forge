package agentbridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedTSCompiles is the semantic guard for the generated agent plugins.
// The wiring tests (TestOpencodePluginWiring / TestPiExtensionWiring) only
// assert `strings.Contains` — a generated TS file with a stray backtick, unbalanced
// brace, or a broken import passes those but is dead at runtime. This test writes
// the FULL generated TS to a temp dir alongside an ambient module stub for the
// agent SDK imports, then type-checks it with `tsc --noEmit` when tsc is on PATH.
//
// We deliberately check at a permissive level (no strict/noImplicitAny): the
// generated plugins run inside opencode/pi, whose own tsconfig supplies
// @types/node and strictness. Here we only verify the GENERATED source parses,
// resolves its imports, and is internally consistent — catching the failure
// modes string-contains cannot (e.g. a raw-string backtick bug that once split
// the generated file mid-token). tsc absent → t.Skip, not fail, so this never
// blocks on a machine without a TS toolchain.
func TestGeneratedTSCompiles(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not on PATH — skipping generated-TS type-check")
	}

	cases := []struct {
		name string
		ts   string
	}{
		{"opencode", buildOpencodePlugin()},
	}

	// Ambient stubs so tsc resolves the agent-SDK type imports without the real
	// packages installed. node:child_process resolves via @types/node when
	// present; when absent tsc still parses (skipLibCheck + permissive flags).
	stub := `declare module "@opencode-ai/plugin" {}
declare module "node:child_process" {
  export function spawn(cmd: string, args: string[], opts?: any): any;
}
// Node global Buffer — the generated TS annotates (d: Buffer). Declared here so
// the type-check resolves WITHOUT @types/node (CI test job installs none; a
// local machine with global @types/node hides the gap — exactly the v0.26.0
// release failure: local passed, CI failed with TS2591). Paired with
// --typeRoots <empty> below, the test is fully self-contained: identical
// behavior on local tsc and CI tsc.
interface Buffer {
  toString(encoding?: string): string;
}
`

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "plugin.ts"), []byte(c.ts), 0644); err != nil {
				t.Fatalf("write plugin.ts: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "stubs.d.ts"), []byte(stub), 0644); err != nil {
				t.Fatalf("write stubs.d.ts: %v", err)
			}
			// --typeRoots points at an EMPTY dir: tsc then auto-loads NO environment
			// @types (notably @types/node). This is what makes the test reproducible
			// across machines — a dev box with global @types/node and a CI runner
			// without it behave identically, the ONLY type info being stubs.d.ts
			// (Buffer, spawn, agent SDKs). Without it the test passed locally and
			// failed in CI with TS2591 (Cannot find name 'Buffer') on the v0.26.0
			// release. moduleResolution=bundler (TS 5.0+ recommended, never the
			// deprecated node10 → no TS5107 across tsc versions); module=esnext is
			// bundler's companion module setting.
			emptyTypeRoots := filepath.Join(dir, "no-types")
			if err := os.MkdirAll(emptyTypeRoots, 0755); err != nil {
				t.Fatalf("mkdir empty typeRoots: %v", err)
			}
			cmd := exec.Command(tsc, "--noEmit", "--skipLibCheck", "--target", "ES2022",
				"--module", "esnext", "--moduleResolution", "bundler",
				"--typeRoots", emptyTypeRoots,
				"plugin.ts", "stubs.d.ts")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("generated %s TS does not type-check (tsc exit %v):\n%s",
					c.name, err, strings.TrimSpace(string(out)))
			}
		})
	}
}

// TestSharedSpawnSnippetEmbedded guards the DRY refactor (fix#1): both generated
// TS files must embed the shared tsSharedForgeSpawn runForge snippet, so a
// protocol change lands once. Without this, buildOpencodePlugin/buildPiExtension
// could silently drop the `+ tsSharedForgeSpawn` concat and the wiring tests
// (which key on `forge hook <name>` in PRE_HOOKS, not on runForge) would not
// notice the spawn logic vanished.
func TestSharedSpawnSnippetEmbedded(t *testing.T) {
	for _, c := range []struct{ name, ts string }{
		{"opencode", buildOpencodePlugin()},
	} {
		// runForge's signature line is the unique anchor that the snippet is present.
		anchor := `function runForge(cmd: string, payload: object)`
		if !strings.Contains(c.ts, anchor) {
			t.Errorf("%s: generated TS missing shared runForge snippet (DRY concat dropped?) — must contain %q", c.name, anchor)
		}
		// And the snippet must appear exactly once (not duplicated by accident).
		if n := strings.Count(c.ts, anchor); n != 1 {
			t.Errorf("%s: runForge snippet appears %d times, want 1", c.name, n)
		}
	}
}
