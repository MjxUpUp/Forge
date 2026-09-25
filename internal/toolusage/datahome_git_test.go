package toolusage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestRecord_DataDirGitProject pins the data-home migration for toollog in a
// REAL git project: Record must write DataDir/toollog.jsonl, NOT .forge/.
// Other toolusage tests use bare t.TempDir() (non-git), where DataDirFor falls
// back to <dir>/.forge — coinciding with the legacy path and masking divergence.
// If someone reverts store.go dataDir() to filepath.Join(root, ".forge"), those
// tests stay green but THIS one catches the regression.
func TestRecord_DataDirGitProject(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	if err := Record(dir, &ToolCall{ID: "x", ToolName: "Read", TaskRef: "T"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	dataDir := forgedata.DataDirFor(dir)
	// Mux: if DataDir did not diverge from <dir>/.forge the test cannot catch
	// the regression — fail loudly instead of silently passing.
	if dataDir == filepath.Join(dir, ".forge") {
		t.Fatalf("DataDir fell back to <dir>/.forge; git init did not make it diverge — test is moot")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "toollog.jsonl")); err != nil {
		t.Errorf("toollog not written to DataDir/toollog.jsonl: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "toollog.jsonl")); err == nil {
		t.Error("toollog must NOT be written to legacy .forge/toollog.jsonl — Record must use DataDir")
	}
	calls, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(calls) != 1 || calls[0].ToolName != "Read" {
		t.Errorf("LoadAll round-trip = %+v, want 1 Read", calls)
	}
}
