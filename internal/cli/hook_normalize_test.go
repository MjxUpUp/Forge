package cli

import (
	"encoding/json"
	"testing"
)

// normalizeCase 是各方言 normalize 表的一行 payload→期望。want* 列为空表示
// 原逐用例测试未断言该字段（跳过）——绝不表示"断言字段为空"。preUnmarshal
// 模拟 runHook 在 normalizeAgentStdin 之前的默认 json.Unmarshal（原始 stdin
// 与 HookInput 自身 json tag 撞名时的生产时序，如 cline 的 snake_case
// tool_name）。
type normalizeCase struct {
	name         string
	payload      map[string]any
	preUnmarshal bool

	wantEvent    string
	wantSession  string
	wantTool     string
	wantCwd      string
	wantPrompt   string
	wantFilePath string
	wantContent  string
	wantCommand  string
}

// runNormalizeCases 断言一张方言表。每个非空 want* 列携带一条来自被吸收
// 逐用例测试的断言（字段、got、want）。
func runNormalizeCases(t *testing.T, agent string, cases []normalizeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdin := mustJSON(t, tc.payload)
			var hi HookInput
			if tc.preUnmarshal {
				if err := json.Unmarshal(stdin, &hi); err != nil {
					t.Fatalf("default unmarshal: %v", err)
				}
			}
			normalizeAgentStdin(agent, stdin, &hi)

			if tc.wantEvent != "" && hi.HookEventName != tc.wantEvent {
				t.Errorf("HookEventName: got %q, want %q", hi.HookEventName, tc.wantEvent)
			}
			if tc.wantSession != "" && hi.SessionID != tc.wantSession {
				t.Errorf("SessionID: got %q, want %q", hi.SessionID, tc.wantSession)
			}
			if tc.wantTool != "" && hi.ToolName != tc.wantTool {
				t.Errorf("ToolName: got %q, want %q", hi.ToolName, tc.wantTool)
			}
			if tc.wantCwd != "" && hi.Cwd != tc.wantCwd {
				t.Errorf("Cwd: got %q, want %q", hi.Cwd, tc.wantCwd)
			}
			if tc.wantPrompt != "" && hi.Prompt != tc.wantPrompt {
				t.Errorf("Prompt: got %q, want %q", hi.Prompt, tc.wantPrompt)
			}
			if tc.wantFilePath != "" || tc.wantContent != "" || tc.wantCommand != "" {
				var f toolInputFields
				if err := json.Unmarshal(hi.ToolInput, &f); err != nil {
					t.Fatalf("unmarshal normalized tool_input: %v", err)
				}
				if tc.wantFilePath != "" && f.FilePath != tc.wantFilePath {
					t.Errorf("FilePath: got %q, want %q", f.FilePath, tc.wantFilePath)
				}
				if tc.wantContent != "" && f.Content != tc.wantContent {
					t.Errorf("Content: got %q, want %q", f.Content, tc.wantContent)
				}
				if tc.wantCommand != "" && f.Command != tc.wantCommand {
					t.Errorf("Command: got %q, want %q", f.Command, tc.wantCommand)
				}
			}
		})
	}
}

