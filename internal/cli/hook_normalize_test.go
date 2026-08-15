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

// TestReasonixNormalizeEditFile: reasonix PreToolUse for edit_file — camelCase fields
// ({event, sessionId, cwd, toolName, toolArgs}) + snake_case tool name + path (not file_path).
// This is the case that matters most for read-before-edit / task-guard enforcement: without
// normalization, tool_name/file_path parse empty and the hooks fire but fail open (the original
// "reasonix rarely follows Forge" root cause). toolArgs.path must alias to file_path so
// FORGE_FILE_PATH resolves; new_string passes through (assertion-check reads tool_input.new_string,
// which CC Edit also uses — CC Edit has no `content` field).
func TestReasonixNormalizeEditFile(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"event":     "PreToolUse",
		"sessionId": "rsx-123",
		"cwd":       `E:\AgentFare`,
		"toolName":  "edit_file",
		"toolArgs": map[string]any{
			"path":       `E:\AgentFare\main.go`,
			"old_string": "a",
			"new_string": "b",
		},
	})
	var hi HookInput
	normalizeAgentStdin("reasonix", stdin, &hi)

	if hi.HookEventName != "PreToolUse" {
		t.Errorf("HookEventName: got %q, want PreToolUse", hi.HookEventName)
	}
	if hi.SessionID != "rsx-123" {
		t.Errorf("SessionID: got %q, want rsx-123 (sessionId→SessionID)", hi.SessionID)
	}
	if hi.Cwd != `E:\AgentFare` {
		t.Errorf("Cwd: got %q, want E:\\AgentFare", hi.Cwd)
	}
	if hi.ToolName != "Edit" {
		t.Errorf("ToolName: got %q, want Edit (edit_file→Edit)", hi.ToolName)
	}
	var f toolInputFields
	if err := json.Unmarshal(hi.ToolInput, &f); err != nil {
		t.Fatalf("unmarshal normalized tool_input: %v", err)
	}
	if f.FilePath != `E:\AgentFare\main.go` {
		t.Errorf("FilePath: got %q, want toolArgs.path aliased to file_path", f.FilePath)
	}
}

// TestReasonixNormalizeBash: bash → Bash + command passthrough (bash-guard / hazard-guard).
func TestReasonixNormalizeBash(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"event":     "PreToolUse",
		"sessionId": "rsx-1",
		"cwd":       "/app",
		"toolName":  "bash",
		"toolArgs":  map[string]any{"command": "rm -rf /tmp/x"},
	})
	var hi HookInput
	normalizeAgentStdin("reasonix", stdin, &hi)

	if hi.ToolName != "Bash" {
		t.Errorf("ToolName: got %q, want Bash", hi.ToolName)
	}
	var f toolInputFields
	json.Unmarshal(hi.ToolInput, &f)
	if f.Command != "rm -rf /tmp/x" {
		t.Errorf("Command: got %q, want 'rm -rf /tmp/x' (toolArgs.command passthrough)", f.Command)
	}
}

// TestReasonixNormalizeRead: read_file → Read is load-bearing — hook.go records a read in the
// reads-log only when ToolName == "Read". Without this map, read-before-edit would false-positive
// (every edit looks unread) on reasonix.
func TestReasonixNormalizeRead(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"event":     "PostToolUse",
		"sessionId": "rsx-1",
		"cwd":       "/app",
		"toolName":  "read_file",
		"toolArgs":  map[string]any{"path": "/app/main.go"},
	})
	var hi HookInput
	normalizeAgentStdin("reasonix", stdin, &hi)

	if hi.ToolName != "Read" {
		t.Errorf("ToolName: got %q, want Read (read_file→Read, load-bearing for reads-log)", hi.ToolName)
	}
	if hi.HookEventName != "PostToolUse" {
		t.Errorf("HookEventName: got %q, want PostToolUse", hi.HookEventName)
	}
}

// TestReasonixNormalizeSessionStart: the session events carry {event, sessionId, cwd} in
// camelCase too — HookEventName/SessionID would stay empty under default unmarshal (event≠
// hook_event_name, sessionId≠session_id), so reasonixNormalize fills them.
func TestReasonixNormalizeSessionStart(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"event":     "SessionStart",
		"sessionId": "rsx-sess-9",
		"cwd":       "/app",
		"source":    "startup",
	})
	var hi HookInput
	normalizeAgentStdin("reasonix", stdin, &hi)

	if hi.HookEventName != "SessionStart" {
		t.Errorf("HookEventName: got %q, want SessionStart (event→HookEventName)", hi.HookEventName)
	}
	if hi.SessionID != "rsx-sess-9" {
		t.Errorf("SessionID: got %q, want rsx-sess-9", hi.SessionID)
	}
}

