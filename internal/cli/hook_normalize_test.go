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
	// pre_run_command → Bash + command field (bash-guard needs this). The
	// payload uses the documented shape: tool_info.command_line (verified
	// against docs.windsurf.com/windsurf/cascade/hooks — the field is
	// command_line, NOT command).
	stdin := mustJSON(t, map[string]any{
		"agent_action_name": "pre_run_command",
		"trajectory_id":     "traj-1",
		"tool_info":         map[string]any{"command_line": "rm -rf /", "cwd": "/app"},
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

// TestWindsurfNormalizeRunCommandLegacyField: payloads carrying the
// undocumented tool_info.command field (shape predating the current docs, or
// written by an older consumer) must still normalize — command_line is
// preferred, command is the defensive fallback.
func TestWindsurfNormalizeRunCommandLegacyField(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"agent_action_name": "pre_run_command",
		"trajectory_id":     "traj-1",
		"tool_info":         map[string]any{"command": "rm -rf /"},
	})
	var hi HookInput
	normalizeAgentStdin("windsurf", stdin, &hi)

	var f toolInputFields
	json.Unmarshal(hi.ToolInput, &f)
	if f.Command != "rm -rf /" {
		t.Errorf("Command (legacy field): got %q, want 'rm -rf /'", f.Command)
	}
}

// TestWindsurfNormalizePreUserPrompt pins the documented pre_user_prompt
// payload shape (docs.windsurf.com/windsurf/cascade/hooks): the common
// trajectory_id carries the session id, tool_info.user_prompt carries the
// prompt text, and the event normalizes to SessionStart (the SessionStart
// group — skill-scan/task-resume/skill-trigger — hangs on this event; see
// buildWindsurfHooks). Without the user_prompt mapping, skill-trigger's
// coding_intent conditions would never match on windsurf.
func TestWindsurfNormalizePreUserPrompt(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"agent_action_name": "pre_user_prompt",
		"trajectory_id":     "traj-42",
		"execution_id":      "exec-7",
		"timestamp":         "2026-08-01T00:00:00Z",
		"model_name":        "Claude Sonnet 4",
		"tool_info":         map[string]any{"user_prompt": "fix the failing test"},
	})
	var hi HookInput
	normalizeAgentStdin("windsurf", stdin, &hi)

	if hi.SessionID != "traj-42" {
		t.Errorf("SessionID: got %q, want traj-42", hi.SessionID)
	}
	if hi.HookEventName != "SessionStart" {
		t.Errorf("HookEventName: got %q, want SessionStart", hi.HookEventName)
	}
	if hi.Prompt != "fix the failing test" {
		t.Errorf("Prompt: got %q, want 'fix the failing test'", hi.Prompt)
	}
}

// TestWindsurfNormalizePostCascadeResponse pins the documented
// post_cascade_response payload shape: trajectory_id is present as the session
// id and the event normalizes to Stop (the Stop group — task-verify/
// review-stop — hangs on it). The documented tool_info carries only the
// markdown response; no file/command extraction is expected.
func TestWindsurfNormalizePostCascadeResponse(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"agent_action_name": "post_cascade_response",
		"trajectory_id":     "traj-42",
		"tool_info":         map[string]any{"response": "### Planner Response\n\nDone."},
	})
	var hi HookInput
	normalizeAgentStdin("windsurf", stdin, &hi)

	if hi.SessionID != "traj-42" {
		t.Errorf("SessionID: got %q, want traj-42", hi.SessionID)
	}
	if hi.HookEventName != "Stop" {
		t.Errorf("HookEventName: got %q, want Stop", hi.HookEventName)
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

// TestWindsurfHookEvent: F2/N4 回归——windsurfHookEvent 必须映射全部 Windsurf action 到 forge
// HookEventName。Cascade 无 session_start/session_end：现接线用 pre_user_prompt→SessionStart /
// post_cascade_response→Stop（旧 session_* case 保留以兼容旧版 forge 写入的配置；
// F2 修复前这两个 case 缺失，normalize 后 HookEventName 为空，按 event 分发的 hook 如
// skill-trigger 失效成死代码）。若重构误删 case，本测试捕获。
func TestWindsurfHookEvent(t *testing.T) {
	cases := []struct {
		action string
		want   string
	}{
		{"pre_write_code", "PreToolUse"},
		{"pre_read_code", "PreToolUse"},
		{"pre_run_command", "PreToolUse"},
		{"post_write_code", "PostToolUse"},
		{"post_read_code", "PostToolUse"},
		{"post_run_command", "PostToolUse"},
		{"pre_user_prompt", "SessionStart"},
		{"post_cascade_response", "Stop"},
		{"post_cascade_response_with_transcript", "Stop"},
		// Legacy events written by older forge versions — keep normalizing.
		{"session_start", "SessionStart"},
		{"session_end", "Stop"},
		{"unknown_action", ""},
	}
	for _, c := range cases {
		if got := windsurfHookEvent(c.action); got != c.want {
			t.Errorf("windsurfHookEvent(%q) = %q, want %q", c.action, got, c.want)
		}
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