// TestWindsurfNormalizeTable merges the windsurf per-payload tests; rows keep their original load-bearing comments and assertions.
//
// TestWindsurfNormalizeTable 合并 windsurf 逐 payload 测试；行保留原有关键注释
// 与断言。
func TestWindsurfNormalizeTable(t *testing.T) {
	runNormalizeCases(t, "windsurf", []normalizeCase{
		{
			// pre_write_code: the case that matters most for task-guard
			// enforcement (intercept a write before it lands).
			name: "pre_write_code",
			payload: map[string]any{
				"agent_action_name": "pre_write_code",
				"trajectory_id":     "traj-123",
				"tool_info": map[string]any{
					"file_path": "/app/main.go",
					"edits":     []map[string]any{{"new_string": "package main"}},
				},
			},
			wantEvent:    "PreToolUse",
			wantSession:  "traj-123",
			wantTool:     "Write",
			wantFilePath: "/app/main.go",
			wantContent:  "package main",
		},
		{
			// pre_run_command → Bash + command field (bash-guard needs this). The
			// payload uses the documented shape: tool_info.command_line (verified
			// against docs.windsurf.com/windsurf/cascade/hooks — the field is
			// command_line, NOT command).
			name: "pre_run_command documented command_line",
			payload: map[string]any{
				"agent_action_name": "pre_run_command",
				"trajectory_id":     "traj-1",
				"tool_info":         map[string]any{"command_line": "rm -rf /", "cwd": "/app"},
			},
			wantEvent:   "PreToolUse",
			wantTool:    "Bash",
			wantCommand: "rm -rf /",
		},
		{
			// Payloads carrying the undocumented tool_info.command field (shape
			// predating the current docs, or written by an older consumer) must
			// still normalize — command_line is preferred, command is the
			// defensive fallback.
			name: "pre_run_command legacy command field",
			payload: map[string]any{
				"agent_action_name": "pre_run_command",
				"trajectory_id":     "traj-1",
				"tool_info":         map[string]any{"command": "rm -rf /"},
			},
			wantCommand: "rm -rf /",
		},
		{
			// Documented pre_user_prompt shape: the common trajectory_id carries
			// the session id, tool_info.user_prompt carries the prompt text, and
			// the event normalizes to SessionStart (the SessionStart group —
			// skill-scan/task-resume/skill-trigger — hangs on this event; see
			// buildWindsurfHooks). Without the user_prompt mapping, skill-trigger's
			// coding_intent conditions would never match on windsurf.
			name: "pre_user_prompt",
			payload: map[string]any{
				"agent_action_name": "pre_user_prompt",
				"trajectory_id":     "traj-42",
				"execution_id":      "exec-7",
				"timestamp":         "2026-08-01T00:00:00Z",
				"model_name":        "Claude Sonnet 4",
				"tool_info":         map[string]any{"user_prompt": "fix the failing test"},
			},
			wantEvent:   "SessionStart",
			wantSession: "traj-42",
			wantPrompt:  "fix the failing test",
		},
		{
			// Documented post_cascade_response shape: trajectory_id is present as
			// the session id and the event normalizes to Stop (the Stop group —
			// task-verify/review-stop — hangs on it). The documented tool_info
			// carries only the markdown response; no file/command extraction is
			// expected.
			name: "post_cascade_response",
			payload: map[string]any{
				"agent_action_name": "post_cascade_response",
				"trajectory_id":     "traj-42",
				"tool_info":         map[string]any{"response": "### Planner Response\n\nDone."},
			},
			wantEvent:   "Stop",
			wantSession: "traj-42",
		},
		{
			name: "post_read_code",
			payload: map[string]any{
				"agent_action_name": "post_read_code",
				"trajectory_id":     "traj-1",
				"tool_info":         map[string]any{"file_path": "/app/x.go"},
			},
			wantEvent: "PostToolUse",
			wantTool:  "Read",
		},
		{
			// If an agent ever sends Claude-shape stdin despite FORGE_HOOK_AGENT
			// being set, we must not clobber it. The existing-existence guards in
			// windsurfNormalize enforce this.
			name: "claude-shape stdin preserved",
			payload: map[string]any{
				"session_id":      "real-cc-session",
				"hook_event_name": "PreToolUse",
				"tool_name":       "Edit",
				"tool_input":      map[string]any{"file_path": "/kept.go"},
			},
			preUnmarshal: true,
			wantSession:  "real-cc-session",
			wantTool:     "Edit",
		},
	})
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

// TestReasonixNormalizeTable merges the reasonix per-payload tests. reasonix PreToolUse uses camelCase fields ({event, sessionId, cwd, toolName, toolArgs}) + snake_case tool name + path (not file_path) — without normalization, tool_name/file_path parse empty and the hooks fire but fail open (the original "reasonix rarely follows Forge" root cause). toolArgs.path must alias to file_path so FORGE_FILE_PATH resolves; new_string passes through (assertion-check reads tool_input.new_string, which CC Edit also uses — CC Edit has no `content` field).
//
// TestReasonixNormalizeTable 合并 reasonix 逐 payload 测试。reasonix PreToolUse
// 用 camelCase 字段（{event, sessionId, cwd, toolName, toolArgs}）+ snake_case
// 工具名 + path（非 file_path）——不归一化则 tool_name/file_path 解析为空、
// hook 触发却 fail open（"reasonix 很少跟随 Forge"的原始根因）。toolArgs.path
// 须别名到 file_path 供 FORGE_FILE_PATH 解析；new_string 透传（assertion-check
// 读 tool_input.new_string，CC Edit 也用它——CC Edit 无 `content` 字段）。
func TestReasonixNormalizeTable(t *testing.T) {
	runNormalizeCases(t, "reasonix", []normalizeCase{
		{
			// edit_file PreToolUse——read-before-edit / task-guard enforce 所系。
			name: "edit_file",
			payload: map[string]any{
				"event":     "PreToolUse",
				"sessionId": "rsx-123",
				"cwd":       `E:\AgentFare`,
				"toolName":  "edit_file",
				"toolArgs": map[string]any{
					"path":       `E:\AgentFare\main.go`,
					"old_string": "a",
					"new_string": "b",
				},
			},
			wantEvent:    "PreToolUse",
			wantSession:  "rsx-123",
			wantCwd:      `E:\AgentFare`,
			wantTool:     "Edit",
			wantFilePath: `E:\AgentFare\main.go`,
		},
		{
			// bash → Bash + command passthrough (bash-guard / hazard-guard).
			name: "bash",
			payload: map[string]any{
				"event":     "PreToolUse",
				"sessionId": "rsx-1",
				"cwd":       "/app",
				"toolName":  "bash",
				"toolArgs":  map[string]any{"command": "rm -rf /tmp/x"},
			},
			wantTool:    "Bash",
			wantCommand: "rm -rf /tmp/x",
		},
		{
			// read_file → Read is load-bearing — hook.go records a read in the
			// reads-log only when ToolName == "Read". Without this map,
			// read-before-edit would false-positive (every edit looks unread)
			// on reasonix.
			name: "read_file",
			payload: map[string]any{
				"event":     "PostToolUse",
				"sessionId": "rsx-1",
				"cwd":       "/app",
				"toolName":  "read_file",
				"toolArgs":  map[string]any{"path": "/app/main.go"},
			},
			wantEvent: "PostToolUse",
			wantTool:  "Read",
		},
		{
			// The session events carry {event, sessionId, cwd} in camelCase too —
			// HookEventName/SessionID would stay empty under default unmarshal
			// (event≠hook_event_name, sessionId≠session_id), so reasonixNormalize
			// fills them.
			name: "session start",
			payload: map[string]any{
				"event":     "SessionStart",
				"sessionId": "rsx-sess-9",
				"cwd":       "/app",
				"source":    "startup",
			},
			wantEvent:   "SessionStart",
			wantSession: "rsx-sess-9",
		},
	})
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

// TestClineNormalizeTable merges the cline per-payload tests. cline's write_to_file with the documented base fields plus a `parameters` payload is the case read-before-edit / task-guard enforcement hangs on: workspaceRoots[0]→Cwd, taskId→SessionID, parameters.path aliased to file_path, write_to_file→Write.
//
// TestClineNormalizeTable 合并 cline 逐 payload 测试。cline 的 write_to_file
// 携带文档化基础字段加 `parameters` payload——read-before-edit / task-guard
// enforce 所系的场景：workspaceRoots[0]→Cwd、taskId→SessionID、parameters.path
// 别名到 file_path、write_to_file→Write。
func TestClineNormalizeTable(t *testing.T) {
	runNormalizeCases(t, "cline", []normalizeCase{
		{
			// write_to_file PreToolUse，文档化形状。
			name: "write_to_file",
			payload: map[string]any{
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
			},
			wantEvent:    "PreToolUse",
			wantSession:  "cline-task-7",
			wantCwd:      "/home/u/proj",
			wantTool:     "Write",
			wantFilePath: "/home/u/proj/main.go",
		},
		{
			// 工具 payload 的确切字段名仅部分文档化——toolName/toolInput
			// （camelCase）须与 tool/parameters 同等解析，且 execute_command 须
			// 映射到 Bash、command 原样透传（bash-guard / hazard-guard 的输入）。
			name: "camelCase toolName/toolInput candidates",
			payload: map[string]any{
				"hookName":       "PreToolUse",
				"taskId":         "cline-1",
				"workspaceRoots": []string{"/app"},
				"toolName":       "execute_command",
				"toolInput":      map[string]any{"command": "rm -rf /tmp/x"},
			},
			wantTool:    "Bash",
			wantCommand: "rm -rf /tmp/x",
		},
		{
			// TaskStart 挂着 SessionStart 组（见 clineEventMappings）——
			// clineNormalize 必须把事件映射回 "SessionStart"，否则每个会话级
			// hook（恰以该名分发）静默永不触发。这是关键的事件翻译。
			name: "TaskStart maps to SessionStart",
			payload: map[string]any{
				"hookName":       "TaskStart",
				"taskId":         "cline-sess-1",
				"workspaceRoots": []string{"/app"},
			},
			wantEvent: "SessionStart",
		},
		{
			// 守卫 ToolName 的无条件覆盖。cline 的 snake_case `tool_name` 字段
			// （某版本发送时）与 HookInput 自身的 json tag 撞名，默认 unmarshal
			// 会在 normalizer 之前把原始 cline 名填进 ToolName——填空策略会保留
			// "write_to_file" 而跳过映射，恰是 normalizer 要防的 fail-open 形态。
			name: "overrides default-unmarshal snake tool_name",
			payload: map[string]any{
				"hookName":  "PreToolUse",
				"taskId":    "cline-2",
				"tool_name": "write_to_file",
				"tool_input": map[string]any{
					"path": "/app/main.go",
				},
			},
			preUnmarshal: true,
			wantTool:     "Write",
			wantFilePath: "/app/main.go",
		},
		{
			// prompt 候选（prompt/userPrompt/question——确切名仅部分文档化）落入
			// Prompt，供 resume-reinject / skill-trigger 读取。
			name: "UserPromptSubmit prompt",
			payload: map[string]any{
				"hookName":       "UserPromptSubmit",
				"taskId":         "cline-3",
				"workspaceRoots": []string{"/app"},
				"prompt":         "continue the task",
			},
			wantEvent:  "UserPromptSubmit",
			wantPrompt: "continue the task",
		},
	})
}

// TestClineToCCToolName pins the snake_case → PascalCase map for cline's documented tool roster; unknown names pass through unchanged (forward-compat — a future cline tool must not silently match nothing forge dispatches on).
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

// TestClineNormalize_EmptyAndGarbage: empty stdin and non-JSON garbage must not panic and must leave the input untouched (hooks degrade to no-payload behavior).
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
