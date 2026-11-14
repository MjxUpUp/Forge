package taskpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

func GoalHashForTest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeGoal(t *testing.T, root, body string) string {
	t.Helper()
	rel := filepath.Join("spec", "changes", "demo", "goal.md")
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

func TestCheckGoalContract_NoContractIsAdvisory(t *testing.T) {
	root := t.TempDir()
	state := &TaskState{TaskRef: "feat/demo"}
	if block := checkGoalContract(root, state); block != nil {
		t.Fatalf("无契约应只 advisory，actual block: %+v", block)
	}
}

func TestCheckGoalContract_UnchangedPasses(t *testing.T) {
	root := t.TempDir()
	rel := writeGoal(t, root, "## Objective\n\nx\n")
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	state := &TaskState{
		TaskRef:      "feat/demo",
		GoalContract: &tasktypes.GoalContract{Path: rel, SHA256: GoalHashForTest(raw)},
	}
	if block := checkGoalContract(root, state); block != nil {
		t.Fatalf("契约未变应通过，actual: %+v", block)
	}
}

func TestCheckGoalContract_DriftBlocks(t *testing.T) {
	root := t.TempDir()
	rel := writeGoal(t, root, "## Objective\n\nx\n")
	state := &TaskState{
		TaskRef:      "feat/demo",
		GoalContract: &tasktypes.GoalContract{Path: rel, SHA256: GoalHashForTest([]byte("old"))},
	}
	block := checkGoalContract(root, state)
	if block == nil || block.Passed {
		t.Fatalf("内容变化应 BLOCKED，actual: %+v", block)
	}
	if block.GateID != GateImplement {
		t.Fatalf("gate = %q, want %q", block.GateID, GateImplement)
	}
}

func TestCheckGoalContract_DeletedFileBlocks(t *testing.T) {
	root := t.TempDir()
	state := &TaskState{
		TaskRef:      "feat/demo",
		GoalContract: &tasktypes.GoalContract{Path: "spec/changes/demo/goal.md", SHA256: "abc"},
	}
	block := checkGoalContract(root, state)
	if block == nil || block.Passed {
		t.Fatalf("契约文件被删应 BLOCKED，actual: %+v", block)
	}
}
