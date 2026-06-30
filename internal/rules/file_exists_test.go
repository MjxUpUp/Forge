package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileExistsInGateDir(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "prd.md"), []byte("# PRD\n"), 0644)

	eval := &FileExistsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File: "prd.md",
	})
	if !result.Passed {
		t.Errorf("prd.md in gate dir should pass: %s", result.Detail)
	}
}

func TestFileExistsInProjectRoot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Project\n"), 0644)
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-6-acceptance")
	os.MkdirAll(gateDir, 0755)

	eval := &FileExistsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir, ProjectRoot: dir}, CheckParams{
		File: "README.md",
		In:   "project_root",
	})
	if !result.Passed {
		t.Errorf("README.md in project root should pass: %s", result.Detail)
	}
}

func TestFileExistsMissing(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-6")
	os.MkdirAll(gateDir, 0755)

	eval := &FileExistsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir, ProjectRoot: dir}, CheckParams{
		File: "CHANGELOG.md",
		In:   "project_root",
	})
	if result.Passed {
		t.Error("missing CHANGELOG.md should fail")
	}
}
