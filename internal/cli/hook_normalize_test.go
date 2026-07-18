package cli

import (
	"encoding/json"
	"testing"
)

func TestWindsurfNormalize(t *testing.T) {
	// Windsurf pre_write_code: the case that matters most for task-guard
	// enforcement (intercept a write before it lands).
	stdin := mustJSON(t, map[string]any{
		"agent_action_name": "pre_write_code",
		"trajectory_id":     "traj-123",
		"tool_info": map[string]any{
			"file_path": "/app/main.go",
			"edits":     []map[string]any{{"new_string": "package main"}},
		},
	})
	var hi HookInput
	normalizeAgentStdin("windsurf", stdin, &hi)

	if hi.SessionID != "traj-123" {
		t.Errorf("SessionID: got %q, want traj-123", hi.SessionID)
	}
	if hi.ToolName != "Write" {
		t.Errorf("ToolName: got %q, want Write", hi.ToolName)
	}
	if hi.HookEventName != "PreToolUse" {
		t.Errorf("HookEventName: got %q, want PreToolUse", hi.HookEventName)
	}
	var f toolInputFields
	if err := json.Unmarshal(hi.ToolInput, &f); err != nil {
		t.Fatalf("unmarshal normalized tool_input: %v", err)
	}
	if f.FilePath != "/app/main.go" {
		t.Errorf("FilePath: got %q, want /app/main.go", f.FilePath)
	}
	if f.Content != "package main" {
		t.Errorf("Content: got %q, want 'package main'", f.Content)
	}
}

func TestWindsurfNormalizeRunCommand(t *testing.T) {
	// pre_run_command → Bash + command field (bash-guard needs this).
	stdin := mustJSON(t, map[string]any{
		"agent_action_name": "pre_run_command",
		"trajectory_id":     "traj-1",
		"tool_info":         map[string]any{"command": "rm -rf /"},
	})
	var hi HookInput
	normalizeAgentStdin("windsurf", stdin, &hi)

	if hi.ToolName != "Bash" {
		t.Errorf("ToolName: got %q, want Bash", hi.ToolName)
	}
	if hi.HookEventName != "PreToolUse" {
		t.Errorf("HookEventName: got %q, want PreToolUse", hi.HookEventName)
	}
	var f toolInputFields
	json.Unmarshal(hi.ToolInput, &f)
	if f.Command != "rm -rf /" {
		t.Errorf("Command: got %q, want 'rm -rf /'", f.Command)
	}
}

func TestWindsurfNormalizePostRead(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"agent_action_name": "post_read_code",
		"trajectory_id":     "traj-1",
		"tool_info":         map[string]any{"file_path": "/app/x.go"},
	})
	var hi HookInput
	normalizeAgentStdin("windsurf", stdin, &hi)
	if hi.ToolName != "Read" {
		t.Errorf("ToolName: got %q, want Read", hi.ToolName)
	}
	if hi.HookEventName != "PostToolUse" {
		t.Errorf("HookEventName: got %q, want PostToolUse", hi.HookEventName)
	}
}

// TestWindsurfNormalizePreservesClaudeStdin: if an agent ever sends
// Claude-shape stdin despite FORGE_HOOK_AGENT being set, we must not clobber it.
// The existing-existence guards in windsurfNormalize enforce this.
func TestWindsurfNormalizePreservesClaudeStdin(t *testing.T) {
	claude := mustJSON(t, map[string]any{
		"session_id":      "real-cc-session",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Edit",
		"tool_input":      map[string]any{"file_path": "/kept.go"},
	})
	var hi HookInput
	json.Unmarshal(claude, &hi)
	normalizeAgentStdin("windsurf", claude, &hi)

	if hi.SessionID != "real-cc-session" {
		t.Errorf("clobbered SessionID: got %q", hi.SessionID)
	}
	if hi.ToolName != "Edit" {
		t.Errorf("clobbered ToolName: got %q", hi.ToolName)
	}
}

func TestNormalizeUnknownAgentNoOp(t *testing.T) {
	before := HookInput{SessionID: "keep", ToolName: "keep"}
	normalizeAgentStdin("does-not-exist", []byte(`{"x":1}`), &before)
	if before.SessionID != "keep" || before.ToolName != "keep" {
		t.Errorf("unknown agent mutated input: %+v", before)
	}
}

// TestResolveHookAgent covers the --agent flag → FORGE_HOOK_AGENT fallback →
// empty (no normalization) resolution that runHook uses to pick a stdin
// dialect. This is the glue between the cross-platform flag translators set
// and normalizeAgentStdin; without coverage a flag-name typo or a dropped env
// fallback would silently disable agent normalization.
func TestResolveHookAgent(t *testing.T) {
	cases := []struct {
		name            string
		flagVal, envVal string
		want            string
	}{
		{"flag wins", "windsurf", "copilot", "windsurf"},
		{"env fallback when flag empty", "", "copilot", "copilot"},
		{"both empty → no normalization", "", "", ""},
		{"flag alone", "windsurf", "", "windsurf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveHookAgent(c.flagVal, c.envVal); got != c.want {
				t.Errorf("resolveHookAgent(%q, %q) = %q, want %q", c.flagVal, c.envVal, got, c.want)
			}
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
