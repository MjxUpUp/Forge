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
//
// 2026-08-24 per-edit rewrite: the hook no longer scans the whole
// staged+unstaged diff in per-file mode (a stale/unrelated change re-fired the
// same advisory on EVERY subsequent Edit — the triple/quadruple-repeat incident).
// It analyzes exactly the change the current call introduces: Write = new
// content vs on-disk old content, Edit = old_string→new_string. The fixtures
// therefore fire the hook BEFORE the edit lands (production timing at
// PreToolUse), not after staging it.

// TestAssertionCheck_EditFatalNotWeakening: editing a t.Fatalf line (bumping an
// expected count 4->5) deletes and re-adds the assertion in equal measure —
// net zero. The net-count fix must NOT flag this. Regression for the
// false-positive that blocked legitimate assertion edits during codex/golden.
func TestAssertionCheck_EditFatalNotWeakening(t *testing.T) {
	dir := freshProject(t)
	fatalCall := "t" + ".Fatalf"
	// On-disk (pre-edit) content; the Write hasn't landed at PreToolUse time.
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+fatalCall+"(\"expected 4\")\n}\n")
	newContent := "package x\nfunc TestFoo(t *testing.T) {\n\t" + fatalCall + "(\"expected 5\")\n}\n"

	in := hookStdin(t, "sess-edit", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "foo_test.go"),
		"content":   newContent,
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("editing a t.Fatalf line must not trip assertion-check (net-zero); got block:\n%s", stdout)
	}
	if strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("net-zero t.Fatalf edit must not surface the advisory; got:\n%s", stdout)
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
	// Production timing: the on-disk file still holds the OLD content; the Write
	// tool call carries the NEW content. The hook must diff disk vs content.
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+fatalCall+"(\"expected 4\")\n}\n")
	newContent := "package x\nfunc TestFoo(t *testing.T) {\n\t" + logCall + "(\"expected 4\")\n}\n"

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

// TestAssertionCheck_EditOldNewAnalysis pins the per-edit Edit path: the hook
// compares old_string→new_string, not any worktree diff. old_string carrying a
// t.Fatalf that new_string drops must fire the advisory; an equal-measure edit
// must not — even when NO git diff exists for the file at all (the edit has not
// landed at PreToolUse time, so a diff-based implementation sees nothing).
func TestAssertionCheck_EditOldNewAnalysis(t *testing.T) {
	dir := freshProject(t)
	fatalCall := "t" + ".Fatalf"
	logCall := "t" + ".Log"
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+fatalCall+"(\"expected 4\")\n}\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "baseline")

	// Weakening edit: t.Fatalf -> t.Log in the replaced region.
	in := hookStdin(t, "sess-editweak", "PreToolUse", "Edit", map[string]any{
		"file_path":  filepath.Join(dir, "foo_test.go"),
		"old_string": "\t" + fatalCall + "(\"expected 4\")",
		"new_string": "\t" + logCall + "(\"expected 4\")",
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("advisory must not block; got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("Edit replacing t.Fatalf with t.Log must surface the advisory (old→new analysis); got:\n%s", stdout)
	}

	// Net-zero edit: only the constant changes.
	in = hookStdin(t, "sess-editzero", "PreToolUse", "Edit", map[string]any{
		"file_path":  filepath.Join(dir, "foo_test.go"),
		"old_string": "\t" + fatalCall + "(\"expected 4\")",
		"new_string": "\t" + fatalCall + "(\"expected 5\")",
	})
	stdout, _, err = forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("net-zero edit must not block; got block:\n%s", stdout)
	}
	if strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("net-zero Edit (constant bump) must not surface the advisory; got:\n%s", stdout)
	}
}

// TestAssertionCheck_ScopedToCurrentEdit is the 2026-08-24 triple-fire
// regression: a stale/unrelated change in ANOTHER test file (9 del / 1 add of
// t.Fatal lines in init_test.go) must NOT fire the advisory when the current
// Edit touches an unrelated file — the old whole-diff scan re-fired it on every
// subsequent Edit.
func TestAssertionCheck_ScopedToCurrentEdit(t *testing.T) {
	dir := freshProject(t)
	fatalCall := "t" + ".Fatalf"
	logCall := "t" + ".Log"
	// Stale unrelated change: init_test.go loses its t.Fatalf lines (never
	// committed — exactly the stale-diff shape from the incident).
	writeFile(t, dir, "init_test.go", "package x\nfunc TestA(t *testing.T) {\n\t"+fatalCall+"(\"a\")\n\t"+fatalCall+"(\"b\")\n}\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "baseline")
	writeFile(t, dir, "init_test.go", "package x\nfunc TestA(t *testing.T) {\n\t"+logCall+"(\"a\")\n\t"+logCall+"(\"b\")\n}\n")

	// Benign edit on a DIFFERENT test file: the stale init_test.go diff must not
	// leak into this call's verdict.
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+fatalCall+"(\"expected 4\")\n}\n")
	in := hookStdin(t, "sess-scoped", "PreToolUse", "Edit", map[string]any{
		"file_path":  filepath.Join(dir, "foo_test.go"),
		"old_string": "\t" + fatalCall + "(\"expected 4\")",
		"new_string": "\t" + fatalCall + "(\"expected 5\")",
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("benign edit must not block; got block:\n%s", stdout)
	}
	if strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("stale diff in ANOTHER test file must not fire the advisory on this edit (triple-fire regression); got:\n%s", stdout)
	}
}

