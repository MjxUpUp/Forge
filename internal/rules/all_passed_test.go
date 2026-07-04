package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllGatesPassed(t *testing.T) {
	dir := t.TempDir()
	gatesDir := filepath.Join(dir, ".forge", "gates")

	for _, gateID := range []string{"gate-1-prd", "gate-2-design", "gate-4-implement"} {
		gDir := filepath.Join(gatesDir, gateID)
		os.MkdirAll(gDir, 0755)
		os.WriteFile(filepath.Join(gDir, "status.json"), []byte(`{"gate":"`+gateID+`","passed":true}`), 0644)
	}

	gateDir := filepath.Join(gatesDir, "gate-8-release")
	os.MkdirAll(gateDir, 0755)

	eval := &AllGatesPassedEvaluator{}
	result := eval.Evaluate(Context{
		GateDir:        gateDir,
		GatesDir:       gatesDir,
		ProjectRoot:    dir,
		GateID:         "gate-8-release",
		EnabledGateIDs: []string{"gate-1-prd", "gate-2-design", "gate-4-implement", "gate-8-release"},
	}, CheckParams{})

	if !result.Passed {
		t.Errorf("all passed should pass: %s", result.Detail)
	}
}

func TestAllGatesPassedOneFailed(t *testing.T) {
	dir := t.TempDir()
	gatesDir := filepath.Join(dir, ".forge", "gates")

	os.MkdirAll(filepath.Join(gatesDir, "gate-1-prd"), 0755)
	os.WriteFile(filepath.Join(gatesDir, "gate-1-prd", "status.json"), []byte(`{"passed":true}`), 0644)

	os.MkdirAll(filepath.Join(gatesDir, "gate-4-implement"), 0755)
	os.WriteFile(filepath.Join(gatesDir, "gate-4-implement", "status.json"), []byte(`{"passed":false}`), 0644)

	gateDir := filepath.Join(gatesDir, "gate-8-release")
	os.MkdirAll(gateDir, 0755)

	eval := &AllGatesPassedEvaluator{}
	result := eval.Evaluate(Context{
		GateDir:        gateDir,
		GatesDir:       gatesDir,
		ProjectRoot:    dir,
		GateID:         "gate-8-release",
		EnabledGateIDs: []string{"gate-1-prd", "gate-4-implement", "gate-8-release"},
	}, CheckParams{})

	if result.Passed {
		t.Error("should fail when one gate did not pass")
	}
}

func TestAllGatesPassedNeverRun(t *testing.T) {
	dir := t.TempDir()
	gatesDir := filepath.Join(dir, ".forge", "gates")
	os.MkdirAll(filepath.Join(gatesDir, "gate-1-prd"), 0755)

	gateDir := filepath.Join(gatesDir, "gate-8-release")
	os.MkdirAll(gateDir, 0755)

	eval := &AllGatesPassedEvaluator{}
	result := eval.Evaluate(Context{
		GateDir:        gateDir,
		GatesDir:       gatesDir,
		ProjectRoot:    dir,
		GateID:         "gate-8-release",
		EnabledGateIDs: []string{"gate-1-prd", "gate-8-release"},
	}, CheckParams{})

	if result.Passed {
		t.Error("should fail when gate never run")
	}
}

// Regression: corrupted status.json must cause FAIL, not silent skip.
func TestAllGatesPassedCorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	gatesDir := filepath.Join(dir, ".forge", "gates")

	os.MkdirAll(filepath.Join(gatesDir, "gate-1-prd"), 0755)
	os.WriteFile(filepath.Join(gatesDir, "gate-1-prd", "status.json"), []byte(`CORRUPTED{`), 0644)

	gateDir := filepath.Join(gatesDir, "gate-8-release")
	os.MkdirAll(gateDir, 0755)

	eval := &AllGatesPassedEvaluator{}
	result := eval.Evaluate(Context{
		GateDir:        gateDir,
		GatesDir:       gatesDir,
		ProjectRoot:    dir,
		GateID:         "gate-8-release",
		EnabledGateIDs: []string{"gate-1-prd", "gate-8-release"},
	}, CheckParams{})

	if result.Passed {
		t.Error("corrupted status.json should cause FAIL (fail-safe)")
	}
}
