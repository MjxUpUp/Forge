package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexTranslator_Translate_CreatesFile verifies Translate works when the codex
// home (and hooks.json) does not exist yet — e.g. wiring before codex's first run.
func TestCodexTranslator_Translate_CreatesFile(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "not-yet-created")
	t.Setenv("CODEX_HOME", codexHome)

	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "hooks.json")); err != nil {
		t.Fatalf("hooks.json not created: %v", err)
	}
}

// TestCodexTranslator_MergePreservesUserEntries pins the merge contract: user-defined
// hook entries (commands not forge-sourced) and unknown top-level fields survive
// Translate; forge entries are present exactly once (replaced wholesale, not appended).
func TestCodexTranslator_MergePreservesUserEntries(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "hooks.json")

	existing := `{
  "notify": ["user-notify-script"],
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/lint.sh"}]}
    ],
    "PostToolUse": [
      {"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "forge hook stale-removed-hook"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// User top-level field and user hook entry preserved.
	if !strings.Contains(content, `"notify"`) || !strings.Contains(content, "user-notify-script") {
		t.Error("user top-level field not preserved")
	}
	if !strings.Contains(content, "./scripts/lint.sh") {
		t.Error("user hook entry not preserved")
	}
	// Stale forge entry replaced: a command that is no longer generated must be gone.
	if strings.Contains(content, "stale-removed-hook") {
		t.Error("stale forge hook entry not replaced")
	}
	// Forge wiring present exactly once per command.
	if n := strings.Count(content, `"forge hook task-guard"`); n != 1 {
		t.Errorf("forge hook task-guard appears %d times, want 1", n)
	}
	if !strings.Contains(content, "forge hook bash-guard") {
		t.Error("generated forge wiring missing")
	}
}

// TestCodexTranslator_Idempotent verifies a second Translate is a byte-identical no-op.
func TestCodexTranslator_Idempotent(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "hooks.json")

	tr := &CodexTranslator{}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("second Translate not idempotent")
	}
}

// TestStripCodexHooksUserLevel covers the strip roundtrip: Translate then Strip
// removes every forge entry while preserving user entries; a second Strip and a
// missing file are clean no-ops.
func TestStripCodexHooksUserLevel(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	path := filepath.Join(codexHome, "hooks.json")

	// Missing file → clean no-op.
	changed, err := StripCodexHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("missing file: changed=%v err=%v, want false/nil", changed, err)
	}

	// Seed a user entry, then Translate to add forge wiring.
	seed := `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/lint.sh"}]}]}}`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CodexTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}

	changed, err = StripCodexHooksUserLevel()
	if err != nil || !changed {
		t.Fatalf("strip after Translate: changed=%v err=%v, want true/nil", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "forge hook") || strings.Contains(content, "forge gate") {
		t.Errorf("forge entries remain after strip:\n%s", content)
	}
	if !strings.Contains(content, "./scripts/lint.sh") {
		t.Error("user hook entry lost after strip")
	}
	// File itself must survive (user-level config is never deleted).
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("stripped file is not valid JSON: %v", err)
	}

	// Second strip → no-op.
	changed, err = StripCodexHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("second strip: changed=%v err=%v, want false/nil", changed, err)
	}
}
