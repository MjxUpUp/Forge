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

// writeUntracked writes files into dir WITHOUT staging/committing — leaving
// them untracked, the working-tree shape at task-verify time before `git add`.
// Drives the untracked-files source added to taskChangedFiles: the agent's new
// files exist on disk but not yet in the index.
func writeUntracked(t *testing.T, dir string, files map[string]string) {
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
	ok, missing, _ := CheckTestCoverage(dir, state)
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
	ok, _, _ := CheckTestCoverage(dir2, state2)
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
		"pkg/executor.go":          "package pkg\n\nfunc Exec() int { return 1 }\n",
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
	ok, missing, _ := CheckTestCoverage(dir, state)
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
		"foo.go":         "package main\n\nfunc Foo() int { return 1 }\n",
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
		ok, missing, _ := CheckTestCoverage(dir, state)
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
		ok, missing, _ := CheckTestCoverage(dir, state)
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
		"internal/pkg/foo.go":             "package pkg\n\nfunc Foo() int { return 1 }\n",
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

// TestTestCoverageWhitelistsRustAndTauriEntryPoints dogfood 2.1②: the
// test-coverage gate was over-eager on Tauri command glue and Rust entry points.
// Under `src-tauri/`, `#[tauri::command]` and tokio::spawn are exercised
// end-to-end by the Tauri runtime; `main.rs`/`lib.rs` are Rust crate entry
// points whose integration tests live in `tests/`, not as same-package
// `_test.rs`. Exempting these avoids forcing non-existent unit tests. baseExact
// matches only the final path component; the substr carries a trailing slash to
// bound the directory.
//
// TestTestCoverageWhitelistsRustAndTauriEntryPoints dogfood 2.1②：测试覆盖门规
// 对 Tauri 命令胶水与 Rust 入口点过度敏感。`src-tauri/` 下 `#[tauri::command]`
// 与 tokio::spawn 由 Tauri 运行时跑端到端；`main.rs`/`lib.rs` 是 Rust crate
// 入口，集成测试放 `tests/`，不在同包下配 `_test.rs`。免测这些点避免强制写
// 不存在的单测。baseExact 仅匹配最终路径分量，substr 带尾斜杠保证目录范围。
func TestTestCoverageWhitelistsRustAndTauriEntryPoints(t *testing.T) {
	for _, p := range []string{
		"src/main.rs",               // Rust binary crate 入口
		"src/lib.rs",                // Rust lib crate 入口
		"src-tauri/src/main.rs",     // Tauri Rust 二进制入口
		"src-tauri/src/lib.rs",      // Tauri Rust lib
		"src-tauri/src/commands.rs", // #[tauri::command] 处理器
		"src-tauri/src/ipc.rs",      // tokio::spawn IPC 桥接
		"src-tauri/src/state.rs",    // Tauri 状态管理
		"src-tauri/src/cli.rs",      // src-tauri/ 子串命中，所有目录下文件均豁免
	} {
		if !isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = false; Rust entry / Tauri command glue must be exempt", p)
		}
	}
	// Negative case: sources neither at entry points (main.rs/lib.rs) nor under
	// src-tauri/ still require tests.
	//
	// 负向：不在 entry-point (main.rs/lib.rs) 也不在 src-tauri/ 目录下的源仍需测试。
	for _, p := range []string{
		"src/widget/click.rs",       // 普通 Rust 源码（非 main/lib）
		"src-tauri.rs",              // 根目录文件，非 src-tauri/ 目录下
		"src-tauri-helper/state.rs", // 邻近目录名 src-tauri-helper/，不被 src-tauri/ 命中
	} {
		if isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = true; 非入口/非 Tauri 目录源码不应被免测", p)
		}
	}
}

