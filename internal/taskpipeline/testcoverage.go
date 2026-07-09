package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// testCoverageDisableEnv lets a project opt out of the test-coverage gate
// (symmetric to FORGE_WORK_ACTIVITY). Legitimate cases: doc-only repos,
// generated-code-heavy modules, or a task touching only whitelist files.
// The CLI surfaces this escape hatch in the gate-failure message so it is
// never silently bypassed.
const testCoverageDisableEnv = "FORGE_TEST_COVERAGE"

// sourceExts 是 test-coverage 门控认定的"源码"后缀集。早期有一层 bash advisory hook
// 镜像此集合，hook 精简时已删——本门控现是唯一真相源（hooks/embed.go 不复存在）。
var sourceExts = map[string]bool{
	".go": true, ".rs": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".py": true, ".java": true,
	".rb": true, ".zig": true, ".nim": true,
}

// testCoverageWhitelist describes source files exempt from the test requirement:
//   - entry points: main.go, cmd/** main entry binaries
//   - generated code: *.gen.*, *_generated.*, *.pb.* protobuf bindings
//   - pure type/protocol definitions: no executable logic to test
//   - embedded asset directories: go:embed content shipped as runtime data
//
// Matched against the forward-slash repo-relative path.
// (An earlier revision mirrored this list in a bash advisory hook; that hook
// was trimmed away, so this gate layer is now the single source of truth.)
type whitelistEntry struct {
	// substr matches anywhere in the path (e.g. ".gen.", "/dto/").
	substr string
	// baseExact matches the final path component exactly (e.g. "main.go").
	baseExact string
}

var testCoverageWhitelist = []whitelistEntry{
	// Entry points.
	{baseExact: "main.go"},
	{substr: "cmd/"},
	// Generated code.
	{substr: ".gen."},
	{substr: "_generated."},
	{substr: ".pb.go"},
	{substr: ".pb.rs"},
	{substr: ".pb.dart"},
	// Pure type/protocol/dto definitions.
	{baseExact: "types.ts"},
	{baseExact: "types.js"},
	{baseExact: "types.go"},
	{substr: "/dto/"},
	{baseExact: "dto.go"},
	{substr: "/models/"},
	// Embedded asset directories: bundled content shipped as runtime data, not
	// project source under test. forge carries its skill library at skills/*
	// (distributed skill scripts/docs the AI consumes — not compiled/tested
	// units). Without this exemption every committed skill script (.ts/.py)
	// falsely fails the gate. Matches "skills/" so it spares the root asset dir
	// without touching same-named source like internal/cli/skills_install.go.
	{substr: "skills/"},
	// Embedded hook-script containers: internal/hooks/embed.go holds shell scripts
	// as Go string constants (HazardGuardHook, WorkflowTestGuardHook, …). There is
	// no Go logic to unit-test — the scripts' behavior is exercised end-to-end by
	// internal/e2e (e.g. TestHook_HazardGuard_BlocksHazardousCommand). Without this
	// exemption, the file-level hasMatchingTest check (looks for embed_test.go in
	// the same package) falsely flags it as "changed source without a test".
	{baseExact: "embed.go"},
	// Rust entry points — parity with `main.go` for Go crates. baseExact matches
	// the basename, so `src/main.rs` and `src-tauri/src/main.rs` both qualify.
	// Rust convention: binaries declare `src/main.rs`, libraries `src/lib.rs`
	// (or `src-tauri/src/lib.rs` for the Tauri side). Integration tests live
	// under `tests/` rather than as parallel `_test.rs` siblings, and the
	// harness tests the binary via `cargo run`/`cargo test` — file-level
	// hasMatchingTest falsely flags these entry crates. dogfood 2.1②.
	{baseExact: "main.rs"},
	{baseExact: "lib.rs"},
	// Tauri command-glue directory — `src-tauri/src/` holds `#[tauri::command]`
	// handlers and tokio::spawn IPC bridge code. The Tauri runtime exercises
	// these end-to-end via `cargo tauri dev/build`, not via unit tests in the
	// same file; the conventional `__tests__` placement doesn't apply. The
	// trailing slash makes the match directory-scoped — a mixed Rust+TS
	// project root `src/` is unaffected. dogfood 2.1②.
	{substr: "src-tauri/"},
}

// CheckNameTestCoverage is the checklog entry name for the gate test-coverage
// decision, so trace shows the gate verdict (not just the per-edit WARNs).
const CheckNameTestCoverage checklog.CheckName = "test-coverage-gate"