// TestAssertionCheck_BatchModeScansDiff pins the batch/gate path: with no
// file_path (the task-implement gate runs the embedded script via executor's
// runEmbeddedHook), the whole staged+unstaged test-file diff IS scanned — that
// is the gate's job (judging the task's overall change).
func TestAssertionCheck_BatchModeScansDiff(t *testing.T) {
	dir := freshProject(t)
	fatalCall := "t" + ".Fatalf"
	logCall := "t" + ".Log"
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+fatalCall+"(\"expected 4\")\n}\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "baseline")
	// Real weakening, staged (gate time: changes have landed).
	writeFile(t, dir, "foo_test.go", "package x\nfunc TestFoo(t *testing.T) {\n\t"+logCall+"(\"expected 4\")\n}\n")
	git(t, dir, "add", "foo_test.go")

	in := hookStdin(t, "sess-batch", "PreToolUse", "Write", map[string]any{})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("batch mode must not block; got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("batch mode (no file_path) must still scan the full diff and detect the weakening; got:\n%s", stdout)
	}
}

// TestAssertionCheck_SessionDedupe pins the once-per-session marker: the same
// finding re-fired in one session is suppressed the second time (the agent
// already saw it; kimi queues advisories per emission, so unsuppressed repeats
// would also duplicate in the UserPromptSubmit drain).
func TestAssertionCheck_SessionDedupe(t *testing.T) {
	dir := freshProject(t)
	skipCall := "t" + ".Skip"
	content := "package x\nfunc TestFlaky(t *testing.T) {\n\t" + skipCall + "(\"flaky\")\n}\n"
	in := hookStdin(t, "sess-dedupe", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "flaky_test.go"),
		"content":   content,
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("first fire must not block; got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "疑似断言弱化") {
		t.Fatalf("first fire must surface the advisory; got:\n%s", stdout)
	}
	// Second identical fire, same session: suppressed by the session marker.
	stdout, _, err = forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("second fire must not block; got block:\n%s", stdout)
	}
	if strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("same finding in the same session must be suppressed on the second fire; got:\n%s", stdout)
	}
	// A DIFFERENT finding in the same session still fires (marker is per-finding).
	in2 := hookStdin(t, "sess-dedupe", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "flaky2_test.go"),
		"content":   "package x\nfunc TestFlaky2(t *testing.T) {\n\t" + skipCall + "()\n}\n",
	})
	stdout, _, err = forgeHook(t, dir, "assertion-check", in2)
	if err != nil {
		t.Fatalf("different finding must not block; got block:\n%s", stdout)
	}
	if !strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("a different finding in the same session must still fire; got:\n%s", stdout)
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
	if strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("t.Skip with rationale must not surface the advisory; got:\n%s", stdout)
	}
}

// TestAssertionCheck_SkipfFormatStringRationale is the 2026-08-24 false
// positive: t.Skipf whose rationale lives in the format string
// ("…(non-forge repo layout — nothing to regenerate against): %v") was flagged
// "t.Skip added without rationale" because the heuristic only knew a fixed
// keyword list. A skip carrying a multi-word explanation has a rationale.
func TestAssertionCheck_SkipfFormatStringRationale(t *testing.T) {
	dir := freshProject(t)
	skipfCall := "t" + ".Skipf"
	content := "package x\nfunc TestReadme(t *testing.T) {\n\t" + skipfCall + "(\"committed plugin README not found at %s (non-forge repo layout — nothing to regenerate against): %v\", committed, err)\n}\n"
	in := hookStdin(t, "sess-skipf", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, "pluginpack_test.go"),
		"content":   content,
	})
	stdout, _, err := forgeHook(t, dir, "assertion-check", in)
	if err != nil {
		t.Fatalf("t.Skipf with a rationale format string must be allowed, got block:\n%s", stdout)
	}
	if strings.Contains(stdout, "疑似断言弱化") {
		t.Errorf("t.Skipf with rationale in the format string must not be flagged; got:\n%s", stdout)
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
