package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// writeCommitSource writes the given files (relative path → content) into dir,
// stages and commits them on the current branch. Used to drive the real
// git diff path the gate inspects.
func writeCommitSource(t *testing.T, dir string, files map[string]string, msg string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", msg)
}

// initRepoWithMaster sets up a git repo with an initial commit on master, then
// creates a feat/testcov branch — mirroring the real `forge task start --branch`
// workflow (feature branches always descend from master). The task-coverage
// gate's `master...HEAD` diff needs this ancestry to see task-only changes.
func initRepoWithMaster(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/testcov")
}

// newVerifyState builds a TaskState with task-implement passed and a feature
// branch, ready for a task-verify gate call. Reads are seeded into toollog so
// the work-activity check (read-before-edit) does not pre-empt the test.
func newVerifyState(t *testing.T, dir, ref string) *TaskState {
	t.Helper()
	state := &TaskState{TaskRef: ref, Branch: "feat/testcov"}
	state.RecordGateResult("task-implement", true, "")
	base := time.Now().Add(2 * time.Second)
	rr := toolusage.ToolCall{ToolName: "Read", TaskRef: ref, Timestamp: base}
	if err := toolusage.Record(dir, &rr); err != nil {
		t.Fatalf("seed Read: %v", err)
	}
	return state
}

// TestTestCoveragePassesWithNoSourceChanges: empty working tree + no task
// commits → gate passes. Guards against false positives on activity-only
// tasks (the existing activity_ratio_test.go scenarios).
func TestTestCoveragePassesWithNoSourceChanges(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)

	state := newVerifyState(t, dir, "no-changes")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS when no source changed: %v", err)
	}
}

// TestTestCoverageAdvisoryWhenSourceLacksTest: foo.go changed, no foo_test.go.
// v0.25: task-verify no longer FAILs (advisory) — it passes with a stderr
// reminder. The detection itself (missing=[foo.go]) is locked by
// TestCheckTestCoverage_Direct; this test only guards the non-blocking gate.
func TestTestCoverageAdvisoryWhenSourceLacksTest(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")

	state := newVerifyState(t, dir, "lacks-test")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS (advisory) even when foo.go lacks foo_test.go: %v", err)
	}
}

// TestTestCoveragePassesWhenTestPresent: foo.go + foo_test.go → PASS.
func TestTestCoveragePassesWhenTestPresent(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go":      "package main\n\nfunc Foo() int { return 1 }\n",
		"foo_test.go": "package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n",
	}, "add foo + test")

	state := newVerifyState(t, dir, "with-test")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS when foo.go + foo_test.go changed: %v", err)
	}
}

// TestTestCoverageWhitelistsEntryPoints: main.go (entry) needs no test.
func TestTestCoverageWhitelistsEntryPoints(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	}, "add entry")

	state := newVerifyState(t, dir, "entry")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS for whitelisted main.go: %v", err)
	}
}

// TestTestCoverageWhitelistsGenerated: *.gen.* and *_generated.* need no test.
func TestTestCoverageWhitelistsGenerated(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"api.gen.go":         "package api\n\n// generated\n",
		"types_generated.go": "package api\n\n// generated\n",
	}, "add generated")

	state := newVerifyState(t, dir, "gen")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS for generated files: %v", err)
	}
}

// TestTestCoverageEscapeHatch: FORGE_TEST_COVERAGE=disable bypasses the check.
func TestTestCoverageEscapeHatch(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")

	state := newVerifyState(t, dir, "bypass")
	t.Setenv("FORGE_TEST_COVERAGE", "disable")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS with escape hatch: %v", err)
	}
}

// TestTestCoverageEscapeHatchAuditsToChecklog guards the A4 fix: the
// FORGE_TEST_COVERAGE=disable escape hatch is legitimate but its use must leave
// a trail — otherwise an agent silently dodges the test-coverage gate. The
// bypass records an escape-hatch checklog entry queryable via `forge trace`.
func TestTestCoverageEscapeHatchAuditsToChecklog(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")

	state := newVerifyState(t, dir, "hatch-cov")
	t.Setenv("FORGE_TEST_COVERAGE", "disable")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS with escape hatch: %v", err)
	}

	entries, err := checklog.LoadForTask(dir, "hatch-cov")
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	var found *checklog.Entry
	for i := range entries {
		if entries[i].Check == checklog.CheckEscapeHatch {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("escape-hatch checklog entry not recorded for FORGE_TEST_COVERAGE=disable")
	}
	if !found.Passed {
		t.Errorf("escape-hatch entry Passed = false, want true (hatch succeeded)")
	}
	if !strings.Contains(found.Detail, "FORGE_TEST_COVERAGE") {
		t.Errorf("escape-hatch detail = %q, want it to mention FORGE_TEST_COVERAGE", found.Detail)
	}
}