// TestReasonixToCCToolName pins the snake_case → PascalCase map for every tool in reasonix's
// [sandbox] roster. A future reasonix tool (or a rename) that forge doesn't map passes through
// unchanged rather than silently matching nothing.
func TestReasonixToCCToolName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"write_file", "Write"},
		{"edit_file", "Edit"},
		{"multi_edit", "Edit"},
		{"move_file", "Edit"},
		{"bash", "Bash"},
		{"read_file", "Read"},
		{"unknown_tool", "unknown_tool"}, // passthrough (forward-compat)
		{"", ""},
	}
	for _, c := range cases {
		if got := reasonixToCCToolName(c.in); got != c.want {
			t.Errorf("reasonixToCCToolName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestClineNormalize_WriteToFile: cline PreToolUse for write_to_file with the
// documented base fields plus a `parameters` payload — the case read-before-edit /
// task-guard enforcement hangs on. workspaceRoots[0]→Cwd, taskId→SessionID,
// parameters.path aliased to file_path, write_to_file→Write.
//
// TestClineNormalize_WriteToFile：cline 的 write_to_file PreToolUse，携带文档化基础
// 字段加 `parameters` payload——read-before-edit / task-guard enforce 所系的场景。
// workspaceRoots[0]→Cwd、taskId→SessionID、parameters.path 别名到 file_path、
// write_to_file→Write。
func TestClineNormalize_WriteToFile(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"clineVersion":   "3.36.0",
		"hookName":       "PreToolUse",
		"taskId":         "cline-task-7",
		"workspaceRoots": []string{"/home/u/proj"},
		"userId":         "u1",
		"tool":           "write_to_file",
		"parameters": map[string]any{
			"path":    "/home/u/proj/main.go",
			"content": "package main",
		},
	})
	var hi HookInput
	normalizeAgentStdin("cline", stdin, &hi)

	if hi.HookEventName != "PreToolUse" {
		t.Errorf("HookEventName: got %q, want PreToolUse (hookName→HookEventName)", hi.HookEventName)
	}
	if hi.SessionID != "cline-task-7" {
		t.Errorf("SessionID: got %q, want cline-task-7 (taskId→SessionID)", hi.SessionID)
	}
	if hi.Cwd != "/home/u/proj" {
		t.Errorf("Cwd: got %q, want /home/u/proj (workspaceRoots[0]→Cwd)", hi.Cwd)
	}
	if hi.ToolName != "Write" {
		t.Errorf("ToolName: got %q, want Write (write_to_file→Write)", hi.ToolName)
	}
	var f toolInputFields
	if err := json.Unmarshal(hi.ToolInput, &f); err != nil {
		t.Fatalf("unmarshal normalized tool_input: %v", err)
	}
	if f.FilePath != "/home/u/proj/main.go" {
		t.Errorf("FilePath: got %q, want parameters.path aliased to file_path", f.FilePath)
	}
}

// TestClineNormalize_CamelCaseCandidates: the tool payload's exact field names are
// only partially documented — toolName/toolInput (camelCase) must resolve identically
// to tool/parameters, and execute_command must map to Bash with command passthrough
// (bash-guard / hazard-guard input).
//
// TestClineNormalize_CamelCaseCandidates：工具 payload 的确切字段名仅部分文档化——
// toolName/toolInput（camelCase）须与 tool/parameters 同等解析，且 execute_command
// 须映射到 Bash、command 原样透传（bash-guard / hazard-guard 的输入）。
func TestClineNormalize_CamelCaseCandidates(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"hookName":       "PreToolUse",
		"taskId":         "cline-1",
		"workspaceRoots": []string{"/app"},
		"toolName":       "execute_command",
		"toolInput":      map[string]any{"command": "rm -rf /tmp/x"},
	})
	var hi HookInput
	normalizeAgentStdin("cline", stdin, &hi)

	if hi.ToolName != "Bash" {
		t.Errorf("ToolName: got %q, want Bash (execute_command→Bash)", hi.ToolName)
	}
	var f toolInputFields
	json.Unmarshal(hi.ToolInput, &f)
	if f.Command != "rm -rf /tmp/x" {
		t.Errorf("Command: got %q, want 'rm -rf /tmp/x' (toolInput.command passthrough)", f.Command)
	}
}

// TestClineNormalize_TaskStartMapsToSessionStart: the SessionStart group hangs on
// cline's TaskStart (see clineEventMappings) — clineNormalize must map the event back
// to "SessionStart" or every session-scoped hook (which dispatches on exactly that
// name) would silently never fire. This is the load-bearing event translation.
//
// TestClineNormalize_TaskStartMapsToSessionStart：SessionStart 组挂在 cline 的
// TaskStart 上（见 clineEventMappings）——clineNormalize 必须把事件映射回
// "SessionStart"，否则每个会话级 hook（恰以该名分发）静默永不触发。这是关键的
// 事件翻译。
func TestClineNormalize_TaskStartMapsToSessionStart(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"hookName":       "TaskStart",
		"taskId":         "cline-sess-1",
		"workspaceRoots": []string{"/app"},
	})
	var hi HookInput
	normalizeAgentStdin("cline", stdin, &hi)

	if hi.HookEventName != "SessionStart" {
		t.Errorf("HookEventName: got %q, want SessionStart (TaskStart carries the SessionStart group)", hi.HookEventName)
	}
}

