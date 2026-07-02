package skillgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/pipeline"
	"github.com/MjxUpUp/Forge/internal/rules"
)

func makeTestPipeline() *pipeline.Pipeline {
	return &pipeline.Pipeline{
		Project: "test-project",
		Mode:    "small",
		PipelineDef: pipeline.PipelineDef{
			Gates: []pipeline.Gate{
				{
					ID:      "gate-1",
					Name:    "Architecture Review",
					Enabled: true,
					Prompt:  "Review the architecture and identify issues.",
					OnFailure: "abort",
					Checks: []rules.Check{
						{Name: "has-design-doc", Type: "file_exists", Params: rules.CheckParams{File: "docs/design.md"}},
					},
					Artifacts: pipeline.GateArtifacts{
						Outputs: []string{"docs/design.md"},
					},
				},
				{
					ID:        "gate-2",
					Name:      "Implementation",
					Enabled:   true,
					DependsOn: []string{"gate-1"},
					Prompt:    "Implement the approved design.",
					OnFailure: "warn",
					Checks: []rules.Check{
						{Name: "compile-check", Type: "custom_script", Params: rules.CheckParams{Script: "auto-compile.sh"}},
					},
				},
			},
		},
	}
}

func TestGenerateSkillCreatesFile(t *testing.T) {
	dir := t.TempDir()
	p := makeTestPipeline()

	if err := GenerateSkill(dir, p, nil); err != nil {
		t.Fatalf("GenerateSkill returned error: %v", err)
	}

	expected := filepath.Join(dir, ".claude", "skills", "forge-pipeline", "SKILL.md")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("SKILL.md not created at %s", expected)
	}
}

func TestGenerateSkillFrontMatter(t *testing.T) {
	dir := t.TempDir()
	p := makeTestPipeline()

	if err := GenerateSkill(dir, p, nil); err != nil {
		t.Fatalf("GenerateSkill returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "forge-pipeline", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		t.Error("SKILL.md does not start with '---' front matter delimiter")
	}

	// Check that front matter contains name: forge-pipeline
	fmEnd := strings.Index(content[4:], "\n---\n")
	if fmEnd < 0 {
		t.Fatal("SKILL.md front matter closing '---' not found")
	}
	fm := content[4 : fmEnd+4]
	if !strings.Contains(fm, "name: forge-pipeline") {
		t.Errorf("front matter missing 'name: forge-pipeline', got:\n%s", fm)
	}
}

func TestGenerateSkillContainsGatePrompts(t *testing.T) {
	dir := t.TempDir()
	p := makeTestPipeline()

	if err := GenerateSkill(dir, p, nil); err != nil {
		t.Fatalf("GenerateSkill returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "forge-pipeline", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}
	content := string(data)

	for _, gate := range p.PipelineDef.Gates {
		if gate.Prompt != "" && !strings.Contains(content, gate.Prompt) {
			t.Errorf("SKILL.md missing prompt for gate %s: %q", gate.ID, gate.Prompt)
		}
	}
}

func TestGenerateSkillContainsCheckRules(t *testing.T) {
	dir := t.TempDir()
	p := makeTestPipeline()

	if err := GenerateSkill(dir, p, nil); err != nil {
		t.Fatalf("GenerateSkill returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "forge-pipeline", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "检查规则") {
		t.Error("SKILL.md missing '检查规则' section header")
	}

	// Also verify individual check names appear
	for _, gate := range p.PipelineDef.Gates {
		for _, check := range gate.Checks {
			if !strings.Contains(content, check.Name) {
				t.Errorf("SKILL.md missing check name %q from gate %s", check.Name, gate.ID)
			}
		}
	}
}

func TestGenerateSkillEmptyPipeline(t *testing.T) {
	dir := t.TempDir()
	p := &pipeline.Pipeline{
		Project: "empty-project",
		Mode:    "small",
	}

	// Should not panic and should return nil error
	if err := GenerateSkill(dir, p, nil); err != nil {
		t.Fatalf("GenerateSkill with empty pipeline returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "forge-pipeline", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "empty-project") {
		t.Error("SKILL.md missing project name for empty pipeline")
	}
	if !strings.Contains(content, "Gate 数量**: 0") {
		t.Error("SKILL.md should report 0 gates for empty pipeline")
	}
}
