package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// windsurfHooksPathUnder joins the user-level hooks.json path under an isolated home.
//
// windsurfHooksPathUnder 拼出隔离 home 下的 user-level hooks.json 路径。
func windsurfHooksPathUnder(home string) string {
	return filepath.Join(home, ".codeium", "windsurf", "hooks.json")
}

// TestWindsurfTranslator_Translate_CreatesFile verifies Translate creates the
// user-level ~/.codeium/windsurf/hooks.json (Cascade natively loads user-level
// hooks, see https://docs.windsurf.com/windsurf/cascade/hooks) even when the
// directory tree does not exist yet.
func TestWindsurfTranslator_Translate_CreatesFile(t *testing.T) {
	home := isolateHome(t)

	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(windsurfHooksPathUnder(home))
	if err != nil {
		t.Fatalf("user-level hooks.json not created: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"pre_write_code"`, `"pre_run_command"`, "forge hook task-guard --agent windsurf"} {
		if !strings.Contains(content, want) {
			t.Errorf("windsurf user-level hooks.json missing %q", want)
		}
	}
}

// TestWindsurfTranslator_MergePreservesUserEntries pins the merge contract on
// windsurf's flat schema: user entries and unknown top-level fields survive; forge
// entries are replaced wholesale (stale ones removed, current ones present exactly once).
func TestWindsurfTranslator_MergePreservesUserEntries(t *testing.T) {
	home := isolateHome(t)
	path := windsurfHooksPathUnder(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	existing := `{
  "customKey": true,
  "hooks": {
    "pre_run_command": [
      {"command": "echo user-hook", "show_output": true},
      {"command": "forge hook stale-removed-hook --agent windsurf", "show_output": false}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, `"customKey"`) {
		t.Error("user top-level field not preserved")
	}
	if !strings.Contains(content, "echo user-hook") {
		t.Error("user hook entry not preserved")
	}
	if strings.Contains(content, "stale-removed-hook") {
		t.Error("stale forge hook entry not replaced")
	}
	if n := strings.Count(content, `"forge hook task-guard --agent windsurf"`); n != 1 {
		t.Errorf("forge hook task-guard appears %d times, want 1", n)
	}
}

// TestWindsurfTranslator_Idempotent verifies a second Translate leaves the
// user-level hooks.json byte-identical.
func TestWindsurfTranslator_Idempotent(t *testing.T) {
	home := isolateHome(t)
	path := windsurfHooksPathUnder(home)

	tr := &WindsurfTranslator{}
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

// TestStripWindsurfHooksUserLevel covers the strip roundtrip: Translate then Strip
// removes every forge entry while preserving user entries; a second Strip and a
// missing file are clean no-ops.
func TestStripWindsurfHooksUserLevel(t *testing.T) {
	home := isolateHome(t)
	path := windsurfHooksPathUnder(home)

	// Missing file → clean no-op.
	changed, err := StripWindsurfHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("missing file: changed=%v err=%v, want false/nil", changed, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	seed := `{"hooks": {"pre_run_command": [{"command": "echo user-hook", "show_output": true}]}}`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}

	changed, err = StripWindsurfHooksUserLevel()
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
	if !strings.Contains(content, "echo user-hook") {
		t.Error("user hook entry lost after strip")
	}

	// Second strip → no-op.
	changed, err = StripWindsurfHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("second strip: changed=%v err=%v, want false/nil", changed, err)
	}
}
