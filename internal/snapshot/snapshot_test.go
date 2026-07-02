package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/pipeline"
)

func TestTakeEmptyProject(t *testing.T) {
	dir := t.TempDir()
	snap, err := Take(dir)
	if err != nil {
		t.Fatalf("Take returned error: %v", err)
	}

	s := &snap.Signals
	if s.HasSourceCode {
		t.Error("empty project should not have source code")
	}
	if s.HasTests {
		t.Error("empty project should not have tests")
	}
	if s.HasPkgManager {
		t.Error("empty project should not have package manager")
	}
	if s.HasREADME {
		t.Error("empty project should not have README")
	}
	if s.HasCHANGELOG {
		t.Error("empty project should not have CHANGELOG")
	}
	if s.HasCI {
		t.Error("empty project should not have CI")
	}
	if s.HasGitHistory {
		t.Error("empty project should not have git history")
	}
}

func TestTakeDetectsGoProject(t *testing.T) {
	dir := t.TempDir()

	// Create go.mod
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\ngo 1.21\n"), 0644)

	// Create source files
	os.MkdirAll(filepath.Join(dir, "cmd"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "cmd", "app.go"), []byte("package cmd\nfunc Run() {}\n"), 0644)

	// Create test file
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\nimport \"testing\"\nfunc TestMain(t *testing.T) {}\n"), 0644)

	// Create README
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test Project\n"), 0644)

	// Create CI
	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0755)
	os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte("name: CI\n"), 0644)

	snap, err := Take(dir)
	if err != nil {
		t.Fatalf("Take returned error: %v", err)
	}

	s := &snap.Signals

	if !s.HasPkgManager {
		t.Error("should detect go.mod as package manager")
	}
	if !contains(s.PkgManagerFiles, "go.mod") {
		t.Errorf("PkgManagerFiles should contain 'go.mod', got %v", s.PkgManagerFiles)
	}
	if !s.HasSourceCode {
		t.Error("should detect .go files as source code")
	}
	if s.SourceFileCount != 3 {
		t.Errorf("SourceFileCount = %d, want 3", s.SourceFileCount)
	}
	if !s.HasTests {
		t.Error("should detect _test.go as test file")
	}
	if s.TestFileCount != 1 {
		t.Errorf("TestFileCount = %d, want 1", s.TestFileCount)
	}
	if !s.HasREADME {
		t.Error("should detect README.md")
	}
	if !s.HasCI {
		t.Error("should detect .github/workflows as CI")
	}
}

func TestTakeDetectsNodeProject(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name": "test"}`), 0644)
	os.WriteFile(filepath.Join(dir, "index.ts"), []byte("export function hello() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "index.test.ts"), []byte("test('hello', () => {})\n"), 0644)

	snap, err := Take(dir)
	if err != nil {
		t.Fatalf("Take returned error: %v", err)
	}

	s := &snap.Signals

	if !s.HasPkgManager {
		t.Error("should detect package.json")
	}
	if !s.HasSourceCode {
		t.Error("should detect .ts file")
	}
	if !s.HasTests {
		t.Error("should detect .test.ts file")
	}
}

func TestTakeSkipsIgnoredDirs(t *testing.T) {
	dir := t.TempDir()

	// Create a real source file
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)

	// Create files in skipped directories
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"), []byte("module.exports = {};\n"), 0644)

	os.MkdirAll(filepath.Join(dir, "vendor", "lib"), 0755)
	os.WriteFile(filepath.Join(dir, "vendor", "lib", "lib.go"), []byte("package lib\n"), 0644)

	snap, err := Take(dir)
	if err != nil {
		t.Fatalf("Take returned error: %v", err)
	}

	if snap.Signals.SourceFileCount != 1 {
		t.Errorf("SourceFileCount = %d, want 1 (should skip node_modules and vendor)", snap.Signals.SourceFileCount)
	}
}

func TestInferCompletedGatesEmptyProject(t *testing.T) {
	dir := t.TempDir()
	snap, _ := Take(dir)

	p := makeMediumPipeline()
	gates := InferCompletedGates(snap, p)

	if len(gates) != 0 {
		t.Errorf("empty project should have 0 inferred gates, got %d: %v", len(gates), gates)
	}
}

func TestInferCompletedGatesFullProject(t *testing.T) {
	dir := t.TempDir()

	// Create a project with everything
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\n"), 0644)

	// Source files (need >= 10 for PRD inference without git)
	for i := 0; i < 12; i++ {
		name := strings.Repeat("a", i) + ".go" // unique names
		os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0644)
	}

	// Test file
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\nimport \"testing\"\n"), 0644)

	snap, _ := Take(dir)
	// Manually set commit count for test (no git in temp dir)
	snap.Signals.HasGitHistory = true
	snap.Signals.CommitCount = 30

	p := makeMediumPipeline()
	gates := InferCompletedGates(snap, p)

	inferred := make(map[string]bool)
	for _, ig := range gates {
		inferred[ig.GateID] = true
	}

	// With 30 commits, README, go.mod, source files, and tests:
	// gate-1-prd (README + 30 commits) → YES
	// gate-3-plan (source code + pkg manager + >= 5 files) → YES
	// gate-4-implement (source code + pkg manager) → YES
	// gate-5-test (test files exist) → YES
	// gate-6-acceptance (README + CHANGELOG) → YES
	// gate-8-release → never inferred
	expected := []string{"gate-1-prd", "gate-3-plan", "gate-4-implement", "gate-5-test", "gate-6-acceptance"}
	for _, id := range expected {
		if !inferred[id] {
			t.Errorf("expected gate %s to be inferred, but it was not. Inferred: %v", id, inferred)
		}
	}
	if inferred["gate-8-release"] {
		t.Error("gate-8-release should never be inferred")
	}
}