// CheckTestCoverage enforces CLAUDE.md rule 4 ("测试伴随变更"): every non-whitelisted
// source file changed during the task must have a corresponding test file also
// changed. Returns (ok, missing, total): ok=true when no source was changed,
// every changed source is whitelisted, or each has a matching test; total is
// the count of changed source files requiring a test (covered = total-len(missing)),
// which the scoring dimension uses for continuous grading instead of binary ok.
//
// Exported so scoring (cli.scoreTask) can reuse the EXACT verdict the task-verify
// gate computed, instead of re-deriving coverage from the git diff — which
// understated coverage whenever task changes were committed before task start
// (HeadCommit == HEAD → empty diff → testing dimension saw no tests).
//
// Gracefully degrades: non-git repo or empty diff → ok=true (no false positives).
func CheckTestCoverage(root string, state *TaskState) (ok bool, missing []string, total int) {
	if os.Getenv(testCoverageDisableEnv) == "disable" {
		// A4: audit the bypass. The hatch is legitimate (doc-only repos, generated
		// code, whitelist-only tasks) but its use must leave a trail — otherwise an
		// agent can silently dodge the test-coverage gate by exporting the env.
		taskRef := ""
		if state != nil {
			taskRef = state.TaskRef
		}
		checklog.Record(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			TaskRef: taskRef,
			Detail:  "escape-hatch: FORGE_TEST_COVERAGE=disable (test-coverage gate bypassed)",
		})
		return true, nil, 0
	}

	changed := taskChangedFiles(root, state)
	if len(changed) == 0 {
		return true, nil, 0
	}

	changedSet := make(map[string]bool, len(changed))
	for _, f := range changed {
		changedSet[f] = true
	}

	for _, f := range changed {
		if !isSourceFile(f) || isTestFile(f) {
			continue
		}
		if isWhitelisted(f) {
			continue
		}
		total++ // 应配对测试的源码文件数（评分按 covered/total 连续打分）
		if hasMatchingTest(f, changedSet) {
			continue
		}
		missing = append(missing, f)
	}

	return len(missing) == 0, missing, total
}

// taskChangedFiles returns the set of files changed during the task.
// Committed changes since the task's HeadCommit (captured at task start) plus
// the working tree — so covered/total counts only this task's files, aligned
// with scoring.resolveDiffBase. Without this, tasks sharing one feature branch
// accumulate prior tasks' commits into main...HEAD (the feat/evidence-chain
// regression: testing=20 on a fully-tested change). Falls back to main...HEAD
// only when HeadCommit is absent (legacy states). Empty on a clean tree with
// no task-specific commits.
func taskChangedFiles(root string, state *TaskState) []string {
	var files []string
	seen := make(map[string]bool)

	add := func(out []byte) {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			files = append(files, line)
		}
	}

	// 1. Committed changes during THIS task. Prefers the task's HeadCommit
	// (captured at task start) so covered/total counts only this task's files —
	// aligned with scoring.resolveDiffBase. Without this, tasks sharing one
	// feature branch accumulate prior tasks' commits into main...HEAD, inflating
	// the testing dimension and mis-flagging the current task's well-tested files
	// as missing (the feat/evidence-chain regression: testing=20 on a fully-tested
	// change). Falls back to main...HEAD only when no HeadCommit is recorded
	// (legacy states / the pre-start shape the test suite models).
	if state != nil {
		if state.HeadCommit != "" {
			out, err := exec.Command("git", "-C", root, "diff", "--name-only", state.HeadCommit+"..HEAD").Output()
			if err == nil {
				add(out)
			}
		} else if state.Branch != "" && state.Branch != "main" && state.Branch != "master" {
			for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
				out, err := exec.Command("git", "-C", root, "diff", "--name-only", base+"...HEAD").Output()
				if err == nil {
					add(out)
					break
				}
			}
		}
	}

	// Working tree (staged + unstaged), always relevant — covers uncommitted edits.
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", "HEAD").Output()
	if err == nil {
		add(out)
	}

	// Untracked files — newly created, not yet `git add`ed. At task-verify time
	// the agent's new files are typically still untracked, so without this source
	// the gate is blind to them: a just-written foo_test.go can't satisfy its
	// just-modified foo.go sibling, and test-coverage falsely reports "no matching
	// test" for the exact files that DO have tests (the feat/task-scope hit:
	// task.go modified-tracked + task_scope_test.go untracked → false advisory).
	// --exclude-standard keeps .gitignored content out (node_modules, build
	// output, the stray dashboard-render.png) so only genuine working-tree source
	// is considered. Repo-wide to match the working-tree semantics above — only
	// the committed HeadCommit..HEAD part is task-scoped. Also fixes scope-drift's
	// symmetric blind spot (an untracked file outside PlanScope was invisible).
	out, err = exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard").Output()
	if err == nil {
		add(out)
	}

	return files
}

// isSourceFile reports whether path is a source file (not a test, not config).
func isSourceFile(path string) bool {
	ext := filepath.Ext(path)
	if !sourceExts[ext] {
		return false
	}
	return true
}

// isTestFile reports whether path itself looks like a test file.
func isTestFile(path string) bool {
	for _, pat := range []string{"_test.", "_spec.", ".test.", ".spec.", "test/", "tests/", "__tests__/"} {
		if strings.Contains(path, pat) {
			return true
		}
	}
	return false
}

