package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDAGValidChain(t *testing.T) {
	yaml := []byte(`version: "2.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-1-prd
      name: "PRD"
      enabled: true
      depends_on: []
    - id: gate-3-plan
      name: "Plan"
      enabled: true
      depends_on: [gate-1-prd]
    - id: gate-8-release
      name: "Release"
      enabled: true
      depends_on: [gate-3-plan]
`)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), yaml, 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	order := p.TopoOrder()
	if len(order) != 3 {
		t.Errorf("TopoOrder length = %d, want 3", len(order))
	}

	// gate-1 should come before gate-3, gate-3 before gate-8
	assertBefore(t, order, "gate-1-prd", "gate-3-plan")
	assertBefore(t, order, "gate-3-plan", "gate-8-release")
}

func TestDAGDiamond(t *testing.T) {
	// A -> B, A -> C, B -> D, C -> D
	yaml := []byte(`version: "2.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-a
      name: "A"
      enabled: true
      depends_on: []
    - id: gate-b
      name: "B"
      enabled: true
      depends_on: [gate-a]
    - id: gate-c
      name: "C"
      enabled: true
      depends_on: [gate-a]
    - id: gate-d
      name: "D"
      enabled: true
      depends_on: [gate-b, gate-c]
`)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), yaml, 0644)

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	order := p.TopoOrder()
	assertBefore(t, order, "gate-a", "gate-b")
	assertBefore(t, order, "gate-a", "gate-c")
	assertBefore(t, order, "gate-b", "gate-d")
	assertBefore(t, order, "gate-c", "gate-d")
}

func TestDAGCycleDetected(t *testing.T) {
	yaml := []byte(`version: "2.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-a
      name: "A"
      enabled: true
      depends_on: [gate-c]
    - id: gate-b
      name: "B"
      enabled: true
      depends_on: [gate-a]
    - id: gate-c
      name: "C"
      enabled: true
      depends_on: [gate-b]
`)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), yaml, 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() should fail with cycle detection")
	}
	// Error is wrapped by Load() — check that it mentions cycle
	if dagErr, ok := err.(*DAGError); !ok {
		// Load wraps the error, so check the error message instead
		if err.Error() == "" || !containsStr(err.Error(), "circular") {
			t.Errorf("expected DAG cycle error, got: %v", err)
		}
	} else if dagErr.Type != "cycle" {
		t.Errorf("expected DAG cycle error type, got: %v", dagErr.Type)
	}
}

func TestDAGMissingDependency(t *testing.T) {
	yaml := []byte(`version: "2.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-a
      name: "A"
      enabled: true
      depends_on: [nonexistent]
`)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), yaml, 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() should fail with missing dependency")
	}
}

func TestDAGDuplicateID(t *testing.T) {
	yaml := []byte(`version: "2.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-a
      name: "A"
      enabled: true
      depends_on: []
    - id: gate-a
      name: "A2"
      enabled: true
      depends_on: []
`)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), yaml, 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() should fail with duplicate ID")
	}
}

func TestNextReadyGate(t *testing.T) {
	yaml := []byte(`version: "2.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-1
      name: "1"
      enabled: true
      depends_on: []
    - id: gate-2
      name: "2"
      enabled: true
      depends_on: [gate-1]
    - id: gate-3
      name: "3"
      enabled: true
      depends_on: [gate-2]
`)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), yaml, 0644)

	p, _ := Load(dir)

	// Nothing completed → gate-1 is ready
	next := p.NextReadyGate(map[string]bool{})
	if next != "gate-1" {
		t.Errorf("NextReadyGate(empty) = %s, want gate-1", next)
	}

	// gate-1 completed → gate-2 is ready
	next = p.NextReadyGate(map[string]bool{"gate-1": true})
	if next != "gate-2" {
		t.Errorf("NextReadyGate(gate-1 done) = %s, want gate-2", next)
	}

	// all completed → nothing ready
	next = p.NextReadyGate(map[string]bool{"gate-1": true, "gate-2": true, "gate-3": true})
	if next != "" {
		t.Errorf("NextReadyGate(all done) = %s, want empty", next)
	}
}

func TestV1PipelineRejected(t *testing.T) {
	yaml := []byte(`version: "1.0"
project: "test"
mode: medium
pipeline:
  gates:
    - id: gate-1
      name: "1"
      enabled: true
      depends_on: []
`)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
	os.WriteFile(filepath.Join(dir, ".forge", "pipeline.yml"), yaml, 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() should reject v1 format")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func assertBefore(t *testing.T, order []string, before, after string) {
	t.Helper()
	beforeIdx, afterIdx := -1, -1
	for i, id := range order {
		if id == before {
			beforeIdx = i
		}
		if id == after {
			afterIdx = i
		}
	}
	if beforeIdx == -1 || afterIdx == -1 {
		t.Fatalf("%s or %s not found in topo order %v", before, after, order)
	}
	if beforeIdx >= afterIdx {
		t.Errorf("%s (idx %d) should come before %s (idx %d)", before, beforeIdx, after, afterIdx)
	}
}