// TestTaskChangedFiles_ScopedToHeadCommit guards the task-scoped diff: two tasks
// on the same feature branch each commit their own work, and the later task's
// CheckTestCoverage sees only its own commits (HeadCommit..HEAD) without leaking
// the prior task's changes.
//
// Regression scenario: feat/evidence-chain stacked atop earlier branch commits;
// the old taskChangedFiles used main...HEAD and accumulated every unmerged commit
// on the branch (26 files), pressing the current task's testing dimension to 20.
// After prioritizing HeadCommit, scoring sees only the current task's scope.
//
// TestTaskChangedFiles_ScopedToHeadCommit 守卫任务范围 diff：两个任务在同一
// feature 分支上各 commit，后一个任务的 CheckTestCoverage 只看自己的 commit
// （HeadCommit..HEAD），不混入前一个任务的改动。
//
// 回归场景：feat/evidence-chain 叠在分支前序 commit 之上，旧 taskChangedFiles
// 用 main...HEAD 累积了分支上全部未合并 commit（26 文件），把当前任务的 testing
// 维度压到 20。HeadCommit 优先后，评分只看本任务范围。
func TestTaskChangedFiles_ScopedToHeadCommit(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)

	// Task 1: commit foo.go (no test) — the prior task's change, must not be
	// attributed to task 2.
	//
	// 任务1：commit foo.go（无 test）——前一个任务的改动，不该计入任务2。
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "task1: add foo")
	headAfterTask1 := GetHeadCommit(dir)

	// Task 2: commit bar.go + bar_test.go.
	//
	// 任务2：commit bar.go + bar_test.go。
	writeCommitSource(t, dir, map[string]string{
		"bar.go":      "package main\n\nfunc Bar() int { return 2 }\n",
		"bar_test.go": "package main\n\nimport \"testing\"\n\nfunc TestBar(t *testing.T) {}\n",
	}, "task2: add bar")

	// state2.HeadCommit = HEAD at task 2 start (= HEAD after task 1 finished).
	//
	// state2.HeadCommit = 任务2 启动时的 HEAD（= 任务1 结束后的 HEAD）。
	state2 := &TaskState{TaskRef: "task2", Branch: "feat/testcov", HeadCommit: headAfterTask1}

	ok, missing, total := CheckTestCoverage(dir, state2)
	// Old impl (main...HEAD) would yield total=2 with missing=[foo.go]:
	// it accumulated task 1.
	//
	// 旧实现（main...HEAD）会 total=2 且 missing=[foo.go]：累积了任务1。
	if total != 1 {
		t.Fatalf(`task2 scope should contain only bar.go: want total=1, got total=%d missing=%v (HeadCommit..HEAD must not accumulate task1's foo.go)`, total, missing)
	}
	if !ok {
		t.Fatalf(`bar.go has bar_test.go in task2 scope: want ok=true, got missing=%v`, missing)
	}
}

// TestTaskChangedFiles_IncludesUntracked guards the untracked-files blind-spot
// fix. At task-verify time the agent's new files are typically not yet
// `git add`-ed — the old taskChangedFiles read only `git diff HEAD` (tracked
// staged/unstaged) plus committed diffs, missing untracked files, so a freshly
// written foo_test.go could not satisfy a freshly changed foo.go and
// test-coverage false-reported no matching test (feat/task-scope hit this for
// real: task.go changed+tracked + task_scope_test.go new+untracked → false
// advisory). Adding `git ls-files --others --exclude-standard` makes detection
// run against the working tree the agent actually leaves behind.
//
// TestTaskChangedFiles_IncludesUntracked 守卫未跟踪文件盲点修复。task-verify 时机
// agent 的新文件通常还没 `git add`——旧 taskChangedFiles 只读 `git diff HEAD`
// （已跟踪的暂存/未暂存）+ 已提交 diff，看不到 untracked，导致刚写的 foo_test.go
// 无法满足刚改的 foo.go，test-coverage 误报「无对应测试」（feat/task-scope 实撞：
// task.go 已改已跟踪 + task_scope_test.go 新建未跟踪 → 假 advisory）。
// 加 `git ls-files --others --exclude-standard` 后，检测按 agent 真实留下的工作树跑。
func TestTaskChangedFiles_IncludesUntracked(t *testing.T) {
	t.Run("untracked_test_covers_untracked_source", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWithMaster(t, dir)
		// foo.go + foo_test.go are both left un-git-add-ed — two untracked
		// files, mirroring the working tree at verify time.
		//
		// foo.go + foo_test.go 都不 git add——两个 untracked，镜像 verify 时的工作树。
		writeUntracked(t, dir, map[string]string{
			"foo.go":      "package main\n\nfunc Foo() int { return 1 }\n",
			"foo_test.go": "package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n",
		})
		state := newVerifyState(t, dir, "untracked-pair")
		ok, missing, _ := CheckTestCoverage(dir, state)
		if !ok {
			t.Fatalf(`untracked foo.go + foo_test.go: want ok=true（untracked test 必须覆盖 untracked source）, got missing=%v`, missing)
		}
	})

	t.Run("untracked_source_without_test_still_detected", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWithMaster(t, dir)
		writeUntracked(t, dir, map[string]string{
			"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
		})
		state := newVerifyState(t, dir, "untracked-bare")
		ok, missing, _ := CheckTestCoverage(dir, state)
		// Detection must also run on untracked sources — otherwise admitting
		// untracked files becomes a free pass for untracked files.
		//
		// 检测必须对 untracked 源码也跑——否则「纳入 untracked」变成「放行 untracked」。
		if ok {
			t.Fatal(`untracked foo.go without test: want ok=false（检测必须覆盖 untracked 源码，不能因纳入而放行）`)
		}
		if len(missing) == 0 || missing[0] != "foo.go" {
			t.Fatalf(`want missing=[foo.go], got %v`, missing)
		}
	})

	t.Run("gitignored_untracked_excluded", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWithMaster(t, dir)
		// .gitignore excludes ignored.go; foo.go is not ignored. Both are
		// untracked.
		//
		// .gitignore 排除 ignored.go；foo.go 不忽略。两者都 untracked。
		writeUntracked(t, dir, map[string]string{
			".gitignore":  "ignored.go\n",
			"ignored.go":  "package main\n\nfunc Ignored() int { return 1 }\n",
			"foo.go":      "package main\n\nfunc Foo() int { return 1 }\n",
			"foo_test.go": "package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n",
		})
		changed := taskChangedFiles(dir, &TaskState{TaskRef: "gi", Branch: "feat/testcov"})
		foundIgnored := false
		for _, f := range changed {
			if f == "ignored.go" {
				foundIgnored = true
			}
		}
		if foundIgnored {
			t.Fatalf(`gitignored ignored.go must be excluded (--exclude-standard), got changed=%v`, changed)
		}
	})
}

