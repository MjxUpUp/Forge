package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// TestIsGitRepo_NonGitDirectory guards the degraded-mode signal: a plain temp
// dir is not a git repo, so IsGitRepo must return false. `task start` relies on
// this to print the non-git warning that a stranded code-knowledge-base-style
// session lacked — without it, an agent starting a task in a bare directory
// gets no signal it's running in degraded mode.
func TestIsGitRepo_NonGitDirectory(t *testing.T) {
	dir := t.TempDir()
	if IsGitRepo(dir) {
		t.Errorf("IsGitRepo(%q) = true, want false for non-git dir", dir)
	}
}

// TestIsGitRepo_GitDirectory confirms the helper returns true inside an actual
// git working tree (even before any commit — `git init` creates .git, which
// `rev-parse --git-dir` resolves). This is what suppresses the warning for
// normal projects.
func TestIsGitRepo_GitDirectory(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	if !IsGitRepo(dir) {
		t.Errorf("IsGitRepo(%q) = false, want true for git dir", dir)
	}
}

// TestSaveTaskState_DataDirGitProject pins the data-home migration for task
// state in a REAL git project: SaveTaskState must write to the user-level
// DataDir/tasks/, NOT project-level .forge/tasks/. Every other taskpipeline
// test uses a bare t.TempDir() (non-git), where DataDirFor falls back to
// <dir>/.forge — coinciding with the legacy path and masking divergence. If
// someone reverts state.go dataHome() to filepath.Join(root, ".forge", "tasks"),
// those tests stay green but THIS one catches the regression.
func TestSaveTaskState_DataDirGitProject(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init")

	state := &TaskState{TaskRef: "feature/x", Branch: "feature/x", Source: "explicit"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	dataDir := forgedata.DataDirFor(dir)
	// Mux: if DataDir did not diverge from <dir>/.forge the test cannot catch
	// the regression — fail loudly instead of silently passing.
	if dataDir == filepath.Join(dir, ".forge") {
		t.Fatalf("DataDir fell back to <dir>/.forge; git init did not make it diverge — test is moot")
	}

	filename := taskcontext.SanitizeRef("feature/x") + ".json"
	wantPath := filepath.Join(dataDir, "tasks", filename)
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("state not written to DataDir/tasks/%s: %v", filename, err)
	}
	legacyPath := filepath.Join(dir, ".forge", "tasks", filename)
	if _, err := os.Stat(legacyPath); err == nil {
		t.Errorf("state must NOT be written to legacy .forge/tasks/%s — SaveTaskState must use DataDir", filename)
	}

	// Round-trip: LoadTaskState reads back from DataDir.
	loaded, err := LoadTaskState(dir, "feature/x")
	if err != nil {
		t.Fatalf("LoadTaskState from DataDir: %v", err)
	}
	if loaded.TaskRef != "feature/x" {
		t.Errorf("loaded TaskRef = %q, want feature/x", loaded.TaskRef)
	}
}

// TestEnsureSession_DataDirGitProject pins the data-home migration for sessions
// in a REAL git project: EnsureSession must write DataDir/sessions/<id>.json,
// NOT .forge/sessions/. Other session tests use bare t.TempDir() (non-git),
// where DataDirFor falls back to <dir>/.forge — masking divergence.
func TestEnsureSession_DataDirGitProject(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init")

	if _, err := EnsureSession(dir, "sess1"); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	dataDir := forgedata.DataDirFor(dir)
	if dataDir == filepath.Join(dir, ".forge") {
		t.Fatalf("DataDir fell back to <dir>/.forge; git init did not make it diverge — test is moot")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "sessions", "sess1.json")); err != nil {
		t.Errorf("scoped session not written to DataDir/sessions/sess1.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "sessions", "sess1.json")); err == nil {
		t.Error("session must NOT be written to legacy .forge/sessions/ — EnsureSession must use DataDir")
	}
}

// TestEnsureSession_EmptyID_DataDirGitProject pins the data-home migration for
// the legacy GLOBAL session path (sessionID==""). EnsureSession must write
// DataDir/session.json + DataDir/sessions.jsonl, NOT .forge/. The scoped test
// above covers sessionID!=""; this covers the empty-id branch (sessionFilePath /
// sessionsLogPath) so a revert of either's dataHome() call is caught too.
func TestEnsureSession_EmptyID_DataDirGitProject(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init")

	if _, err := EnsureSession(dir, ""); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	dataDir := forgedata.DataDirFor(dir)
	if dataDir == filepath.Join(dir, ".forge") {
		t.Fatalf("DataDir fell back to <dir>/.forge; git init did not make it diverge — test is moot")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "session.json")); err != nil {
		t.Errorf("global session not written to DataDir/session.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "sessions.jsonl")); err != nil {
		t.Errorf("sessions log not written to DataDir/sessions.jsonl: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "session.json")); err == nil {
		t.Error("session must NOT be written to legacy .forge/session.json — EnsureSession must use DataDir")
	}
}
