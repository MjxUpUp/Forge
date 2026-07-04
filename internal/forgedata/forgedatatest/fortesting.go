// Package forgedatatest provides helpers for constructing forgedata.Project
// values in unit tests without standing up a real .git worktree.
//
// Import only from _test.go files. Production code must resolve a Project via
// forgedata.ProjectFor (which requires a real .git common dir to derive the
// key); tests that exercise runtime-state stores (checklog/hazard/experience/
// act/...) usually don't need that and instead point DataDir at a temp dir.
package forgedatatest

import (
	"path/filepath"

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
