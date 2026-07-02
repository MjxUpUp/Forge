package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// fullYAML is a complete v2 pipeline with all fields populated.
const fullYAML = `version: "2.0"
project: "test-project"
mode: medium
pipeline:
  gates:
    - id: gate-1-prd
      name: "需求定义"
      enabled: true
      prompt: "编写PRD"
      depends_on: []
      hooks: []
      artifacts:
        inputs: ["user_idea"]
        outputs: ["prd.md"]
      checks:
        - name: "PRD存在"
          type: file_exists
          params:
            file: prd.md
      on_failure: abort
      auto_publish_feishu: false
    - id: gate-3-plan
      name: "实现计划"
      enabled: true
      prompt: "制定实���计划"
      depends_on: [gate-1-prd]
      artifacts:
        inputs: ["gate:gate-1-prd/prd.md"]
        outputs: ["plan.md"]
      checks:
        - name: "计划存在"
          type: file_exists
          params:
            file: plan.md
      on_failure: abort
      requires_human_approval: false
`

func loadTestPipeline(t *testing.T) *Pipeline {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	return p
}

func TestExecuteGatePass(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	state := &State{PipelineVersion: "2.0", Mode: "medium"}
	gate, _ := p.GetGate("gate-1-prd")

	// Create the artifact that the check expects
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "prd.md"), []byte("# PRD\n\n## Out of Scope\n- Nothing"), 0644)

	result, err := ExecuteGate(dir, gate, state, p, false)
	if err != nil {
		t.Fatalf("ExecuteGate error: %v", err)
	}
	if !result.Status.Passed {
		t.Errorf("gate should pass: %+v", result.Status.Errors)
	}
	if result.Status.Attempt != 1 {
		t.Errorf("attempt = %d, want 1", result.Status.Attempt)
	}
	if result.Status.Gate != "gate-1-prd" {
		t.Errorf("gate = %s, want gate-1-prd", result.Status.Gate)
	}
}

func TestExecuteGateFailNoArtifact(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	state := &State{PipelineVersion: "2.0", Mode: "medium"}
	gate, _ := p.GetGate("gate-1-prd")

	result, err := ExecuteGate(dir, gate, state, p, false)
	if err != nil {
		t.Fatalf("ExecuteGate error: %v", err)
	}
	if result.Status.Passed {
		t.Error("gate should fail without artifact")
	}
	if len(result.Status.Checks) != 1 {
		t.Errorf("checks count = %d, want 1", len(result.Status.Checks))
	}
	if result.Status.Checks[0].Passed {
		t.Error("check should fail")
	}
}

func TestExecuteGatePrerequisites(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	state := &State{PipelineVersion: "2.0", Mode: "medium"}
	gate3, _ := p.GetGate("gate-3-plan")

	// gate-3 depends on gate-1 which was never run — should fail
	_, err = ExecuteGate(dir, gate3, state, p, false)
	if err == nil {
		t.Fatal("should fail when prerequisite not met")
	}
}

func TestExecuteGateForceSkipsPrerequisites(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	state := &State{PipelineVersion: "2.0", Mode: "medium"}
	gate3, _ := p.GetGate("gate-3-plan")

	// Create artifact for gate-3
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-3-plan")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "plan.md"), []byte("# Plan"), 0644)

	// --force should skip prerequisite check
	result, err := ExecuteGate(dir, gate3, state, p, true)
	if err != nil {
		t.Fatalf("ExecuteGate with --force: %v", err)
	}
	if !result.Status.Passed {
		t.Errorf("--force should allow skipping prereqs: %+v", result.Status.Errors)
	}
}

func TestExecuteGateAttemptCount(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	state := &State{PipelineVersion: "2.0", Mode: "medium"}
	gate, _ := p.GetGate("gate-1-prd")

	// First run — no artifact, should fail
	result1, _ := ExecuteGate(dir, gate, state, p, false)
	if result1.Status.Passed {
		t.Error("first run should fail without artifact")
	}
	if result1.Status.Attempt != 1 {
		t.Errorf("attempt 1 = %d, want 1", result1.Status.Attempt)
	}

	// Create artifact
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "prd.md"), []byte("# PRD"), 0644)

	// Second run — should pass, attempt = 2
	result2, _ := ExecuteGate(dir, gate, state, p, false)
	if !result2.Status.Passed {
		t.Errorf("second run should pass: %+v", result2.Status.Errors)
	}
	if result2.Status.Attempt != 2 {
		t.Errorf("attempt 2 = %d, want 2", result2.Status.Attempt)
	}

	// History should have 2 entries
	if len(state.History) != 2 {
		t.Errorf("history length = %d, want 2", len(state.History))
	}
}

func TestExecuteGateStatusFileWritten(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, _ := Load(dir)
	state := &State{PipelineVersion: "2.0", Mode: "medium"}
	gate, _ := p.GetGate("gate-1-prd")

	// Create artifact
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "prd.md"), []byte("# PRD"), 0644)

	ExecuteGate(dir, gate, state, p, false)

	// Verify status.json was written
	status, err := LoadStatus(dir, "gate-1-prd")
	if err != nil {
		t.Fatalf("LoadStatus: %v", err)
	}
	if !status.Passed {
		t.Error("status.json should show passed")
	}
	if status.Attempt != 1 {
		t.Errorf("status.json attempt = %d, want 1", status.Attempt)
	}

	// Verify state.json was written
	loadedState, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(loadedState.History) != 1 {
		t.Errorf("state history = %d entries, want 1", len(loadedState.History))
	}
}

