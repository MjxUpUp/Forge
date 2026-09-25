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
// generated plugins run inside opencode, whose own tsconfig supplies
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
  // Single-command form (shell:true routes the whole line through cmd.exe).
  export function spawn(cmd: string, opts?: any): any;
}
// Node process global — forge_spawn.ts's win32 routing reads process.platform
// (same self-contained rationale as Buffer below).
declare const process: { platform: string };
// Node console global — the exit-code-aware close handler mirrors infrastructure
// failures to console.error (the opencode caller drops the error field, so
// stderr is the only channel an operator sees).
declare const console: { error(...args: unknown[]): void };
// Node global Buffer — the generated TS annotates (d: Buffer). Declared here so
// the type-check resolves WITHOUT @types/node (CI test job installs none; a
// local machine with global @types/node hides the gap — exactly the v0.26.0
// release failure: local passed, CI failed with TS2591). Paired with
// --typeRoots <empty> below, the test is fully self-contained: identical
// behavior on local tsc and CI tsc.
interface Buffer {
  toString(encoding?: string): string;
}
// Node timer globals used by runForge's 30s hang ceiling (same self-contained
// rationale as Buffer above — lib es2022 does not declare these).
declare function setTimeout(fn: () => void, ms: number): any;
declare function clearTimeout(t: any): void;
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

// TestForgeSpawnTimeoutContract pins the hang-freeze fix: runForge previously
// resolved only on error/close, so a hung forge process froze the agent's tool
// call forever (opencode has no harness timeout). The shared snippet must now
// carry a 30s ceiling that kills the child and fails open (block:false), in
// line with the spawn-error / parse-failure contract.
func TestForgeSpawnTimeoutContract(t *testing.T) {
	for _, want := range []string{
		"setTimeout",             // the ceiling exists
		"30000",                  // 30s
		"child.kill()",           // the hung process is actually killed
		"clearTimeout",           // no timer leak on the normal path
		"resolve({ block: false", // timeout path fails open
	} {
		if !strings.Contains(tsSharedForgeSpawn, want) {
			t.Errorf("forge_spawn.ts missing %q (hang-freeze timeout contract)", want)
		}
	}
	// runForge must never reject — the opencode post-hook relies on plain
	// `await runForge(...)` (no .catch) for its never-abort contract.
	if strings.Contains(tsSharedForgeSpawn, "reject(") {
		t.Error("forge_spawn.ts must not reject (fail-open resolve-only contract; opencode post-hook awaits without .catch)")
	}
}

// TestForgeSpawnWindowsContract pins the win32 npm-shim fix: npm lays out
// forge as forge + forge.cmd (no forge.exe), and CreateProcess cannot execute
// a .cmd shim — a bare spawn("forge") is ENOENT on Windows and every gate
// silently fails open (2026-08-20 reproduced live: PATH held only the npm
// shims while the dsh plugin's gates were all fail-open). The shared snippet
// must route win32 spawns through cmd.exe (shell) as ONE pre-built command
// string, and its timeout must tree-kill via taskkill /T — killing only the
// cmd.exe wrapper orphans the forge grandchild, which keeps the inherited
// stdio pipes open and pins the host's event loop forever (same fix as
// plugins/forge-dsh/lib/runner.js planSpawn).
func TestForgeSpawnWindowsContract(t *testing.T) {
	for _, want := range []string{
		`process.platform === "win32"`,       // the routing exists
		"shell: true",                        // …through cmd.exe
		`[bin, ...parts.slice(1)].join(" ")`, // one pre-built command string (args+shell is DEP0190)
		`/[&|()<>^,;=\s]/`,                   // …binary quoted on cmd metacharacters, not just spaces
		"taskkill",                           // timeout tree-kills shell + descendants
		`"/T", "/F"`,                         // …atomically, not just the wrapper
	} {
		if !strings.Contains(tsSharedForgeSpawn, want) {
			t.Errorf("forge_spawn.ts missing %q (win32 npm-shim contract)", want)
		}
	}
}

// TestForgeSpawnExitCodeContract pins the fix-asymmetry repair (2026-08-20
// review MEDIUM-1): the dsh runner got exit-code-aware empty-stdout handling
// but the embedded snippet still parsed unconditionally. On the win32 cmd.exe
// route a missing binary exits nonzero with its error on stderr and nothing
// on stdout — no spawn "error" event fires (cmd.exe itself started fine), so
// JSON.parse("") used to collapse it into an anonymous allow: every gate
// silently inert, zero diagnostics, the exact incident signature this whole
// fix addresses. The snippet must now split empty stdout by exit code AND
// mirror the failure to console.error (the opencode caller drops the error
// field — stderr is the only channel an operator sees). Also pins the taskkill
// fallback widening (review LOW-1): killer.on("close") must fall back to
// kill() when taskkill itself exits nonzero (access denied), not only when it
// fails to spawn.
func TestForgeSpawnExitCodeContract(t *testing.T) {
	for _, want := range []string{
		`if (out.trim() === "")`,              // empty stdout is classified before parsing
		"if (code !== 0) {",                   // …by exit code: nonzero ≠ clean silent allow
		"console.error(`[forge]",              // …and the failure reaches a visible channel
		`error?: string`,                      // the verdict carries the failure note
		`killer.on("close", (c: number) => {`, // taskkill nonzero-exit fallback (LOW-1)
		"if (c !== 0) fallback();",            // …so an access-denied taskkill still kills
		"forge spawn error:",                  // spawn-error path is diagnosed too (review follow-up LOW)
		"unparseable forge stdout",            // …as is the parse-failure path
		"timed out after 30000ms",             // …and the timeout path — no anonymous allows left
	} {
		if !strings.Contains(tsSharedForgeSpawn, want) {
			t.Errorf("forge_spawn.ts missing %q (exit-code/fallback contract)", want)
		}
	}
}
