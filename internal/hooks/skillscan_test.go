package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runSkillScanHook executes the REAL SkillScanHook script with a bash FUNCTION
// stub overriding the `forge` command, so the audit-scan call is driven by the
// test. Bash functions win over PATH executables (verified cross-platform), so
// this needs no fake binary and no PATH manipulation — unlike a PATH-based fake
// forge, which does NOT resolve reliably under Git Bash's semicolon PATH on
// Windows (semicolon PATH silently falls through to the real forge). HOME points
// at a temp dir containing .claude/skills so the hook's no-skills early-exit is
// bypassed and the real CODE/OUTPUT judgment runs.
//
// This is the behavioral layer that settings_test.go's containsString guards
// cannot reach: containsString proves the script *contains* the "扫描未完成"
// literal; this proves the script *actually emits* it when the scan crashes.
func runSkillScanHook(t *testing.T, forgeStubStdout, forgeStubStderr string, forgeStubExit int) string {
	t.Helper()
	// forge() function stub: controlled stdout + stderr + exit code, driven via
	// env vars so multi-line / unicode (✗) stdout needs no shell-quoting.
	stub := "forge() { printf '%s' \"$FORGE_STUB_OUT\"; printf '%s' \"$FORGE_STUB_ERR\" >&2; return \"$FORGE_STUB_CODE\"; }\n"
	script := stub + SkillScanHook

	tmp, err := os.CreateTemp("", "skill-scan-*.sh")
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	if _, err := tmp.WriteString(script); err != nil {
		t.Fatalf("write script: %v", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	// HOME with a .claude/skills dir so the hook's [ ! -d "$SCAN_DIR" ] early-exit
	// is bypassed — otherwise the script never reaches the forge call / judgment.
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}

	cmd := exec.Command("bash", tmp.Name())
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"FORGE_STUB_OUT=" + forgeStubStdout,
		"FORGE_STUB_ERR=" + forgeStubStderr,
		"FORGE_STUB_CODE=" + strconv.Itoa(forgeStubExit),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// skill-scan is advisory — it never exits non-zero by design. A non-zero
		// exit here means the hook script itself crashed (syntax error), which is
		// a test failure regardless of the scenario under test.
		t.Fatalf("SkillScanHook script exited non-zero (script bug): err=%v, out=%s", err, out)
	}
	return string(out)
}

// TestSkillScanHook_HonestSignalBranches is the behavioral guard for the
// honest-signal logic (code-review suggest#1). It executes the REAL SkillScanHook
// script across the four scan outcomes and asserts the emitted advisory line —
// not merely that the literal string exists in the source. If a future edit
// drops the `[ -z "$OUTPUT" ]` empty-output guard or mis-handles exit 4, these
// cases fail:
//   - crash (exit 1)            → "扫描未完成" (NOT a fake "all SAFE")
//   - empty output (exit 0)     → "扫描未完成" (the empty-OUTPUT guard — the fix's core)
//   - gate (exit 4) + risk line → "发现风险"   (exit 4 = success; must NOT read as crash)
//   - safe (exit 0, no ✗)       → "all skills SAFE"
func TestSkillScanHook_HonestSignalBranches(t *testing.T) {
	cases := []struct {
		name       string
		stubOut    string
		stubExit   int
		wantSubstr string
	}{
		{"crash exit1", "", 1, "扫描未完成"},
		{"crash empty-output exit0", "", 0, "扫描未完成"},
		{"gate exit4 with risk", "✗ evil-skill score=60 CRITICAL", 4, "发现风险"},
		{"all safe exit0", "✓ good-skill score=0", 0, "all skills SAFE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runSkillScanHook(t, c.stubOut, "", c.stubExit)
			if !strings.Contains(out, c.wantSubstr) {
				t.Errorf("scenario %q: SkillScanHook stdout = %q, want substring %q", c.name, out, c.wantSubstr)
			}
		})
	}
}

// TestSkillScanHook_CrashSurfacesStderr is the behavioral guard for the
// stderr-diagnostic improvement (code-review suggest#2). When the audit command
// crashes, exit code alone says "broke"; forge's stderr (panic / error) says
// "broke how". The crash branch must surface a truncated tail of stderr as a
// diagnostic hint, not discard it entirely via 2>/dev/null. Pre-fix the crash
// line dropped stderr on the floor; this fails until SkillScanHook captures it.
func TestSkillScanHook_CrashSurfacesStderr(t *testing.T) {
	const stderrMarker = "panic: nil pointer dereference at audit.go:42"
	out := runSkillScanHook(t, "", stderrMarker, 1)
	// The crash branch must (a) still report scan-incomplete, AND (b) echo the
	// distinctive stderr marker so the agent/user can see WHY the scan failed.
	if !strings.Contains(out, "扫描未完成") {
		t.Errorf("crash branch must still report scan-incomplete, got: %q", out)
	}
	if !strings.Contains(out, "panic: nil pointer") {
		t.Errorf("crash branch must surface forge stderr tail for diagnostics, got: %q", out)
	}
}