func TestLoadFullV2YAML(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load full v2 YAML: %v", err)
	}

	if p.Version != "2.0" {
		t.Errorf("version = %s, want 2.0", p.Version)
	}
	if p.Project != "test-project" {
		t.Errorf("project = %s", p.Project)
	}
	if len(p.PipelineDef.Gates) != 2 {
		t.Fatalf("gates = %d, want 2", len(p.PipelineDef.Gates))
	}

	g1 := p.PipelineDef.Gates[0]
	if g1.Prompt != "编写PRD" {
		t.Errorf("prompt = %s", g1.Prompt)
	}
	if len(g1.Artifacts.Inputs) != 1 || g1.Artifacts.Inputs[0] != "user_idea" {
		t.Errorf("inputs = %v", g1.Artifacts.Inputs)
	}
	if len(g1.Artifacts.Outputs) != 1 || g1.Artifacts.Outputs[0] != "prd.md" {
		t.Errorf("outputs = %v", g1.Artifacts.Outputs)
	}
	if len(g1.Checks) != 1 {
		t.Errorf("checks = %d", len(g1.Checks))
	}
	if g1.Checks[0].Type != "file_exists" {
		t.Errorf("check type = %s", g1.Checks[0].Type)
	}
	if g1.OnFailure != "abort" {
		t.Errorf("on_failure = %s", g1.OnFailure)
	}

	g3 := p.PipelineDef.Gates[1]
	if len(g3.DependsOn) != 1 || g3.DependsOn[0] != "gate-1-prd" {
		t.Errorf("depends_on = %v", g3.DependsOn)
	}
}

func TestValidateOnlyValid(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)
	os.MkdirAll(filepath.Join(dir, ".forge", "hooks"), 0755)

	errs := ValidateOnly(dir)
	if len(errs) != 0 {
		t.Errorf("valid pipeline should have 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateOnlyBadVersion(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(`version: "1.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-1
      name: "1"
      enabled: true
`), 0644)

	errs := ValidateOnly(dir)
	found := false
	for _, e := range errs {
		if containsStr(e.Error(), "version") {
			found = true
		}
	}
	if !found {
		t.Errorf("should report version error, got: %v", errs)
	}
}

func TestValidateOnlyEmptyCheckType(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(`version: "2.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-1
      name: "1"
      enabled: true
      checks:
        - name: "bad"
          type: ""
`), 0644)

	errs := ValidateOnly(dir)
	found := false
	for _, e := range errs {
		if containsStr(e.Error(), "empty type") {
			found = true
		}
	}
	if !found {
		t.Errorf("should report empty check type, got: %v", errs)
	}
}

func TestSaveLoadStatusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	status := &GateStatus{
		Gate:            "gate-1",
		Passed:          true,
		Attempt:         3,
		DurationMs:      1500,
		Mode:            "medium",
		InputArtifacts:  []string{"user_idea"},
		OutputArtifacts: []string{"prd.md"},
	}

	err := SaveStatus(dir, "gate-1", status)
	if err != nil {
		t.Fatalf("SaveStatus: %v", err)
	}

	loaded, err := LoadStatus(dir, "gate-1")
	if err != nil {
		t.Fatalf("LoadStatus: %v", err)
	}
	if loaded.Gate != "gate-1" {
		t.Errorf("gate = %s", loaded.Gate)
	}
	if !loaded.Passed {
		t.Error("passed = false")
	}
	if loaded.Attempt != 3 {
		t.Errorf("attempt = %d", loaded.Attempt)
	}
	if loaded.DurationMs != 1500 {
		t.Errorf("duration_ms = %d", loaded.DurationMs)
	}
}

func TestLoadRejectsEmptyVersion(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	// No version field — empty string
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(`project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-1
      name: "1"
      enabled: true
`), 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("should reject empty version")
	}
}

// TestExecuteGate_ClearsOverrideOnLegitimatePass guards the A5 fix: a gate that
// was previously --force'd gets its override cleared once it passes WITHOUT
// --force. The override must not linger — History records the real pass, so
// dependents no longer need the force to be satisfied. Without this, a one-time
// force permanently substituted for genuine passage on every future run.
func TestExecuteGate_ClearsOverrideOnLegitimatePass(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), []byte(fullYAML), 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	state := &State{PipelineVersion: "2.0", Mode: "medium"}
	gate, _ := p.GetGate("gate-1-prd")

	// Force the gate once (no artifact) → override recorded.
	if _, err := ExecuteGate(dir, gate, state, p, true); err != nil {
		t.Fatalf("ExecuteGate --force: %v", err)
	}
	if !state.IsOverridden("gate-1-prd") {
		t.Fatal("override should be recorded after --force")
	}

	// Now make it pass legitimately: create the artifact and run WITHOUT --force.
	gateDir := filepath.Join(dir, ".forge", "gates", "gate-1-prd")
	os.MkdirAll(gateDir, 0755)
	os.WriteFile(filepath.Join(gateDir, "prd.md"), []byte("# PRD"), 0644)
	result, err := ExecuteGate(dir, gate, state, p, false)
	if err != nil {
		t.Fatalf("ExecuteGate legitimate pass: %v", err)
	}
	if !result.Status.Passed {
		t.Fatalf("gate should pass with artifact: %+v", result.Status.Errors)
	}
	if state.IsOverridden("gate-1-prd") {
		t.Error("override should be cleared after a legitimate (non-force) pass")
	}
}
