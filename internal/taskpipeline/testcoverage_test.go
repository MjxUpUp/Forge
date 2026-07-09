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

// TestTestCoverageWhitelistsRustAndTauriEntryPoints dogfood 2.1②：测试覆盖门规
// 对 Tauri 命令胶水与 Rust 入口点过度敏感。`src-tauri/` 下 `#[tauri::command]`
// 与 tokio::spawn 由 Tauri 运行时跑端到端；`main.rs`/`lib.rs` 是 Rust crate
// 入口，集成测试放 `tests/`，不在同包下配 `_test.rs`。免测这些点避免强制写
// 不存在的单测。baseExact 仅匹配最终路径分量，substr 带尾斜杠保证目录范围。
func TestTestCoverageWhitelistsRustAndTauriEntryPoints(t *testing.T) {
	for _, p := range []string{
		"src/main.rs",                   // Rust binary crate 入口
		"src/lib.rs",                    // Rust lib crate 入口
		"src-tauri/src/main.rs",         // Tauri Rust 二进制入口
		"src-tauri/src/lib.rs",          // Tauri Rust lib
		"src-tauri/src/commands.rs",     // #[tauri::command] 处理器
		"src-tauri/src/ipc.rs",          // tokio::spawn IPC 桥接
		"src-tauri/src/state.rs",        // Tauri 状态管理
		"src-tauri/src/cli.rs",          // src-tauri/ 子串命中，所有目录下文件均豁免
	} {
		if !isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = false; Rust entry / Tauri command glue must be exempt", p)
		}
	}
	// 负向：不在 entry-point (main.rs/lib.rs) 也不在 src-tauri/ 目录下的源仍需测试。
	for _, p := range []string{
		"src/widget/click.rs",        // 普通 Rust 源码（非 main/lib）
		"src-tauri.rs",               // 根目录文件，非 src-tauri/ 目录下
		"src-tauri-helper/state.rs",  // 邻近目录名 src-tauri-helper/，不被 src-tauri/ 命中
	} {
		if isWhitelisted(p) {
			t.Errorf("isWhitelisted(%q) = true; 非入口/非 Tauri 目录源码不应被免测", p)
		}
	}
}

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

	// 任务1：commit foo.go（无 test）——前一个任务的改动，不该计入任务2。
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "task1: add foo")
	headAfterTask1 := GetHeadCommit(dir)

	// 任务2：commit bar.go + bar_test.go。
	writeCommitSource(t, dir, map[string]string{
		"bar.go":      "package main\n\nfunc Bar() int { return 2 }\n",
		"bar_test.go": "package main\n\nimport \"testing\"\n\nfunc TestBar(t *testing.T) {}\n",
	}, "task2: add bar")

	// state2.HeadCommit = 任务2 启动时的 HEAD（= 任务1 结束后的 HEAD）。
	state2 := &TaskState{TaskRef: "task2", Branch: "feat/testcov", HeadCommit: headAfterTask1}

	ok, missing, total := CheckTestCoverage(dir, state2)
	// 旧实现（main...HEAD）会 total=2 且 missing=[foo.go]：累积了任务1。
	if total != 1 {
		t.Fatalf(`task2 scope should contain only bar.go: want total=1, got total=%d missing=%v (HeadCommit..HEAD must not accumulate task1's foo.go)`, total, missing)
	}
	if !ok {
		t.Fatalf(`bar.go has bar_test.go in task2 scope: want ok=true, got missing=%v`, missing)
	}
}

// TestTaskChangedFiles_IncludesUntracked 守卫未跟踪文件盲点修复。task-verify 时机
// agent 的新文件通常还没 `git add`——旧 taskChangedFiles 只读 `git diff HEAD`
// （已跟踪的暂存/未暂存）+ 已提交 diff，看不到 untracked，导致刚写的 foo_test.go
// 无法满足刚改的 foo.go，test-coverage 误报"无对应测试"（feat/task-scope 实撞：
// task.go 已改已跟踪 + task_scope_test.go 新建未跟踪 → 假 advisory）。
// 加 `git ls-files --others --exclude-standard` 后，检测按 agent 真实留下的工作树跑。
func TestTaskChangedFiles_IncludesUntracked(t *testing.T) {
	t.Run("untracked_test_covers_untracked_source", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWithMaster(t, dir)
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
		// 检测必须对 untracked 源码也跑——否则"纳入 untracked"变成"放行 untracked"。
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
