package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCustomScriptPass(t *testing.T) {
	tmpDir := t.TempDir()
	hooksDir := filepath.Join(tmpDir, ".forge", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	scriptName := "check_pass.sh"
	scriptPath := filepath.Join(hooksDir, scriptName)
	content := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &CustomScriptEvaluator{}
	ctx := Context{ProjectRoot: tmpDir}
	params := CheckParams{Script: scriptName}

	result := e.Evaluate(ctx, params)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s — %s", result.Detail, result.Message)
	}
}

func TestCustomScriptFail(t *testing.T) {
	tmpDir := t.TempDir()
	hooksDir := filepath.Join(tmpDir, ".forge", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	scriptName := "check_fail.sh"
	scriptPath := filepath.Join(hooksDir, scriptName)
	content := "#!/bin/sh\necho 'something went wrong'\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &CustomScriptEvaluator{}
	ctx := Context{ProjectRoot: tmpDir}
	params := CheckParams{Script: scriptName}

	result := e.Evaluate(ctx, params)
	if result.Passed {
		t.Fatal("expected fail for exit 1, got pass")
	}
}

func TestCustomScriptNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	e := &CustomScriptEvaluator{}
	ctx := Context{ProjectRoot: tmpDir}
	params := CheckParams{Script: "nonexistent.sh"}

	result := e.Evaluate(ctx, params)
	if result.Passed {
		t.Fatal("expected fail for missing script, got pass")
	}
}

func TestTruncateOutput(t *testing.T) {
	// Short output is unchanged.
	short := "hello world"
	if got := truncateOutput([]byte(short), 200); got != short {
		t.Fatalf("short output: expected %q, got %q", short, got)
	}

	// Long output is truncated.
	long := make([]rune, 300)
	for i := range long {
		long[i] = 'a'
	}
	got := truncateOutput([]byte(string(long)), 200)
	if len(got) != len("...")+200 {
		t.Fatalf("truncated length: expected %d, got %d", 200+3, len(got))
	}
	if got[len(got)-3:] != "..." {
		t.Fatalf("truncated suffix: expected '...', got %q", got[len(got)-3:])
	}

	// Chinese characters are safely truncated (no mid-rune cut).
	chinese := ""
	for i := 0; i < 300; i++ {
		chinese += "你"
	}
	gotCN := truncateOutput([]byte(chinese), 200)
	// Should be 200 runes + 3 bytes for "..."
	if len([]rune(gotCN[:len(gotCN)-3])) != 200 {
		t.Fatalf("chinese truncation: expected 200 runes before '...', got %d", len([]rune(gotCN[:len(gotCN)-3])))
	}
}

func TestTruncateOutputExactBoundary(t *testing.T) {
	// Output exactly at maxLen should not be truncated.
	exact := make([]rune, 200)
	for i := range exact {
		exact[i] = 'x'
	}
	got := truncateOutput([]byte(string(exact)), 200)
	if got != string(exact) {
		t.Fatalf("exact boundary: expected no truncation, got %q", got)
	}

	// One rune over boundary triggers truncation.
	over := make([]rune, 201)
	for i := range over {
		over[i] = 'x'
	}
	got = truncateOutput([]byte(string(over)), 200)
	if got == string(over) {
		t.Fatal("one-over boundary: expected truncation, got full string")
	}
}