// isWhitelisted reports whether a source file is exempt from the test requirement.
func isWhitelisted(path string) bool {
	base := filepath.Base(path)
	// Normalize to forward slashes for cross-platform substring matching.
	norm := filepath.ToSlash(path)
	for _, w := range testCoverageWhitelist {
		if w.baseExact != "" && base == w.baseExact {
			return true
		}
		if w.substr != "" && strings.Contains(norm, w.substr) {
			return true
		}
	}
	return false
}

// hasMatchingTest infers the conventional test-file path for a changed source
// file and checks the changed set for it (per-language conventions below).
func hasMatchingTest(src string, changed map[string]bool) bool {
	// git reports repo-relative paths with forward slashes on every platform.
	// Normalize the source path to match: filepath.Dir runs Clean, which on
	// Windows converts '/' to the OS separator '\', while the changed keys stay
	// forward-slash. Without ToSlash, the package-level fallback below silently
	// never matches on Windows — breaking the gate (and scoreTask's B3 live
	// fallback) for any multi-directory package.
	src = filepath.ToSlash(src)
	base := strings.TrimSuffix(src, filepath.Ext(src))
	ext := filepath.Ext(src)
	dir := filepath.ToSlash(filepath.Dir(src))
	name := filepath.ToSlash(filepath.Base(src))

	switch ext {
	case ".go":
		// Convention: foo.go ↔ foo_test.go (preferred, most precise).
		if changed[base+"_test.go"] {
			return true
		}
		// Package-level fallback: Go tests are conventionally package-scoped,
		// so a test file in the SAME directory as the source covers it even
		// without the matched name (e.g. executor.go's tests live in
		// testcoverage_test.go). Without this fallback the gate falsely fails
		// well-tested packages whose tests are named after a sibling concept.
		// The fallback stays strict: a _test.go must exist in the changed set
		// under the source's directory — a directory with NO test change still
		// fails, so "genuinely untested" code is still caught.
		pkgDir := strings.TrimSuffix(dir, "/") + "/"
		if pkgDir == "./" {
			pkgDir = ""
		}
		for f := range changed {
			if !strings.HasSuffix(f, "_test.go") {
				continue
			}
			if pkgDir == "" {
				// Root-level source: any root-level _test.go qualifies.
				if !strings.Contains(strings.TrimSuffix(f, "_test.go"), "/") {
					return true
				}
				continue
			}
			if strings.HasPrefix(f, pkgDir) {
				return true
			}
		}
		return false
	case ".rs":
		if changed[base+"_test.rs"] {
			return true
		}
		// Rust inline #[cfg(test)] modules are also acceptable — but we can only
		// see file names here, so accept a same-file-named _test.rs only.
		return false
	case ".ts", ".tsx":
		for _, p := range []string{base + ".test.ts", base + ".test.tsx", base + ".spec.ts", base + ".spec.tsx"} {
			if changed[p] {
				return true
			}
		}
		return false
	case ".js", ".jsx":
		for _, p := range []string{base + ".test.js", base + ".test.jsx", base + ".spec.js", base + ".spec.jsx"} {
			if changed[p] {
				return true
			}
		}
		return false
	case ".py":
		for _, p := range []string{dir + "/test_" + name, dir + "/" + base + "_test.py", "tests/test_" + name} {
			if changed[p] {
				return true
			}
		}
		return false
	default:
		// java/rb/zig/nim: accept any *_test.* or test_*.* in the changed set
		// for the same base name — conservative match.
		wantBase := base
		for f := range changed {
			if (strings.HasPrefix(f, wantBase) || strings.HasSuffix(filepath.Dir(f)+"/"+name, "_test")) && isTestFile(f) {
				return true
			}
		}
		return false
	}
}

// testCoverageDetail returns a concise checklog detail string for the gate
// verdict (kept short — checklog Detail is one line, not the user-facing
// failure message that formatMissing produces).
func testCoverageDetail(ok bool, missing []string) string {
	if ok {
		return "all changed source files have corresponding tests"
	}
	if len(missing) > 3 {
		return fmt.Sprintf("missing tests for %d files: %s ...", len(missing), strings.Join(missing[:3], ", "))
	}
	return "missing tests for: " + strings.Join(missing, ", ")
}

// formatMissing produces the human-readable gate failure message.
func formatMissing(missing []string) string {
	if len(missing) > 5 {
		return fmt.Sprintf("%d source files changed without a corresponding test: %s ... (and %d more). "+
			"Add tests for changed source (CLAUDE.md rule 4: 测试伴随变更). "+
			"To bypass for this task: FORGE_TEST_COVERAGE=disable",
			len(missing), strings.Join(missing[:5], ", "), len(missing)-5)
	}
	return fmt.Sprintf("source files changed without a corresponding test: %s. "+
		"Add tests for changed source (CLAUDE.md rule 4: 测试伴随变更). "+
		"To bypass for this task: FORGE_TEST_COVERAGE=disable",
		strings.Join(missing, ", "))
}