func TestInferCompletedGatesMonotonicity(t *testing.T) {
	// Test that inference is monotonic: if a middle gate's signals are met
	// but an earlier gate's signals are not, nothing is inferred.
	dir := t.TempDir()

	// Only source code (no README, no pkg manager with enough files)
	// This means gate-1-prd won't be inferred (no README + insufficient commits)
	// But gate-4-implement's individual check (source code >= 10) would pass
	// However it shouldn't be inferred because gate-3-plan depends on gate-1-prd
	// which isn't inferred.
	for i := 0; i < 12; i++ {
		name := strings.Repeat("a", i) + ".go"
		os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0644)
	}

	snap, _ := Take(dir)
	p := makeMediumPipeline()
	gates := InferCompletedGates(snap, p)

	if len(gates) != 0 {
		t.Errorf("monotonicity violated: gates inferred without prerequisites: %v", gates)
	}
}

func TestInferCompletedGatesSmallPipeline(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)

	snap, _ := Take(dir)
	p := makeSmallPipeline()
	gates := InferCompletedGates(snap, p)

	inferred := make(map[string]bool)
	for _, ig := range gates {
		inferred[ig.GateID] = true
	}

	if !inferred["gate-4-implement"] {
		t.Errorf("gate-4-implement should be inferred for small pipeline with source code, got: %v", inferred)
	}
	if inferred["gate-8-release"] {
		t.Error("gate-8-release should never be inferred")
	}
}

func TestInferCompletedGatesLargePipeline(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644)

	// Enough source files for design check (>= 20 files, >= 3 dirs)
	dirs := []string{"cmd", "internal", "pkg"}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(dir, d), 0755)
	}
	for i := 0; i < 25; i++ {
		d := dirs[i%3]
		name := filepath.Join(d, strings.Repeat("a", i)+".go")
		os.WriteFile(filepath.Join(dir, name), []byte("package " + d + "\n"), 0644)
	}

	// Tests
	os.WriteFile(filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0644)

	snap, _ := Take(dir)
	snap.Signals.HasGitHistory = true
	snap.Signals.CommitCount = 60

	p := makeLargePipeline()
	gates := InferCompletedGates(snap, p)

	inferred := make(map[string]bool)
	for _, ig := range gates {
		inferred[ig.GateID] = true
	}

	// With 60 commits, README, go.mod, 25 source files in 3 dirs, tests:
	// gate-0-research (60 commits >= 20) → YES
	// gate-1-prd (README + 60 commits) → YES
	// gate-2-design (25 files, 3 dirs) → YES
	// gate-3-plan (source + pkg + >= 5 files) → YES
	// gate-4-implement (source + pkg) → YES
	// gate-5-test (test files) → YES
	expected := []string{
		"gate-0-research", "gate-1-prd", "gate-2-design",
		"gate-3-plan", "gate-4-implement", "gate-5-test",
	}
	for _, id := range expected {
		if !inferred[id] {
			t.Errorf("expected %s to be inferred in large pipeline. Got: %v", id, inferred)
		}
	}
}

func TestFormatSignalsEmpty(t *testing.T) {
	s := &Signals{}
	out := FormatSignals(s)
	if !strings.Contains(out, "empty project") {
		t.Errorf("FormatSignals for empty project should contain 'empty project', got: %s", out)
	}
}

func TestFormatSignalsNonEmpty(t *testing.T) {
	s := &Signals{
		HasGitHistory:    true,
		CommitCount:      42,
		HasSourceCode:    true,
		SourceFileCount:  10,
		SourceDirs:       3,
		HasTests:         true,
		TestFileCount:    5,
		HasPkgManager:    true,
		PkgManagerFiles:  []string{"go.mod"},
		HasREADME:        true,
		HasCHANGELOG:     true,
		HasCI:            true,
	}
	out := FormatSignals(s)

	checks := []string{"42 commits", "10 files", "5", "go.mod", "README.md", "CHANGELOG.md", "CI"}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("FormatSignals output missing '%s', got: %s", c, out)
		}
	}
}

