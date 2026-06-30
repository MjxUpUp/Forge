package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnknownRuleTypeFails(t *testing.T) {
	ctx := Context{
		GateDir:     "/tmp/test",
		ProjectRoot: "/tmp/test",
		GateID:      "gate-test",
	}

	checks := []Check{
		{Name: "unknown check", Type: "nonexistent_rule_type", Params: CheckParams{}},
	}

	results := EvaluateChecks(ctx, checks)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Passed {
		t.Error("unknown rule type should FAIL (fail-safe), not pass")
	}
	if results[0].Detail == "" {
		t.Error("unknown rule type should have detail explaining the failure")
	}
}

func TestMultipleChecks(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-test")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "compile.log"), []byte("BUILD OK\n"), 0644)
	os.WriteFile(filepath.Join(gateDir, "test-results.json"), []byte(`{"failed":0}`), 0644)

	ctx := Context{
		GateDir:     gateDir,
		ProjectRoot: dir,
		GateID:      "gate-test",
	}

	checks := []Check{
		{Name: "compile", Type: "file_not_contains", Params: CheckParams{File: "compile.log", Keyword: "ERROR"}},
		{Name: "tests", Type: "json_equals", Params: CheckParams{File: "test-results.json", Field: "failed", Value: 0}},
	}

	results := EvaluateChecks(ctx, checks)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("check '%s' should pass: %s", r.Name, r.Detail)
		}
	}
}

func TestEmptyChecks(t *testing.T) {
	ctx := Context{GateDir: "/tmp"}
	results := EvaluateChecks(ctx, nil)
	if len(results) != 0 {
		t.Errorf("empty checks should return empty: got %d", len(results))
	}
}