// TestTestCoveragePerTaskOverride (plan 5 leak-prevention path):
// state.Overrides.TestCoverage=disable escapes without relying on the
// FORGE_TEST_COVERAGE env — this is the core value of a per-task override (one
// task escaping must not pollute other tasks in the same shell). Compared with
// TestTestCoverageEscapeHatch (the env path), this test pins the per-task path
// as independently usable. Two layers are verified: (1) CheckTestCoverage
// returns ok=true; (2) a CheckEscapeHatch entry is recorded (escape has a
// priced audit trail; Strength is capped at Weak accordingly). The env is
// explicitly cleared, proving the override path is independent of env.
//
// TestTestCoveragePerTaskOverride（方案5 防泄漏路径）：state.Overrides.TestCoverage="disable"
// 不靠 FORGE_TEST_COVERAGE env 也能逃生——这是 per-task override 的核心价值（一个任务逃生不污染
// 同 shell 的其他任务）。对照 TestTestCoverageEscapeHatch（env 路径），此测试钉住 per-task 路径
// 独立可用。验证两层：(1) CheckTestCoverage 返 ok=true；(2) 记 CheckEscapeHatch 条目（逃生有代价
// 的审计 trail，Strength 据此 cap Weak）。env 显式清空，证明 override 路径独立于 env。
func TestTestCoveragePerTaskOverride(t *testing.T) {
	t.Setenv("FORGE_TEST_COVERAGE", "") // 确保不是 env 在起作用——走 per-task override 路径
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")

	state := &TaskState{TaskRef: "per-task-cov", Branch: "feat/testcov"}
	state.Overrides.TestCoverage = "disable"

	// Layer 1: per-task override escapes the gate (env unset, independent path).
	//
	// 层1：per-task override 让门禁逃生（env 未设，独立路径）。
	ok, missing, _ := CheckTestCoverage(dir, state)
	if !ok {
		t.Fatalf("per-task override (no env): want ok=true, got missing=%v", missing)
	}
	// Layer 2: escape has a price — record CheckEscapeHatch audit trail
	// (UsedEscapeHatch → Strength capped at Weak).
	//
	// 层2：逃生有代价——记 CheckEscapeHatch 审计 trail（UsedEscapeHatch → Strength cap Weak）。
	entries, err := checklog.LoadForTask(dir, "per-task-cov")
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
		t.Fatal("per-task override must record CheckEscapeHatch (escape has a cost); got none")
	}
	if !found.Passed {
		t.Errorf("escape-hatch entry Passed=false, want true (hatch succeeded)")
	}
}

// TestTestCoverageShouldBlock pins the tiered decision of the task-complete
// hard-block backstop: the input is the count of changed source files WITHOUT a
// paired test (not total changed files) — many missing (≥3) plus zero assertions →
// block (corrupt success); few missing (≤2, fudge factor — e.g. partial coverage of
// an otherwise well-tested change) or with assertions (tests live elsewhere /
// refactor scenario) → pass. Eval evidence: feat/eval-core 0/19 and feat/m2 0/25
// (all files missing) should block; fix/m2-review-fixes 0/2 should pass (few missing).
//
// TestTestCoverageShouldBlock 钉死 task-complete 兜底硬阻断的分级判定：输入是「无配对
// 测试的改动源文件数」（非全部改动文件数）——缺测多（≥3）且零断言 → 阻断
// （corrupt success）；缺测少（≤2，fudge factor——如测试充分的改动只漏个别文件的
// 部分覆盖）或有断言（测试在别处/重构场景）→ 放行。eval 证据：feat/eval-core 0/19、
// feat/m2 0/25（全部缺测）应阻断；fix/m2-review-fixes 0/2 应放行（缺测少）。
func TestTestCoverageShouldBlock(t *testing.T) {
	cases := []struct {
		name    string
		missing int
		assertN int
		want    bool
	}{
		{"no source changed", 0, 0, false},
		{"few missing fudge factor (2 files, 0 assertion)", 2, 0, false},
		{"threshold boundary exactly 2 → pass", 2, 0, false},
		{"threshold boundary exactly 3 zero assertion → BLOCK", 3, 0, true},
		{"many missing zero assertion (3 files) → BLOCK", 3, 0, true},
		{"eval-core scale (19 files missing) → BLOCK", 19, 0, true},
		{"many missing but has assertions (refactor) → pass", 3, 5, false},
		{"many missing one assertion → pass (有验证痕迹)", 5, 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := testCoverageShouldBlock(c.missing, c.assertN); got != c.want {
				t.Errorf("testCoverageShouldBlock(missing=%d, assertN=%d) = %v, want %v", c.missing, c.assertN, got, c.want)
			}
		})
	}
}

