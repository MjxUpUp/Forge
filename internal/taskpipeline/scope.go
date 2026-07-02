package taskpipeline

// scope.go 实现 PlanScope 的 advisory 偏差检测：把"开工前声明要改哪些文件"（规划前置）
// 变成可度量契约——实改文件（TaskChangedFiles）与声明的差集 = scope-drift。
//
// 设计依据（业界/学术对照，2026-07 调研）：
//   - Terraform drift detection：PlanScope = desired state，TaskChangedFiles = actual state，
//     差集 = drift。Terraform drift 也是"报告供 review，不阻断 apply"——本机制同构（advisory）。
//   - Copilot Workspace / VS Plan agent：plan-then-execute 是 2025 主流范式，plan 存机器可读
//     形态供执行。PlanScope []string 是其轻量版。
//   - 变更影响分析（SCIA/TIA）召回率仅 ~44%（PASTE 论文）：影响集预测本质是概率问题，
//     故 scope 当 prediction 而非 contract，drift 是常态信号而非异常——硬拦会误杀一半合法改动。
//   - Agentless 两阶段定位（FSE 2025）：定位是分层、可修正的——故 scope 支持中途追加
//     （task scope add），不一次锁死。
//
// 全程 advisory：MatchesScope/ScopeDrift 只回答"是否覆盖/偏差何在"，不决策是否放行。
// 调用方（hook.go）记 checklog CheckScopeDrift 供 review/看板，绝不阻断工具调用。

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// MatchesScope reports whether file (repo-relative, any separators) is covered by
// the declared PlanScope. Empty scope → true everywhere (no declaration = nothing
// is drift; callers short-circuit, but this keeps the function total).
//
// Coverage is intentionally generous (scope is a prediction/superset, not a contract):
//   - exact path match: scope entry == file
//   - directory-prefix recursion: "internal/cli" or "internal/cli/" covers everything under it
//   - shell glob via path.Match: "internal/cli/*.go", "*.go" ('*' does NOT cross '/')
//   - test files auto-covered when their source counterpart is in scope
//     (declared a.go covers a_test.go — planning source is enough; mirrors testcoverage conventions)
//   - generated/type/entry-point files (testcoverage whitelist) always covered —
//     drift is about real source, not derived noise. Reuses the single whitelist truth source.
func MatchesScope(file string, scope []string) bool {
	file = filepath.ToSlash(file)
	if len(scope) == 0 {
		return true
	}
	// Derived/type noise: never counts as drift (reuse testcoverage whitelist — single truth source).
	if isWhitelisted(file) {
		return true
	}
	for _, g := range scope {
		if scopeMatchOne(g, file) {
			return true
		}
	}
	// Test file: covered if its conventional source counterpart is in scope.
	// Not gated on isTestFile — that helper (from testcoverage) misses Python's
	// "test_" prefix convention, which would leave Python tests never auto-covered.
	// sourcePathsForTest returns nil for non-test files, so this is a safe no-op
	// for regular source.
	if testSourceInScope(file, scope) {
		return true
	}
	return false
}

// scopeMatchOne tests a single scope glob against one normalized file path.
// path.Match (not filepath.Match) is deliberate: it treats '/' as the sole
// separator on every platform, so '*' behaves identically on Windows and POSIX
// (filepath.Match would let '*' cross '/' on Windows, splitting the same glob
// differently per OS — a latent cross-platform drift-detection bug).
func scopeMatchOne(glob, file string) bool {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return false
	}
	if glob == file {
		return true
	}
	// Directory-prefix recursion: "internal/cli" / "internal/cli/" → everything under.
	prefix := strings.TrimSuffix(glob, "/")
	if prefix != "" && strings.HasPrefix(file, prefix+"/") {
		return true
	}
	if ok, err := path.Match(glob, file); err == nil && ok {
		return true
	}
	return false
}

// testSourceInScope reports whether a test file's conventional source counterpart
// is covered by scope. Inverse of testcoverage.hasMatchingTest's conventions
// (Go/Rust/TS/JS/Python). Lets declaring the source alone cover its tests, so a
// well-planned source edit doesn't false-positive its own test file as drift.
func testSourceInScope(testFile string, scope []string) bool {
	for _, src := range sourcePathsForTest(testFile) {
		for _, g := range scope {
			if scopeMatchOne(g, src) {
				return true
			}
		}
	}
	return false
}

// sourcePathsForTest returns candidate source paths a test file would exercise.
// Covers the common conventions; misses are conservative (test then counts as drift),
// never silent false-positives in the other direction.
func sourcePathsForTest(testFile string) []string {
	f := filepath.ToSlash(testFile)
	dir := path.Dir(f)
	name := path.Base(f)
	ext := path.Ext(name) // includes leading '.'
	if ext == "" {
		return nil
	}
	var cands []string
	join := func(n string) string {
		if dir == "." {
			return n
		}
		return dir + "/" + n
	}
	// foo_test.go → foo.go  (Go/Rust/Zig/Nim)
	if strings.HasSuffix(name, "_test"+ext) {
		cands = append(cands, join(strings.TrimSuffix(name, "_test"+ext)+ext))
	}
	// foo.test.ts → foo.ts ; foo.spec.jsx → foo.jsx  (TS/JS)
	for _, marker := range []string{".test", ".spec"} {
		if strings.Contains(name, marker+ext) {
			cands = append(cands, join(strings.Replace(name, marker+ext, ext, 1)))
		}
	}
	// test_foo.py → foo.py  (Python only — "test_" prefix is Python's convention;
	// gating by ext avoids mis-firing on Go files like test_config.go which are
	// NOT tests but happen to start with "test_".)
	if ext == ".py" && strings.HasPrefix(name, "test_") {
		cands = append(cands, join(strings.TrimPrefix(name, "test_")))
	}
	return cands
}

// ScopeDrift returns the source files in `changed` NOT covered by `scope` — the
// advisory drift signal. Empty scope → nil (no declaration → no drift). Only real
// source files count (isSourceFile): touching README/.yaml/config during a source
// task is exploration, not drift. Test files whose source is in scope don't count.
// Order is preserved; duplicates are not deduped (caller may pass a set already).
func ScopeDrift(changed, scope []string) []string {
	if len(scope) == 0 {
		return nil
	}
	var drift []string
	for _, f := range changed {
		if !isSourceFile(f) {
			continue
		}
		if !MatchesScope(f, scope) {
			drift = append(drift, filepath.ToSlash(f))
		}
	}
	return drift
}

// ChangedFiles returns the files changed during the task — committed since the
// task's HeadCommit (captured at start) plus the working tree. Exported wrapper
// over testcoverage.taskChangedFiles so the CLI (task scope show) reuses the SAME
// git-diff source-of-truth as test-coverage and scoring, never a second derivation.
func ChangedFiles(root string, state *TaskState) []string {
	return taskChangedFiles(root, state)
}

// scopeDriftDetail produces a concise checklog detail for the scope-drift verdict
// (one line — checklog Detail is a summary, not the user-facing stderr message).
func scopeDriftDetail(drift []string) string {
	if len(drift) == 0 {
		return "all changed source files within declared PlanScope"
	}
	if len(drift) > 5 {
		return fmt.Sprintf("scope-drift: %d files out-of-scope: %s ...", len(drift), strings.Join(drift[:5], ", "))
	}
	return "scope-drift: out-of-scope: " + strings.Join(drift, ", ")
}