// TestTestCoverageWhitelistIsExact guards the whitelist from being too loose:
// "main.go" must NOT match "remain.go" or "domain.go", and "types.go" must NOT
// match "prototypes.go". baseExact matches the whole final component only.
// v0.25: gate is advisory (always passes), so whitelist precision is verified
// at the detection layer — remain.go is NOT whitelisted → missing.
func TestTestCoverageWhitelistIsExact(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// remain.go is a real source file, NOT the entry point main.go.
	writeCommitSource(t, dir, map[string]string{
		"remain.go": "package main\n\nfunc Remain() int { return 2 }\n",
	}, "add non-entry")

	state := newVerifyState(t, dir, "non-entry")
	ok, missing := CheckTestCoverage(dir, state)
	if ok {
		t.Fatal("remain.go should be detected as missing a test (not whitelisted as main.go)")
	}
	if len(missing) == 0 || missing[0] != "remain.go" {
		t.Fatalf("want missing=[remain.go], got %v", missing)
	}
}

// TestTestCoverageInfersByLanguage: TS source with .test.ts sibling passes;
// without it fails. Guards the per-language inference branch.
func TestTestCoverageInfersByLanguage(t *testing.T) {
	// With matching .test.ts.
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"src/a.ts":      "export const a = 1\n",
		"src/a.test.ts": "import './a'\n",
	}, "add ts + test")
	state := newVerifyState(t, dir, "ts-with")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("ts source with .test.ts should PASS: %v", err)
	}

	// Without matching test — v0.25: gate is advisory, so verify detection directly.
	dir2 := t.TempDir()
	initRepoWithMaster(t, dir2)
	writeCommitSource(t, dir2, map[string]string{
		"src/a.ts": "export const a = 1\n",
	}, "add ts only")
	state2 := newVerifyState(t, dir2, "ts-without")
	ok, _ := CheckTestCoverage(dir2, state2)
	if ok {
		t.Fatal("ts source without .test.ts should be detected as missing (advisory gate, but detection still runs)")
	}
}

// TestTestCoverageGoPackageFallback: a Go source whose conventional _test.go
// is absent but whose SAME DIRECTORY has another _test.go change must PASS.
// This is the common Go pattern — tests are package-scoped, so
// testcoverage_test.go legitimately covers executor.go. Without the fallback the
// gate falsely fails well-tested packages.
func TestTestCoverageGoPackageFallback(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		// executor.go has NO executor_test.go change, but sibling
		// testcoverage_test.go in the same package covers it.
		"pkg/executor.go":         "package pkg\n\nfunc Exec() int { return 1 }\n",
		"pkg/testcoverage_test.go": "package pkg\n\nimport \"testing\"\n\nfunc TestExec(t *testing.T) {}\n",
	}, "add source + sibling test")

	state := newVerifyState(t, dir, "pkg-fallback")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS via package-level _test.go fallback: %v", err)
	}
}

// TestTestCoverageGoPackageFallbackStillFailsWhenDirUntested: the fallback must
// not become a free pass. A source whose directory has NO _test.go change at
// all must still be detected as missing — this is the "genuinely untested" case.
// v0.25: gate is advisory, so the same-directory constraint is verified at the
// detection layer.
func TestTestCoverageGoPackageFallbackStillFailsWhenDirUntested(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		// foo.go in pkg/, but the only test change is in a DIFFERENT directory
		// (other/pkg) — must not satisfy the same-directory fallback.
		"pkg/foo.go":            "package pkg\n\nfunc Foo() int { return 1 }\n",
		"other/pkg/bar_test.go": "package pkg\n\nimport \"testing\"\n\nfunc TestBar(t *testing.T) {}\n",
	}, "add source + unrelated test")

	state := newVerifyState(t, dir, "pkg-untested")
	ok, missing := CheckTestCoverage(dir, state)
	if ok {
		t.Fatal("foo.go with no same-dir _test.go should be detected as missing (fallback is same-directory)")
	}
	foundFoo := false
	for _, m := range missing {
		if m == "pkg/foo.go" {
			foundFoo = true
		}
	}
	if !foundFoo {
		t.Fatalf("want missing to contain pkg/foo.go, got %v", missing)
	}
}

// TestTestCoverageGoPackageFallbackRootLevel: a root-level source covered by a
// root-level _test.go (no directory) must pass the fallback.
func TestTestCoverageGoPackageFallbackRootLevel(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go":      "package main\n\nfunc Foo() int { return 1 }\n",
		"helper_test.go": "package main\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) {}\n",
	}, "add root source + root test")

	state := newVerifyState(t, dir, "root-fallback")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS via root-level _test.go fallback: %v", err)
	}
}