// TestClineNormalize_OverridesDefaultUnmarshalSnakeToolName: guards the UNCONDITIONAL
// ToolName override. cline's snake_case `tool_name` field (when a version sends it)
// collides with HookInput's own json tag, so the default unmarshal fills ToolName with
// the raw cline name BEFORE the normalizer runs — a fill-empty policy would preserve
// "write_to_file" and skip the mapping, the exact fail-open shape the normalizer
// exists to prevent.
//
// TestClineNormalize_OverridesDefaultUnmarshalSnakeToolName：守卫 ToolName 的无条件
// 覆盖。cline 的 snake_case `tool_name` 字段（某版本发送时）与 HookInput 自身的
// json tag 撞名，默认 unmarshal 会在 normalizer 之前把原始 cline 名填进 ToolName——
// 填空策略会保留 "write_to_file" 而跳过映射，恰是 normalizer 要防的 fail-open 形态。
func TestClineNormalize_OverridesDefaultUnmarshalSnakeToolName(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"hookName":  "PreToolUse",
		"taskId":    "cline-2",
		"tool_name": "write_to_file",
		"tool_input": map[string]any{
			"path": "/app/main.go",
		},
	})
	// Simulate runHook's default unmarshal first (the production sequence for cline).
	//
	// 先模拟 runHook 的默认 unmarshal（cline 的生产时序）。
	var hi HookInput
	if err := json.Unmarshal(stdin, &hi); err != nil {
		t.Fatalf("default unmarshal: %v", err)
	}
	normalizeAgentStdin("cline", stdin, &hi)

	if hi.ToolName != "Write" {
		t.Errorf("ToolName: got %q, want Write — normalizer must override the raw snake_case name the default unmarshal filled", hi.ToolName)
	}
	var f toolInputFields
	if err := json.Unmarshal(hi.ToolInput, &f); err != nil {
		t.Fatalf("unmarshal normalized tool_input: %v", err)
	}
	if f.FilePath != "/app/main.go" {
		t.Errorf("FilePath: got %q, want tool_input.path aliased to file_path", f.FilePath)
	}
}

// TestClineNormalize_UserPromptSubmitPrompt: the prompt candidates (prompt/userPrompt/
// question — exact name partially documented) land in Prompt, which resume-reinject /
// skill-trigger read.
//
// TestClineNormalize_UserPromptSubmitPrompt：prompt 候选（prompt/userPrompt/question
// ——确切名仅部分文档化）落入 Prompt，供 resume-reinject / skill-trigger 读取。
func TestClineNormalize_UserPromptSubmitPrompt(t *testing.T) {
	stdin := mustJSON(t, map[string]any{
		"hookName":       "UserPromptSubmit",
		"taskId":         "cline-3",
		"workspaceRoots": []string{"/app"},
		"prompt":         "continue the task",
	})
	var hi HookInput
	normalizeAgentStdin("cline", stdin, &hi)

	if hi.HookEventName != "UserPromptSubmit" {
		t.Errorf("HookEventName: got %q, want UserPromptSubmit", hi.HookEventName)
	}
	if hi.Prompt != "continue the task" {
		t.Errorf("Prompt: got %q, want 'continue the task'", hi.Prompt)
	}
}

// TestClineToCCToolName pins the snake_case → PascalCase map for cline's documented
// tool roster; unknown names pass through unchanged (forward-compat — a future cline
// tool must not silently match nothing forge dispatches on).
//
// TestClineToCCToolName 钉死 cline 文档化工具名册的 snake_case → PascalCase 映射；
// 未知名原样透传（向前兼容——未来的 cline 工具不得静默匹配不到 forge 的分发名）。
func TestClineToCCToolName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"write_to_file", "Write"},
		{"insert_content", "Edit"},
		{"search_and_replace", "Edit"},
		{"read_file", "Read"},
		{"execute_command", "Bash"},
		{"future_tool", "future_tool"}, // passthrough (forward-compat)
		{"", ""},
	}
	for _, c := range cases {
		if got := clineToCCToolName(c.in); got != c.want {
			t.Errorf("clineToCCToolName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestClineNormalize_EmptyAndGarbage: empty stdin and non-JSON garbage must not panic
// and must leave the input untouched (hooks degrade to no-payload behavior).
//
// TestClineNormalize_EmptyAndGarbage：空 stdin 与非 JSON 垃圾输入不得 panic、不得
// 改动输入（hook 退化到无 payload 行为）。
func TestClineNormalize_EmptyAndGarbage(t *testing.T) {
	var hi HookInput
	normalizeAgentStdin("cline", nil, &hi)
	normalizeAgentStdin("cline", []byte("not json"), &hi)
	if hi.ToolName != "" || hi.SessionID != "" || hi.HookEventName != "" {
		t.Errorf("garbage stdin must leave HookInput empty, got %+v", hi)
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