// TestTaskCompleteTestCoverageHardGate_PartialCoverageAdvisoryPass pins the
// missing-count semantics: 3 source files changed, 2 with paired tests, 1 missing →
// len(missing)=1 < threshold → advisory pass (zero assertions). Before the fix the
// gate passed `total` (all changed source files) to testCoverageShouldBlock, so this
// scenario hard-blocked with a BLOCKED text claiming "改了 3 个源文件却无配对测试" —
// a lie (2 of the 3 did have tests). The documented semantics
// (testCoverageHardGateThreshold: 「无配对测试的源文件数 ≥ 阈值」) say advisory.
// Sources are split across packages so the same-package _test.go fallback does not
// accidentally cover the missing one.
//
// TestTaskCompleteTestCoverageHardGate_PartialCoverageAdvisoryPass 钉死 missing 计数
// 语义：改 3 个源文件，2 个有配对测试，1 个缺 → len(missing)=1 < 阈值 → 零断言也
// advisory 放行。修复前门禁把 total（全部改动源文件数）传给 testCoverageShouldBlock，
// 此场景会被硬阻断且 BLOCKED 文案谎称「改了 3 个源文件却无配对测试」（实际 2 个有
// 测试）——与文档语义（testCoverageHardGateThreshold：「无配对测试的源文件数 ≥
// 阈值」）矛盾。源文件分散在不同包，避免同包 _test.go fallback 意外覆盖缺测文件。
func TestTaskCompleteTestCoverageHardGate_PartialCoverageAdvisoryPass(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"pkg1/a.go":      "package pkg1\n\nfunc A() int { return 1 }\n",
		"pkg1/a_test.go": "package pkg1\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"pkg2/b.go":      "package pkg2\n\nfunc B() int { return 2 }\n",
		"pkg2/b_test.go": "package pkg2\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n",
		"pkg3/c.go":      "package pkg3\n\nfunc C() int { return 3 }\n",
	}, "add 3 sources, 2 with paired tests, 1 missing")

	state := newVerifyState(t, dir, "partial-coverage")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "")

	if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
		t.Fatalf("部分覆盖（缺测 1 个 < 阈值）应 advisory 放行——实现与文档语义（无配对测试的源文件数 ≥ 阈值才硬阻断）须一致, got: %v", err)
	}
}

// TestIsSourceFile_VendorExcluded pins the vendor baseline exclusion
// (weekly-hardening 4c): vendored dependencies are third-party baselines, not
// project source the task must pair tests with — a vendor update otherwise
// floods "missing tests" (cooking project: 986 files).
//
// TestIsSourceFile_VendorExcluded 钉死 vendor 基线排除（周复盘加固 4c）：
// vendor 依赖是第三方基线，不是本任务要配对测试的项目源码——一次 vendor
// 更新会把 "missing tests" 打爆（cooking 项目报 986 文件）。
func TestIsSourceFile_VendorExcluded(t *testing.T) {
	vendor := []string{
		"vendor/github.com/foo/bar/baz.go",
		"sub/vendor/mod/x.ts",
		"vendor/golang.org/x/sys/cpu.go",
	}
	for _, p := range vendor {
		if isSourceFile(p) {
			t.Errorf("vendor path %q must NOT count as source (third-party baseline)", p)
		}
	}
	nonVendor := []string{
		"internal/foo/bar.go",
		"vendorutil/x.go",   // "vendor" substring but no vendor/ segment
		"src/vendored/x.go", // vendored ≠ vendor/
		"internal/vendorx/y.go",
	}
	for _, p := range nonVendor {
		if !isSourceFile(p) {
			t.Errorf("non-vendor path %q must count as source", p)
		}
	}
}
