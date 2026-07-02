package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// These tests guard the assertion-check fix. Test data strings (t.Fatalf /
// t.Skip calls written into temp fixtures) are assembled via the fatalCall /
// skipCall variables below rather than written as literals — otherwise this
// very test file's own diff would trip assertion-check on itself.

// TestAssertionCheck_EditFatalNotWeakening: editing a t.Fatalf line (bumping an
// expected count 4->5) deletes and re-adds the assertion in equal measure —
// net zero. The net-count fix must NOT flag this. Regression for the
// false-positive that blocked legitimate assertion edits during codex/golden.
//
// NOTE: file_path must point at the test file being edited (not a non-source
// file) — otherwise assertion-check's per-file branch hits its "non-source ->
// exit 0" guard at line 116 and the diff branch never runs, producing a false
// PASS. This subtlety is itself part of what the test pins.
func TestAssertionCheck_EditFatalNotWeakening(t *testing.T) {
	dir := freshProject(t)
	fatalCall := "t" + ".Fatalf"
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+fatalCall+"(\"expected 4\")\n}\n")
	git(t, dir, "add", "foo_test.go")
	git(t, dir, "commit", "-m", "baseline")
	newContent := "package x\nfunc TestFoo(t *testing.T) {\n\t" + fatalCall + "(\"expected 5\")\n}\n"
	writeFile(t, dir, "foo_test.go", newContent)
	git(t, dir, "add", "foo_test.go")

	in := hookStdin(t, "sess-edit", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "foo_test.go"),
		"content":   newContent,
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("editing a t.Fatalf line must not trip assertion-check (net-zero); got block:\n%s", stdout)
	}
}

// TestAssertionCheck_RealFatalRemoval: replacing t.Fatalf with t.Log is a real
// net reduction. v0.25 advisory: assertion-check no longer blocks (stdout always
// PASS, err == nil), but must STILL DETECT the weakening and surface it in stdout
// — forge turns stdout PASS detail into AdditionalContext shown to the agent, so
// the agent sees what to check. Guards that the advisory rewrite preserved the
// detection itself (only blocking was removed).
func TestAssertionCheck_RealFatalRemoval(t *testing.T) {
	dir := freshProject(t)
	fatalCall := "t" + ".Fatalf"
	logCall := "t" + ".Log"
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+fatalCall+"(\"expected 4\")\n}\n")
	git(t, dir, "add", "foo_test.go")
	git(t, dir, "commit", "-m", "baseline")
	newContent := "package x\nfunc TestFoo(t *testing.T) {\n\t" + logCall + "(\"expected 4\")\n}\n"
	writeFile(t, dir, "foo_test.go", newContent)
	git(t, dir, "add", "foo_test.go")

	in := hookStdin(t, "sess-remove", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "foo_test.go"),
		"content":   newContent,
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("advisory assertion-check must not block even on real t.Fatalf weakening; got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("advisory must still detect t.Fatalf→t.Log weakening and surface it in stdout; got:\n%s", stdout)
	}
}

// TestAssertionCheck_SkipWithRationale: t.Skip carrying a rationale keyword
// (regenerate/bootstrap/first run) is a legitimate skip (fixture generators,
// env guards). Must be allowed.
func TestAssertionCheck_SkipWithRationale(t *testing.T) {
	dir := freshProject(t)
	skipCall := "t" + ".Skip"
	content := "package x\nfunc TestGen(t *testing.T) {\n\t" + skipCall + "(\"regenerate fixtures on first run\")\n}\n"
	in := hookStdin(t, "sess-skipok", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "gen_test.go"),
		"content":   content,
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("t.Skip with rationale keyword must be allowed, got block:\n%s", stdout)
	}
}

// TestAssertionCheck_BareSkip: a bare t.Skip with no rationale is the weakening
// pattern (skipping a failing test). v0.25 advisory: no block, but must still
// detect it and surface the warning in stdout.
func TestAssertionCheck_BareSkip(t *testing.T) {
	dir := freshProject(t)
	skipCall := "t" + ".Skip"
	content := "package x\nfunc TestFlaky(t *testing.T) {\n\t" + skipCall + "(\"flaky\")\n}\n"
	in := hookStdin(t, "sess-skipbad", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "flaky_test.go"),
		"content":   content,
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("advisory assertion-check must not block on bare t.Skip; got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("advisory must still detect bare t.Skip and surface it in stdout; got:\n%s", stdout)
	}
}
