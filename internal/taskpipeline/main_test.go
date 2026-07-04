package taskpipeline

import (
	"os"
	"testing"
)

// TestMain redirects the user-level DataDir (where checklog and other runtime
// state now live after the refactor-data-home migration) to an isolated temp
// dir. Tests in this package create real git repos via runGit and record
// checklog entries; without redirection, checklog.Record would resolve a real
// DataDir under ~/.forge/projects/<key>/ and pollute the developer's machine.
//
// task state still lives in the project-level ConfigDir (<dir>/.forge/), which
// FORGE_DATA_HOME does not touch, so this only isolates runtime state — it does
// not affect task-state path assertions. A fresh MkdirTemp per process avoids
// cross-run and cross-package leakage.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "forge-taskpipeline-datahome-")
	if err != nil {
		panic(err)
	}
	os.Setenv("FORGE_DATA_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir) // defer won't run before os.Exit — clean up explicitly
	os.Exit(code)
}