func TestFormatInferredEmpty(t *testing.T) {
	out := FormatInferred(nil)
	if !strings.Contains(out, "no gates inferred") {
		t.Errorf("FormatInferred(nil) should mention 'no gates inferred', got: %s", out)
	}
}

func TestFormatInferredNonEmpty(t *testing.T) {
	gates := []InferredGate{
		{GateID: "gate-4-implement", Reason: "project has working code", Signals: []string{"15 source files"}},
		{GateID: "gate-5-test", Reason: "project has tests", Signals: []string{"3 test files"}},
	}
	out := FormatInferred(gates)

	if !strings.Contains(out, "gate-4-implement") {
		t.Errorf("FormatInferred output missing gate-4-implement, got: %s", out)
	}
	if !strings.Contains(out, "gate-5-test") {
		t.Errorf("FormatInferred output missing gate-5-test, got: %s", out)
	}
}

func TestInferredGateIDs(t *testing.T) {
	gates := []InferredGate{
		{GateID: "gate-1-prd"},
		{GateID: "gate-3-plan"},
	}
	m := InferredGateIDs(gates)

	if !m["gate-1-prd"] {
		t.Error("gate-1-prd should be in the set")
	}
	if !m["gate-3-plan"] {
		t.Error("gate-3-plan should be in the set")
	}
	if m["gate-4-implement"] {
		t.Error("gate-4-implement should not be in the set")
	}
}

// --- Helper functions to create test pipelines ---

func makeMediumPipeline() *pipeline.Pipeline {
	p := &pipeline.Pipeline{
		Version: "2.0",
		Project: "test",
		Mode:    "medium",
		PipelineDef: pipeline.PipelineDef{
			Gates: []pipeline.Gate{
				{ID: "gate-1-prd", Name: "需求定义", Enabled: true},
				{ID: "gate-3-plan", Name: "实现计划", Enabled: true, DependsOn: []string{"gate-1-prd"}},
				{ID: "gate-4-implement", Name: "代码实现", Enabled: true, DependsOn: []string{"gate-3-plan"}},
				{ID: "gate-5-test", Name: "测试验证", Enabled: true, DependsOn: []string{"gate-4-implement"}},
				{ID: "gate-6-acceptance", Name: "项目验收", Enabled: true, DependsOn: []string{"gate-5-test"}},
				{ID: "gate-8-release", Name: "发布", Enabled: true, DependsOn: []string{"gate-6-acceptance"}},
			},
		},
	}
	_ = p.ValidateDAG()
	return p
}

func makeSmallPipeline() *pipeline.Pipeline {
	p := &pipeline.Pipeline{
		Version: "2.0",
		Project: "test",
		Mode:    "small",
		PipelineDef: pipeline.PipelineDef{
			Gates: []pipeline.Gate{
				{ID: "gate-4-implement", Name: "代码实现", Enabled: true},
				{ID: "gate-8-release", Name: "发布", Enabled: true, DependsOn: []string{"gate-4-implement"}},
			},
		},
	}
	_ = p.ValidateDAG()
	return p
}

func makeLargePipeline() *pipeline.Pipeline {
	p := &pipeline.Pipeline{
		Version: "2.0",
		Project: "test",
		Mode:    "large",
		PipelineDef: pipeline.PipelineDef{
			Gates: []pipeline.Gate{
				{ID: "gate-0-research", Name: "立项调研", Enabled: true},
				{ID: "gate-1-prd", Name: "需求定义", Enabled: true, DependsOn: []string{"gate-0-research"}},
				{ID: "gate-2-design", Name: "技术方案", Enabled: true, DependsOn: []string{"gate-1-prd"}},
				{ID: "gate-3-plan", Name: "实现计划", Enabled: true, DependsOn: []string{"gate-1-prd", "gate-2-design"}},
				{ID: "gate-4-implement", Name: "代码实现", Enabled: true, DependsOn: []string{"gate-3-plan"}},
				{ID: "gate-5-test", Name: "测试验证", Enabled: true, DependsOn: []string{"gate-4-implement"}},
				{ID: "gate-6-acceptance", Name: "项目验收", Enabled: true, DependsOn: []string{"gate-5-test"}},
				{ID: "gate-7-archive", Name: "经验归档", Enabled: true, DependsOn: []string{"gate-6-acceptance"}},
				{ID: "gate-8-release", Name: "发布", Enabled: true, DependsOn: []string{"gate-6-acceptance", "gate-7-archive"}},
			},
		},
	}
	_ = p.ValidateDAG()
	return p
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
