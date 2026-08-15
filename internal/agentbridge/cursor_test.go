package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cursorHooksPathUnder joins the user-level hooks.json path under an isolated home.
//
// cursorHooksPathUnder 拼出隔离 home 下的 user-level hooks.json 路径。
func cursorHooksPathUnder(home string) string {
	return filepath.Join(home, ".cursor", "hooks.json")
}

// TestCursorTranslator_Translate_CreatesFile verifies Translate works when
// ~/.cursor/hooks.json does not exist yet (fresh machine).
func TestCursorTranslator_Translate_CreatesFile(t *testing.T) {
	home := isolateHome(t)

	if err := (&CursorTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if _, err := os.Stat(cursorHooksPathUnder(home)); err != nil {
		t.Fatalf("user-level hooks.json not created: %v", err)
	}
}

// TestCursorTranslator_MergePreservesUserEntries pins the merge contract on cursor's
// flat schema: user entries and unknown top-level fields survive; forge entries are
// replaced wholesale (stale ones removed, current ones present exactly once).
func TestCursorTranslator_MergePreservesUserEntries(t *testing.T) {
	home := isolateHome(t)
	path := cursorHooksPathUnder(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	existing := `{
  "version": 1,
  "customKey": {"a": 1},
  "hooks": {
    "preToolUse": [
      {"command": "npx prettier --check .", "matcher": "Write|Edit"},
      {"command": "forge hook stale-removed-hook", "matcher": "Write|Edit", "timeout": 60}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := (&CursorTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
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
	if !strings.Contains(content, "npx prettier --check .") {
		t.Error("user hook entry not preserved")
	}
	if strings.Contains(content, "stale-removed-hook") {
		t.Error("stale forge hook entry not replaced")
	}
	if n := strings.Count(content, `"forge hook task-guard --agent cursor"`); n != 1 {
		t.Errorf("forge hook task-guard appears %d times, want 1", n)
	}
}

// TestCursorTranslator_Idempotent verifies a second Translate is a byte-identical no-op.
func TestCursorTranslator_Idempotent(t *testing.T) {
	home := isolateHome(t)
	path := cursorHooksPathUnder(home)

	tr := &CursorTranslator{}
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

// TestStripCursorHooksUserLevel covers the strip roundtrip: Translate then Strip
// removes every forge entry while preserving user entries and the version field; a
// second Strip and a missing file are clean no-ops.
func TestStripCursorHooksUserLevel(t *testing.T) {
	home := isolateHome(t)
	path := cursorHooksPathUnder(home)

	// Missing file → clean no-op.
	changed, err := StripCursorHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("missing file: changed=%v err=%v, want false/nil", changed, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	seed := `{"version": 1, "hooks": {"preToolUse": [{"command": "npx prettier --check .", "matcher": "Write|Edit"}]}}`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CursorTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}

	changed, err = StripCursorHooksUserLevel()
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
	if !strings.Contains(content, "npx prettier --check .") {
		t.Error("user hook entry lost after strip")
	}
	if !strings.Contains(content, `"version"`) {
		t.Error("version field lost after strip")
	}

	// Second strip → no-op.
	changed, err = StripCursorHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("second strip: changed=%v err=%v, want false/nil", changed, err)
	}
}

// TestCursorTranslator_MergePreservesUnknownFields pins the raw-merge fix: user
// hook entries carrying fields the typed cursorHookEntry struct does not declare
// (e.g. powershell) must survive Translate with values intact — a typed
// round-trip silently dropped them.
//
// TestCursorTranslator_MergePreservesUnknownFields 钉死 raw-merge 修复：携带
// 类型化 cursorHookEntry 未声明字段（如 powershell）的用户 hook 条目必须在
// Translate 后值完整保留——类型化往返会静默丢弃它们。
func TestCursorTranslator_MergePreservesUnknownFields(t *testing.T) {
	home := isolateHome(t)
	path := cursorHooksPathUnder(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	existing := `{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {"command": "npx prettier --check .", "matcher": "Write|Edit", "powershell": "prettier --check .", "timeout": 45}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&CursorTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Field-level assertion (formatting-agnostic): the kept user entry must carry
	// every original field with its original value.
	var cfg struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	var userEntry map[string]any
	for _, e := range cfg.Hooks["preToolUse"] {
		if e["command"] == "npx prettier --check ." {
			userEntry = e
		}
	}
	if userEntry == nil {
		t.Fatal("user hook entry not preserved")
	}
	if userEntry["powershell"] != "prettier --check ." {
		t.Errorf("user entry unknown field powershell lost or altered: %v", userEntry["powershell"])
	}
	if userEntry["timeout"] != float64(45) {
		t.Errorf("user entry field timeout lost or altered: %v", userEntry["timeout"])
	}
}

// TestCursorMatcherTokens pins the Claude→Cursor tool-name translation (Wave 2a):
// Bash→Shell, Edit→Write (cursor reports file create AND edit as Write), Agent→Task,
// Skill dropped; tokens deduplicated after translation; unknown tokens pass through;
// empty stays empty (match-all event groups); an all-dropped (Skill-only) matcher
// yields keep=false so the caller skips the entries instead of wiring match-all.
func TestCursorMatcherTokens(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Write|Edit", "Write"},
		{"Bash", "Shell"},
		{"Read|Skill|Agent", "Read|Task"},
		{"Read", "Read"},
		{"", ""},
		{"SomeFutureTool", "SomeFutureTool"},
	}
	for _, tc := range cases {
		got, keep := cursorMatcherTokens(tc.in)
		if !keep {
			t.Errorf("cursorMatcherTokens(%q) unexpectedly dropped all tokens", tc.in)
			continue
		}
		if got != tc.want {
			t.Errorf("cursorMatcherTokens(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, keep := cursorMatcherTokens("Skill"); keep {
		t.Error("Skill-only matcher must yield keep=false (no Cursor tool to fire on)")
	}
}

// TestBuildCursorHooks_AgentSuffixAndMatchers verifies the generated cursor wiring
// end-to-end: every forge command carries ` --agent cursor`, and no matcher still
// uses a Claude-only tool token (Bash/Edit/Agent/Skill — they never match on
// Cursor and silently disarm the gate).
func TestBuildCursorHooks_AgentSuffixAndMatchers(t *testing.T) {
	hooksMap, ok := buildCursorHooks()["hooks"].(map[string][]cursorHookEntry)
	if !ok {
		t.Fatal("buildCursorHooks shape changed — hooks map missing")
	}
	sawForge := false
	for event, entries := range hooksMap {
		for _, e := range entries {
			if !strings.HasPrefix(e.Command, "forge hook ") {
				continue
			}
			sawForge = true
			if !strings.HasSuffix(e.Command, " --agent cursor") {
				t.Errorf("%s: forge command missing --agent cursor suffix: %s", event, e.Command)
			}
			for _, tok := range strings.Split(e.Matcher, "|") {
				switch tok {
				case "Bash", "Edit", "Agent", "Skill":
					t.Errorf("%s: matcher %q still uses Claude-only token %q (never matches on Cursor)", event, e.Matcher, tok)
				}
			}
		}
	}
	if !sawForge {
		t.Fatal("no forge commands generated")
	}
}
