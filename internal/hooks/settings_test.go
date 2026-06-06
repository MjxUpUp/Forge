package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSettingsCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	expected := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("settings file not created at %s", expected)
	}
}

func TestGenerateSettingsJSONStructure(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	hooks, ok := parsed["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing top-level 'hooks' key or wrong type")
	}

	for _, key := range []string{"PreToolUse", "PostToolUse", "Stop"} {
		if _, exists := hooks[key]; !exists {
			t.Errorf("hooks.%s not found", key)
		}
	}
}

func TestGenerateSettingsHookEntries(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	for _, hookGroup := range parsed.Hooks {
		for _, matcher := range hookGroup {
			for _, entry := range matcher.Hooks {
				if entry.Type != "command" {
					t.Errorf("hook entry has type=%q, want \"command\"", entry.Type)
				}
				if entry.Command == "" {
					t.Error("hook entry has empty command")
				}
			}
		}
	}
}

func TestWriteHookTemplatesCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHookTemplates(dir); err != nil {
		t.Fatalf("WriteHookTemplates returned error: %v", err)
	}

	hooksDir := filepath.Join(dir, "hooks")
	expected := []string{"auto-compile.sh", "assertion-check.sh", "experience-check.sh"}
	for _, name := range expected {
		path := filepath.Join(hooksDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("hook file not created: %s", path)
		}
	}
}

func TestWriteHookTemplatesContentMatches(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHookTemplates(dir); err != nil {
		t.Fatalf("WriteHookTemplates returned error: %v", err)
	}

	hooksDir := filepath.Join(dir, "hooks")
	cases := []struct {
		filename string
		needle   string
	}{
		{"auto-compile.sh", "go build ./..."},
		{"auto-compile.sh", "cargo check"},
		{"assertion-check.sh", "Assertion weakening detected"},
		{"assertion-check.sh", "Fix the code, not the tests."},
		{"experience-check.sh", "experience-check"},
		{"experience-check.sh", "KNOWLEDGE_DIR"},
	}

	for _, tc := range cases {
		data, err := os.ReadFile(filepath.Join(hooksDir, tc.filename))
		if err != nil {
			t.Fatalf("failed to read %s: %v", tc.filename, err)
		}
		content := string(data)
		if !containsString(content, tc.needle) {
			t.Errorf("%s: expected to contain %q", tc.filename, tc.needle)
		}
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
