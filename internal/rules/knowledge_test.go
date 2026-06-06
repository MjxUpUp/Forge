package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeCheckNoDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)

	// No ~/.forge/knowledge/gotchas/ directory exists.
	e := &KnowledgeCheckEvaluator{}
	ctx := Context{ProjectRoot: tmpDir}
	result := e.Evaluate(ctx, CheckParams{})

	if !result.Passed {
		t.Fatalf("expected pass when no gotchas dir, got fail: %s — %s", result.Detail, result.Message)
	}
}

func TestKnowledgeCheckWithViolation(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Create gotchas knowledge base with a pattern.
	gotchasDir := filepath.Join(tmpHome, ".forge", "knowledge", "gotchas")
	if err := os.MkdirAll(gotchasDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a gotcha file that forbids TODO in source files.
	// Raw string: \b is preserved as literal backslash-b for regexp word boundary.
	mdContent := "# No TODO\n\n**Patterns:** TODO\\b\n"
	if err := os.WriteFile(filepath.Join(gotchasDir, "no-todo.md"), []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a project with a .go file containing "TODO".
	projectDir := t.TempDir()
	goFile := filepath.Join(projectDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\n// TODO fix this later\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &KnowledgeCheckEvaluator{}
	ctx := Context{ProjectRoot: projectDir}
	result := e.Evaluate(ctx, CheckParams{})

	if result.Passed {
		t.Fatal("expected violation to be detected, got pass")
	}
}

func TestKnowledgeCheckNoMatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	gotchasDir := filepath.Join(tmpHome, ".forge", "knowledge", "gotchas")
	if err := os.MkdirAll(gotchasDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pattern that won't match anything in the project.
	mdContent := "# Unlikely\n\n**Patterns:** XXXXZ_NOT_EXIST_PATTERN_12345\n"
	if err := os.WriteFile(filepath.Join(gotchasDir, "unlikely.md"), []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &KnowledgeCheckEvaluator{}
	ctx := Context{ProjectRoot: projectDir}
	result := e.Evaluate(ctx, CheckParams{})

	if !result.Passed {
		t.Fatalf("expected pass when no match, got fail: %s", result.Detail)
	}
}

func TestKnowledgeCheckBadRegex(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	gotchasDir := filepath.Join(tmpHome, ".forge", "knowledge", "gotchas")
	if err := os.MkdirAll(gotchasDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a gotcha file with an invalid regex — should not panic.
	mdContent := "# Bad Regex\n\n**Patterns:** [invalid(\n"
	if err := os.WriteFile(filepath.Join(gotchasDir, "bad-regex.md"), []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &KnowledgeCheckEvaluator{}
	ctx := Context{ProjectRoot: projectDir}

	// Must not panic.
	result := e.Evaluate(ctx, CheckParams{})
	if !result.Passed {
		t.Fatalf("expected pass (bad regex skipped), got fail: %s", result.Detail)
	}
}

func TestExtractPatterns(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "single pattern",
			content:  "# Title\n\n**Patterns:** TODO\\b\n",
			expected: []string{`TODO\b`},
		},
		{
			name:     "multiple patterns comma-separated",
			content:  "# Title\n\n**Patterns:** TODO\\b, HACK\\b, FIXME\n",
			expected: []string{`TODO\b`, `HACK\b`, "FIXME"},
		},
		{
			name:     "no Patterns marker",
			content:  "# Title\n\nJust some text without patterns.\n",
			expected: nil,
		},
		{
			name:     "patterns with spaces",
			content:  "**Patterns:**  alpha ,  beta \n",
			expected: []string{"alpha", "beta"},
		},
		{
			name:     "empty patterns after marker",
			content:  "**Patterns:**\n",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPatterns(tt.content)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
			for i, p := range got {
				if p != tt.expected[i] {
					t.Fatalf("pattern[%d]: expected %q, got %q", i, tt.expected[i], p)
				}
			}
		})
	}
}
