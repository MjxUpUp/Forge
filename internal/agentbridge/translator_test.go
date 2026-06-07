package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Harness/forge/internal/hooks"
	"github.com/Harness/forge/internal/pipeline"
	"github.com/Harness/forge/internal/protocol"
)

func testInput() *TranslationInput {
	return &TranslationInput{
		Protocol:  protocol.DefaultProtocol("medium"),
		Pipeline:  &pipeline.Pipeline{Project: "test", Mode: "medium"},
		HookNames: hooks.HookNames(),
	}
}

func TestCursorTranslator_Translate(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	translator := &CursorTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".cursor", "rules", "forge-quality.mdc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "description: \"Forge quality protocol\"") {
		t.Error("missing MDC frontmatter")
	}
	if !strings.Contains(content, "alwaysApply: true") {
		t.Error("missing alwaysApply")
	}
	if !strings.Contains(content, "质量标准") {
		t.Error("missing quality standards section")
	}
	if !strings.Contains(content, "代码编译") {
		t.Error("missing compile standard")
	}
}

func TestCursorTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&CursorTranslator{}).Detect(dir) {
		t.Error("should not detect without .cursor/")
	}
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)
	if !(&CursorTranslator{}).Detect(dir) {
		t.Error("should detect with .cursor/")
	}
}

func TestCopilotTranslator_Translate(t *testing.T) {
	dir := t.TempDir()

	translator := &CopilotTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".github", "instructions", "forge-quality.instructions.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "applyTo:") {
		t.Error("missing applyTo frontmatter")
	}
	if !strings.Contains(content, "[ERROR]") {
		t.Error("missing ERROR severity")
	}
	if !strings.Contains(content, "ALWAYS:") {
		t.Error("missing ALWAYS rules")
	}
}

func TestCopilotTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&CopilotTranslator{}).Detect(dir) {
		t.Error("should not detect without .github/instructions/")
	}
	os.MkdirAll(filepath.Join(dir, ".github", "instructions"), 0755)
	if !(&CopilotTranslator{}).Detect(dir) {
		t.Error("should detect with .github/instructions/")
	}
}

func TestWindsurfTranslator_Translate(t *testing.T) {
	dir := t.TempDir()

	translator := &WindsurfTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".windsurfrules")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, forgeRulesStart) {
		t.Error("missing FORGE:START marker")
	}
	if !strings.Contains(content, forgeRulesEnd) {
		t.Error("missing FORGE:END marker")
	}
	if !strings.Contains(content, "代码编译") {
		t.Error("missing compile standard")
	}
}

func TestWindsurfTranslator_PreserveContent(t *testing.T) {
	dir := t.TempDir()
	existing := "# My custom rules\nDo something cool.\n\n"
	os.WriteFile(filepath.Join(dir, ".windsurfrules"), []byte(existing), 0644)

	translator := &WindsurfTranslator{}
	if err := translator.Translate(dir, testInput()); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".windsurfrules"))
	content := string(data)
	if !strings.Contains(content, "My custom rules") {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, forgeRulesStart) {
		t.Error("forge section should be appended")
	}
}

func TestWindsurfTranslator_Detect(t *testing.T) {
	dir := t.TempDir()
	if (&WindsurfTranslator{}).Detect(dir) {
		t.Error("should not detect without .windsurfrules")
	}
	os.WriteFile(filepath.Join(dir, ".windsurfrules"), []byte("rules"), 0644)
	if !(&WindsurfTranslator{}).Detect(dir) {
		t.Error("should detect with .windsurfrules")
	}
}

func TestBridge_TranslateForAgents(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	errs := TranslateForAgents(dir, []AgentType{AgentCursor}, testInput())
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	// Verify file was created
	path := filepath.Join(dir, ".cursor", "rules", "forge-quality.mdc")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cursor rules file not created: %v", err)
	}
}

func TestBridge_TranslateForAgents_Empty(t *testing.T) {
	dir := t.TempDir()
	errs := TranslateForAgents(dir, nil, testInput())
	if len(errs) != 0 {
		t.Fatalf("expected no errors for empty agents, got %v", errs)
	}
}

func TestAllTranslators(t *testing.T) {
	translators := AllTranslators()
	if len(translators) != 4 {
		t.Fatalf("expected 4 translators, got %d", len(translators))
	}
	types := make(map[AgentType]bool)
	for _, tr := range translators {
		types[tr.AgentType()] = true
	}
	for _, expected := range []AgentType{AgentClaudeCode, AgentCursor, AgentCopilot, AgentWindsurf} {
		if !types[expected] {
			t.Errorf("missing translator for %s", expected)
		}
	}
}
