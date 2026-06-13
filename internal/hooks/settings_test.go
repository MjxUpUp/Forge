package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
				// Hook commands must NOT use bash with relative .forge/ paths.
				if strings.Contains(entry.Command, "bash .forge/") {
					t.Errorf("hook command uses relative path %q — must use 'forge hook <name>'", entry.Command)
				}
			}
		}
	}
}

func TestGenerateSettingsUsesForgeHook(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}
	content := string(data)

	// All hook invocations should route through "forge hook <name>"
	for _, name := range []string{"auto-compile", "assertion-check", "experience-check", "task-verify", "task-guard", "bash-guard", "file-sentinel"} {
		expected := "forge hook " + name
		if !strings.Contains(content, expected) {
			t.Errorf("settings missing %q command", expected)
		}
	}
}

func TestEmbeddedContent(t *testing.T) {
	// Known hooks return content and true
	for _, name := range []string{"auto-compile", "assertion-check", "experience-check", "task-verify", "bash-guard", "file-sentinel", "task-guard"} {
		content, ok := EmbeddedContent(name)
		if !ok {
			t.Errorf("EmbeddedContent(%q) returned false", name)
		}
		if len(content) == 0 {
			t.Errorf("EmbeddedContent(%q) returned empty content", name)
		}
	}

	// Unknown hook returns false
	_, ok := EmbeddedContent("nonexistent")
	if ok {
		t.Error("EmbeddedContent should return false for unknown hook")
	}
}

func TestWriteHookTemplatesCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHookTemplates(dir); err != nil {
		t.Fatalf("WriteHookTemplates returned error: %v", err)
	}

	hooksDir := filepath.Join(dir, "hooks")
	expected := []string{"auto-compile.sh", "assertion-check.sh", "experience-check.sh", "task-verify.sh", "task-guard.sh", "bash-guard.sh", "file-sentinel.sh"}
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

func TestStopHooksIncludeTaskVerify(t *testing.T) {
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
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	stopHooks := parsed.Hooks["Stop"]
	found := false
	for _, group := range stopHooks {
		for _, h := range group.Hooks {
			if strings.Contains(h.Command, "forge hook task-verify") {
				found = true
			}
		}
	}
	if !found {
		t.Error("Stop hooks missing 'forge hook task-verify' command")
	}
}

func TestTaskVerifyHookContainsMasterCheck(t *testing.T) {
	if !containsString(TaskVerifyHook, "Code changes on") {
		t.Error("TaskVerifyHook missing 'Code changes on' master branch detection")
	}
	if !containsString(TaskVerifyHook, "without active task") {
		t.Error("TaskVerifyHook missing 'without active task' warning")
	}
	if !containsString(TaskVerifyHook, "forge task start") {
		t.Error("TaskVerifyHook missing 'forge task start' hint in warning")
	}
}


func TestTaskGuardHookContainsKeyChecks(t *testing.T) {
	if !containsString(TaskGuardHook, "FORGE_TASK_REF") {
		t.Error("TaskGuardHook missing FORGE_TASK_REF check")
	}
	if !containsString(TaskGuardHook, "FORGE_TASK_GATE") {
		t.Error("TaskGuardHook missing FORGE_TASK_GATE check")
	}
	if !containsString(TaskGuardHook, "WARN [task-guard]") {
		t.Error("TaskGuardHook missing WARN for no-task scenario")
	}
	if !containsString(TaskGuardHook, "task-design") {
		t.Error("TaskGuardHook missing task-design gate check")
	}
	if !containsString(TaskGuardHook, "WARN") {
		t.Error("TaskGuardHook missing WARN for pre-design state")
	}
}

func TestTaskGuardHookPassesNonCodeFiles(t *testing.T) {
	if !containsString(TaskGuardHook, ".(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)") {
		t.Error("TaskGuardHook missing code file extension filter")
	}
}

func TestBashGuardHookContainsKeyChecks(t *testing.T) {
	if !containsString(BashGuardHook, "FORGE_COMMAND") {
		t.Error("BashGuardHook missing FORGE_COMMAND check")
	}
	if !containsString(BashGuardHook, "writeFile") {
		t.Error("BashGuardHook missing writeFile pattern detection")
	}
	if !containsString(BashGuardHook, "WARN [bash-guard]") {
		t.Error("BashGuardHook missing WARN for no-task scenario")
	}
	if !containsString(BashGuardHook, "bash-guard") {
		t.Error("BashGuardHook missing [bash-guard] prefix")
	}
}

func TestFileSentinelHookContainsKeyChecks(t *testing.T) {
	if !containsString(FileSentinelHook, "SNAPSHOT_FILE") {
		t.Error("FileSentinelHook missing SNAPSHOT_FILE reference")
	}
	if !containsString(FileSentinelHook, "file-sentinel") {
		t.Error("FileSentinelHook missing [file-sentinel] prefix")
	}
	if !containsString(FileSentinelHook, "git checkout") {
		t.Error("FileSentinelHook missing git checkout revert logic")
	}
}

func TestTaskGuardHookSelfProtection(t *testing.T) {
	if !containsString(TaskGuardHook, ".forge/*") {
		t.Error("TaskGuardHook missing .forge/ self-protection")
	}
	if !containsString(TaskGuardHook, ".claude/settings") {
		t.Error("TaskGuardHook missing .claude/settings self-protection")
	}
	if !containsString(TaskGuardHook, "Forge-managed") {
		t.Error("TaskGuardHook missing self-protection error message")
	}
}

func TestPreToolUseHasBashGuard(t *testing.T) {
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
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	found := false
	for _, matcher := range parsed.Hooks["PreToolUse"] {
		if matcher.Matcher == "Bash" {
			for _, h := range matcher.Hooks {
				if h.Command == "forge hook bash-guard" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("PreToolUse missing Bash matcher with 'forge hook bash-guard'")
	}
}

func TestPostToolUseHasFileSentinel(t *testing.T) {
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
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	found := false
	for _, matcher := range parsed.Hooks["PostToolUse"] {
		if matcher.Matcher == "Bash" {
			for _, h := range matcher.Hooks {
				if h.Command == "forge hook file-sentinel" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("PostToolUse missing Bash matcher with 'forge hook file-sentinel'")
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