// TestCheckTestCoverage_Direct calls the exported gate logic directly (not via
// ExecuteTaskGate) to lock the contract scoreTask's live fallback relies on.
// In the commit-after-start shape — changes committed on the feature branch,
// working tree clean — CheckTestCoverage still sees them via master...HEAD and
// returns the right verdict. This is the EXACT scenario the old scoreTesting
// diff heuristic misread as "no tests" (HeadCommit==HEAD → empty diff → 20),
// which unfairly penalized ~half of one audited project's tasks.
func TestCheckTestCoverage_Direct(t *testing.T) {
	t.Run("passes_when_test_committed", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWithMaster(t, dir)
		writeCommitSource(t, dir, map[string]string{
			"foo.go":      "package main\n\nfunc Foo() int { return 1 }\n",
			"foo_test.go": "package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n",
		}, "add foo + test")

		state := &TaskState{TaskRef: "direct-pass", Branch: "feat/testcov"}
		ok, missing := CheckTestCoverage(dir, state)
		if !ok {
			t.Fatalf("committed foo.go + foo_test.go: want ok=true, got missing=%v", missing)
		}
	})

	t.Run("fails_when_test_missing_committed", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWithMaster(t, dir)
		writeCommitSource(t, dir, map[string]string{
			"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
		}, "add foo only")

		state := &TaskState{TaskRef: "direct-fail", Branch: "feat/testcov"}
		ok, missing := CheckTestCoverage(dir, state)
		if ok {
			t.Fatalf("committed foo.go without test: want ok=false, got ok=true")
		}
		if len(missing) == 0 {
			t.Fatal("want missing=[foo.go], got empty")
		}
	})
}

// TestTestCoverageGoPackageFallback_NestedDir guards the cross-platform
// package-fallback for a NESTED directory. filepath.Dir returns backslashes on
// Windows (Clean converts '/') while git reports forward slashes — without the
// ToSlash normalization in hasMatchingTest, this multi-directory case fails to
// match on Windows even though the single-directory TestTestCoverageGoPackageFallback
// passes. This is the shape that blocked the B3 fix's own task-verify gate.
func TestTestCoverageGoPackageFallback_NestedDir(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"internal/pkg/foo.go":            "package pkg\n\nfunc Foo() int { return 1 }\n",
		"internal/pkg/foo_helper_test.go": "package pkg\n\nimport \"testing\"\n\nfunc TestFooHelper(t *testing.T) {}\n",
	}, "add nested source + sibling test")

	state := newVerifyState(t, dir, "nested-fallback")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS via nested-dir _test.go fallback: %v", err)
	}
}

// TestTestCoverageWhitelistsSkillsAssetDir: forge ships its bundled skill
// library at skills/* via go:embed. Those are distributed skill scripts/docs
// the AI consumes — not compiled/tested units — so committing a .ts skill
// script must NOT trip the test-coverage gate. This is the regression that
// blocked feat/skills-merge: 16 skill scripts falsely reported as untested.
func TestTestCoverageWhitelistsSkillsAssetDir(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"skills/arkts-runtime-fix/scripts/collect-hilog.ts": "// distributed skill asset, no unit test\nexport const x = 1\n",
	}, "add skill asset")

	state := newVerifyState(t, dir, "skills-asset")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("task-verify should PASS for skills/ embedded asset: %v", err)
	}
}

// TestTestCoverageWhitelistSkillsDirIsPrecise: the "skills/" asset exemption
// must NOT shadow real forge source whose name merely contains "skills".
// Guards against an over-broad substr match letting internal/cli/skills_install.go
// (or any skills*/internal source) skip its test requirement.
func TestTestCoverageWhitelistSkillsDirIsPrecise(t *testing.T) {
	for _, p := range []string{
		"internal/cli/skills.go",
		"internal/cli/skills_install.go",
		"internal/skillsdist/install.go",
		"internal/skillseval/eval.go",
	} {
		if isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = true; forge source named skills* must NOT be exempt", p)
		}
	}
	for _, p := range []string{
		"skills/embed.go",
		"skills/arkts-runtime-fix/scripts/collect-hilog.ts",
		"skills/skill-routing/adapters/pi/index.ts",
	} {
		if !isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = false; skills/ embedded asset must be exempt", p)
		}
	}
}

// TestTestCoverageWhitelistsHookEmbedContainer: internal/hooks/embed.go holds
// shell scripts as Go string constants (HazardGuardHook, WorkflowTestGuardHook,
// …) — no Go logic to unit-test; the scripts' behavior is exercised end-to-end
// by internal/e2e. baseExact="embed.go" exempts it without a same-package
// embed_test.go. This is the regression surfaced by ce9b2410: the
// file-level hasMatchingTest check flagged embed.go as "changed source without
// a test" even though its coverage lives in internal/e2e.
func TestTestCoverageWhitelistsHookEmbedContainer(t *testing.T) {
	for _, p := range []string{
		"internal/hooks/embed.go",
		"hooks/embed.go",
	} {
		if !isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = false; hook-script container embed.go must be exempt", p)
		}
	}
	// baseExact precision: only a final component equal to "embed.go" matches.
	// Real forge source whose name merely contains "embed" stays required.
	for _, p := range []string{
		"internal/hooks/embed_test_support.go",
		"internal/hooks/hooks.go",
		"internal/agentbridge/embedder.go",
	} {
		if isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = true; non-embed.go source must NOT be exempt", p)
		}
	}
}
