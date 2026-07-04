// Package forgedatatest provides helpers for constructing forgedata.Project
// values in tests.
//
// Two helpers cover two test tiers:
//   - ForDataDir: lightweight — points DataDir/GitRoot at dir directly, no git,
//     no ProjectFor. For store unit tests (act/checklog/hazard Append/Load pure
//     IO round-trips) that don't exercise hash derivation.
//   - RealProject: heavyweight — git init + .forge placeholder + FORGE_DATA_HOME
//     isolation + real ProjectFor. For integration tests (cli subprocesses /
//     dashboard HTTP / mcpserver IPC) where the code path itself calls
//     ProjectFor; the test process and the forge subprocess must resolve to the
//     same DataDir, which only happens through a real ProjectFor.
//
// Import only from _test.go files. Production code must resolve a Project via
// forgedata.ProjectFor (which requires a real .git common dir to derive the
// key); store unit tests usually don't need that and instead point DataDir at a
// temp dir via ForDataDir.
package forgedatatest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// ForDataDir builds a Project whose DataDir and GitRoot both point at dir, with
// a stable fake key and ConfigDir = dir/.forge. Runtime-state stores only ever
// touch DataDir, so tests pass t.TempDir() as dir and read/write the produced
// paths directly. GitRoot mirrors DataDir so any git-rooted accessor stays
// inside the temp tree; ConfigDir follows the <cwd>/.forge convention but is
// not exercised by runtime-state stores.
func ForDataDir(dir string) *forgedata.Project {
	return &forgedata.Project{
		Key:       "test",
		GitRoot:   dir,
		DataDir:   dir,
		ConfigDir: filepath.Join(dir, ".forge"),
	}
}

// RealProject builds a real, resolvable *Project: git init + .forge placeholder
// + FORGE_DATA_HOME isolation + ProjectFor. Returns (root, p):
//   - root is passed to runForge subprocesses or to functions still taking a
//     root string (e.g. appendConclusion);
//   - p is passed to stores that have migrated to the *Project signature
//     (e.g. act.Append(p, ...)).
//
// When the code path under test calls forgedata.ProjectFor itself (dashboard.
// Aggregate internals, forge subprocesses), RealProject is mandatory: on a
// gitless t.TempDir() ProjectFor fails and writes/reads land in different
// DataDirs, so the test never sees the data. Pure store round-trips (no
// ProjectFor in the path) keep using ForDataDir.
//
// FORGE_DATA_HOME is set per-test (t.Setenv) so DataDir lands in an isolated
// temp dir, never the real ~/.forge; subprocesses inherit it via os.Environ,
// keeping write/read aligned across process boundaries.
func RealProject(t *testing.T) (root string, p *forgedata.Project) {
	t.Helper()
	root = t.TempDir()
	// git init so ProjectFor's Key() can hash the .git common dir.
	// -C is a global git flag (must precede the subcommand); "git init -C <dir>"
	// is rejected by git init with exit 129 (usage error).
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatalf("git init %s: %v", root, err)
	}
	// .forge placeholder so findForgeConfigDir's walk-up hits it (ProjectFor
	// requires the project to be init'd). runForge init fills it in afterwards;
	// an empty dir does not conflict.
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0o755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	// FORGE_DATA_HOME isolation: set once per test (idempotent). Multiple
	// RealProject calls in the same test (e.g. AggregateGlobal with rootA +
	// rootB) MUST share one DATA_HOME — otherwise the second call overwrites
	// the first and ProjectFor(rootA) inside AggregateGlobal resolves to a
	// different DataDir than where act.Append(pA) wrote, so rootA's data
	// vanishes. Different projects stay isolated by their git-root-derived key
	// (<DATA_HOME>/projects/<key>/), not by separate DATA_HOMEs.
	if os.Getenv("FORGE_DATA_HOME") == "" {
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
	}
	var err error
	p, err = forgedata.ProjectFor(root)
	if err != nil {
		t.Fatalf("ProjectFor %s: %v", root, err)
	}
	return root, p
}
