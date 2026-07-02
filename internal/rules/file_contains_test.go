package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileNotContainsPass(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-4-implement")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "compile.log"), []byte("BUILD SUCCESS\n"), 0644)

	eval := &FileContainsEvaluator{Negated: true}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:    "compile.log",
		Keyword: "ERROR",
	})

	if !result.Passed {
		t.Errorf("should pass: %s", result.Detail)
	}
}

func TestFileNotContainsFail(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-4-implement")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "compile.log"), []byte("ERROR: build failed\n"), 0644)

	eval := &FileContainsEvaluator{Negated: true}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:    "compile.log",
		Keyword: "ERROR",
	})

	if result.Passed {
		t.Error("should fail when ERROR present")
	}
}

func TestFileContainsPass(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "prd.md"), []byte("# PRD\n\n## Out of Scope\n"), 0644)

	eval := &FileContainsEvaluator{Negated: false}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:    "prd.md",
		Keyword: "Out of Scope",
	})

	if !result.Passed {
		t.Errorf("should find 'Out of Scope': %s", result.Detail)
	}
}

func TestFileContainsFail(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "prd.md"), []byte("# PRD\n\nNo scope info\n"), 0644)

	eval := &FileContainsEvaluator{Negated: false}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:    "prd.md",
		Keyword: "Out of Scope",
	})

	if result.Passed {
		t.Error("should fail when keyword missing")
	}
}

func TestFileContainsMissingFile(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)

	eval := &FileContainsEvaluator{Negated: false}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:    "nonexistent.md",
		Keyword: "anything",
	})

	if result.Passed {
		t.Error("file_contains should fail when file missing")
	}
}

func TestFileNotContainsMissingFile(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)

	// D1: default is fail-closed. A missing file cannot be verified to NOT contain
	// the keyword, and the prior pass-on-missing default let an agent delete the
	// target file to bypass a "must not contain" gate. The check now FAILS.
	eval := &FileContainsEvaluator{Negated: true}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:    "nonexistent.log",
		Keyword: "ERROR",
	})

	if result.Passed {
		t.Errorf("file_not_contains must FAIL on missing file by default (D1 fail-closed): %s", result.Detail)
	}
	if result.Message == "" {
		t.Error("fail-closed result must carry a non-empty Message")
	}
}

// TestFileNotContainsMissingFile_PassOnMissing guards the explicit opt-in for the
// "this artifact should not exist" semantic (e.g. a debug.log the build must not
// produce). PassOnMissing:true restores missing→PASS, but only when declared.
func TestFileNotContainsMissingFile_PassOnMissing(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)

	eval := &FileContainsEvaluator{Negated: true}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:          "debug.log",
		Keyword:       "ERROR",
		PassOnMissing: true,
	})

	if !result.Passed {
		t.Errorf("file_not_contains with pass_on_missing should PASS on absent file: %s", result.Detail)
	}
}

func TestFileContainsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "report.md"), []byte("Technical feasibility conclusion\n"), 0644)

	eval := &FileContainsEvaluator{Negated: false}

	// Default: case insensitive
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:    "report.md",
		Keyword: "TECHNICAL FEASIBILITY",
	})
	if !result.Passed {
		t.Errorf("case insensitive should pass: %s", result.Detail)
	}

	// Case sensitive
	result = eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:          "report.md",
		Keyword:       "TECHNICAL FEASIBILITY",
		CaseSensitive: true,
	})
	if result.Passed {
		t.Error("case sensitive should fail on mismatched case")
	}
}

// Regression: file_contains respects params.In="project_root"
func TestFileContainsInProjectRoot(t *testing.T) {
	dir := t.TempDir()
	// Put file at project root, NOT in gate dir
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# My Project\n\nOut of Scope: nothing\n"), 0644)

	gateDir := filepath.Join(dir, ".forge", "gates", "gate-6-acceptance")
	os.MkdirAll(gateDir, 0755)

	eval := &FileContainsEvaluator{Negated: false}

	// Default (gate_dir) — should fail because README.md is not in gate dir
	result := eval.Evaluate(Context{GateDir: gateDir, ProjectRoot: dir}, CheckParams{
		File:    "README.md",
		Keyword: "Out of Scope",
	})
	if result.Passed {
		t.Error("should fail when file not in gate_dir")
	}

	// With in="project_root" — should pass
	result = eval.Evaluate(Context{GateDir: gateDir, ProjectRoot: dir}, CheckParams{
		File:    "README.md",
		Keyword: "Out of Scope",
		In:      "project_root",
	})
	if !result.Passed {
		t.Errorf("should pass with in=project_root: %s", result.Detail)
	}
}
