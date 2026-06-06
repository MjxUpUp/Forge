package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONEqualsPass(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-4-implement")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "test-results.json"), []byte(`{"failed":0,"passed":10}`), 0644)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:  "test-results.json",
		Field: "failed",
		Value: 0,
	})

	if !result.Passed {
		t.Errorf("failed=0 should pass: %s", result.Detail)
	}
}

func TestJSONEqualsFail(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-4-implement")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "test-results.json"), []byte(`{"failed":5,"passed":0}`), 0644)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:  "test-results.json",
		Field: "failed",
		Value: 0,
	})

	if result.Passed {
		t.Error("failed=5 should not equal 0")
	}
}

func TestJSONEqualsMissingField(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-4-implement")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "data.json"), []byte(`{"other":1}`), 0644)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:  "data.json",
		Field: "missing",
		Value: 0,
	})

	if result.Passed {
		t.Error("missing field should fail")
	}
}

func TestJSONEqualsMissingFile(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-4")
	os.MkdirAll(gateDir, 0755)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:  "missing.json",
		Field: "x",
		Value: 0,
	})

	if result.Passed {
		t.Error("missing file should fail")
	}
}

func TestJSONEqualsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-4")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "bad.json"), []byte(`not json`), 0644)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:  "bad.json",
		Field: "x",
		Value: 0,
	})

	if result.Passed {
		t.Error("invalid JSON should fail")
	}
}

// Regression: numeric 0 (float64 from JSON) vs non-numeric string must NOT match.
func TestJSONEqualsNumericZeroVsString(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"count":0}`), 0644)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: dir}, CheckParams{File: "data.json", Field: "count", Value: "hello"})
	if result.Passed {
		t.Error("numeric 0 should not equal string 'hello' — isNumericType regression")
	}
}

// Regression: float64(0) vs int(0) must match (cross-type numeric comparison).
func TestJSONEqualsFloat64ZeroVsIntZero(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"failed":0}`), 0644)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: dir}, CheckParams{File: "data.json", Field: "failed", Value: 0})
	if !result.Passed {
		t.Errorf("float64(0) should equal int(0): %s", result.Detail)
	}
}

func TestJSONEqualsNestedField(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"a":{"b":{"c":42}}}`), 0644)

	eval := &JSONEqualsEvaluator{}
	result := eval.Evaluate(Context{GateDir: dir}, CheckParams{File: "data.json", Field: "a.b.c", Value: 42})
	if !result.Passed {
		t.Errorf("nested field a.b.c = 42 should pass: %s", result.Detail)
	}
}

func TestJSONGTE(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-5-test")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "coverage.json"), []byte(`{"coverage":85}`), 0644)

	eval := &JSONCompareEvaluator{Op: ">="}

	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:  "coverage.json",
		Field: "coverage",
		Value: 80,
	})
	if !result.Passed {
		t.Errorf("85 >= 80 should pass: %s", result.Detail)
	}

	result = eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:  "coverage.json",
		Field: "coverage",
		Value: 90,
	})
	if result.Passed {
		t.Error("85 >= 90 should fail")
	}
}

func TestJSONLTE(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"errors":2}`), 0644)

	eval := &JSONCompareEvaluator{Op: "<="}
	result := eval.Evaluate(Context{GateDir: dir}, CheckParams{File: "data.json", Field: "errors", Value: 5})
	if !result.Passed {
		t.Errorf("2 <= 5 should pass: %s", result.Detail)
	}

	result = eval.Evaluate(Context{GateDir: dir}, CheckParams{File: "data.json", Field: "errors", Value: 1})
	if result.Passed {
		t.Error("2 <= 1 should fail")
	}
}

func TestJSONArrayMinCountPass(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-0-research")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "competitors.json"), []byte(`[{"name":"A"},{"name":"B"},{"name":"C"}]`), 0644)

	eval := &JSONArrayMinCountEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:     "competitors.json",
		MinCount: 3,
	})

	if !result.Passed {
		t.Errorf("array length 3 >= 3 should pass: %s", result.Detail)
	}
}

func TestJSONArrayMinCountFail(t *testing.T) {
	dir := t.TempDir()
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-0-research")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "competitors.json"), []byte(`[{"name":"A"}]`), 0644)

	eval := &JSONArrayMinCountEvaluator{}
	result := eval.Evaluate(Context{GateDir: gateDir}, CheckParams{
		File:     "competitors.json",
		MinCount: 3,
	})

	if result.Passed {
		t.Error("array length 1 < 3 should fail")
	}
}

// JSON object with "count" field fallback.
func TestJSONArrayMinCountObjectWithCount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "summary.json"), []byte(`{"count":7,"label":"tests"}`), 0644)

	eval := &JSONArrayMinCountEvaluator{}
	result := eval.Evaluate(Context{GateDir: dir}, CheckParams{File: "summary.json", MinCount: 5})
	if !result.Passed {
		t.Errorf("count 7 >= 5 should pass: %s", result.Detail)
	}
}

// JSON object without "count" field should fail.
func TestJSONArrayMinCountObjectNoCount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"foo":"bar"}`), 0644)

	eval := &JSONArrayMinCountEvaluator{}
	result := eval.Evaluate(Context{GateDir: dir}, CheckParams{File: "data.json", MinCount: 1})
	if result.Passed {
		t.Error("object without count field should fail")
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected float64
	}{
		{float64(3.14), 3.14},
		{float32(2.5), 2.5},
		{int(42), 42},
		{int64(100), 100},
		{json.Number("3.14"), 3.14},
		{json.Number("bad"), 0},
		{string("7.5"), 7.5},
		{string("notanumber"), 0},
		{true, 0},
		{nil, 0},
	}

	for _, tt := range tests {
		got := toFloat64(tt.input)
		if got != tt.expected {
			t.Errorf("toFloat64(%v (%T)) = %v, want %v", tt.input, tt.input, got, tt.expected)
		}
	}
}
