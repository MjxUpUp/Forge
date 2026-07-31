package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

// normalizeAgentStdin translates the hook stdin of non-Claude-Code agents into the HookInput shape that forge extracts.
// FORGE_HOOK_AGENT is set by each agent's hook command (e.g. `FORGE_HOOK_AGENT=windsurf forge hook task-guard`) to select the dialect.
// Without this step, agent stdin would parse to empty file_path/command, and blocking hooks (task-guard,
// bash-guard) would fail open.
//
// normalizeAgentStdin 把非 Claude Code agent 的 hook stdin 翻译成 forge 抽取
// 所用的 HookInput 形状。FORGE_HOOK_AGENT 由各 agent 的 hook 命令设置（例如
// `FORGE_HOOK_AGENT=windsurf forge hook task-guard`），用以选择方言。不做这步，
// agent stdin 会解析出空的 file_path/command，拦截类 hook（task-guard、
// bash-guard）会 fail open。
//
// opencode and pi are code-based: their TS extensions directly construct Claude-shape stdin before spawning forge,
// so no normalizer is needed here.
//
// opencode 和 pi 是 code-based：它们的 TS 扩展在 spawn forge 前就直接构造
// Claude-shape stdin，故此处无需 normalizer。
func normalizeAgentStdin(agent string, stdinData []byte, hookInput *HookInput) {
	switch agent {
	case "windsurf":
		windsurfNormalize(stdinData, hookInput)
	case "kimi":
		kimiNormalize(stdinData, hookInput)
	}
}

// kimiNormalize parses kimi's hook stdin into HookInput. kimi's payload is Claude-shaped
// ({hook_event_name, session_id, cwd, tool_name, tool_input} — verified against kimi 0.31.0)
// with three divergences:
//   - UserPromptSubmit's prompt is a content-block array [{"type":"text","text":"..."}],
//     not a plain string — a direct unmarshal into HookInput would type-error on it.
//   - PostToolUse carries tool_output (plain string) instead of Claude's tool_response.
//   - File-class tools (Read/Write/Edit) use tool_input.path (project-root-relative)
//     instead of Claude's tool_input.file_path — remapped by remapKimiToolInput.
//
// runHook calls this INSTEAD of the default unmarshal for kimi, so it fills every field.
//
// kimiNormalize 把 kimi 的 hook stdin 解析进 HookInput。kimi 的 payload 与 Claude 同构
// （{hook_event_name, session_id, cwd, tool_name, tool_input}——已对 kimi 0.31.0 实测），
// 三处差异：
//   - UserPromptSubmit 的 prompt 是 content-block 数组 [{"type":"text","text":"..."}]，
//     不是纯字符串——直接 unmarshal 进 HookInput 会在该字段类型错误。
//   - PostToolUse 带 tool_output（纯字符串）而非 Claude 的 tool_response。
//   - 文件类工具（Read/Write/Edit）用 tool_input.path（项目根相对路径）而非 Claude 的
//     tool_input.file_path——由 remapKimiToolInput 重映射。
//
// runHook 对 kimi 用本函数替代默认 unmarshal，故此处填充全部字段。
func kimiNormalize(stdinData []byte, hookInput *HookInput) {
	if len(stdinData) == 0 {
		return
	}
	var k struct {
		SessionID     string          `json:"session_id"`
		HookEventName string          `json:"hook_event_name"`
		ToolName      string          `json:"tool_name"`
		ToolInput     json.RawMessage `json:"tool_input"`
		ToolOutput    json.RawMessage `json:"tool_output"`
		Prompt        []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"prompt"`
	}
	if err := json.Unmarshal(stdinData, &k); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: kimi hook stdin JSON parse failed: %v\n", err)
		return
	}
	hookInput.SessionID = k.SessionID
	hookInput.HookEventName = k.HookEventName
	hookInput.ToolName = k.ToolName
	hookInput.ToolInput = remapKimiToolInput(k.ToolInput)
	hookInput.ToolOutput = k.ToolOutput
	for _, block := range k.Prompt {
		if block.Type != "text" || block.Text == "" {
			continue
		}
		if hookInput.Prompt != "" {
			hookInput.Prompt += "\n"
		}
		hookInput.Prompt += block.Text
	}
}

// remapKimiToolInput aliases kimi's `path` field to Claude's `file_path`. kimi's
// file-class tools (Read/Write/Edit) carry {"path": "main.go", ...} — no file_path —
// verified against kimi-code 0.31.0. Without the alias FORGE_FILE_PATH would be empty and
// the path-based blocking hooks (read-before-edit, task-guard's .forge/* self-protection)
// would fail open. The value is project-root-relative; toRelPath passes relative input
// through unchanged, which already matches the hooks' glob expectations.
//
// remapKimiToolInput 把 kimi 的 `path` 字段别名到 Claude 的 `file_path`。kimi 的
// 文件类工具（Read/Write/Edit）携带 {"path": "main.go", ...}——没有 file_path——
// 已对 kimi-code 0.31.0 实测。不做别名 FORGE_FILE_PATH 会为空，基于路径的拦截类
// hook（read-before-edit、task-guard 的 .forge/* 自保护）会 fail-open。其值是
// 项目根相对路径；toRelPath 对相对输入原样透传，正好符合 hook 的 glob 预期。
func remapKimiToolInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["file_path"]; !ok {
		if p, ok := m["path"]; ok {
			m["file_path"] = p
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// windsurfNormalize maps the Windsurf Cascade hook stdin to HookInput.
//
// windsurfNormalize 把 Windsurf Cascade 的 hook stdin 映射到 HookInput。
//
// Windsurf schema (see docs.windsurf.com/windsurf/cascade/hooks):
//
// Windsurf schema（见 docs.windsurf.com/windsurf/cascade/hooks）：
//
//	{
//	  "agent_action_name": "pre_write_code",
//	  "trajectory_id":     "<session id>",
//	  "tool_info": {
//	    "file_path": "...",
//	    "command":   "...",                 // pre_run_command
//	    "edits":     [{"old_string","new_string"}]  // *_write_code
//	  }
//	}
//
// 这里把 tool_input 重建成 Claude 的 {file_path, content, command}，让既有
// toolInputFields 抽取逻辑无需改动即可拿到。
func windsurfNormalize(stdinData []byte, hookInput *HookInput) {
	var w struct {
		AgentActionName string `json:"agent_action_name"`
		TrajectoryID    string `json:"trajectory_id"`
		ToolInfo        struct {
			FilePath string `json:"file_path"`
			Command  string `json:"command"`
			Edits    []struct {
				NewString string `json:"new_string"`
			} `json:"edits"`
		} `json:"tool_info"`
	}
	if err := json.Unmarshal(stdinData, &w); err != nil {
		return
	}
	if hookInput.SessionID == "" {
		hookInput.SessionID = w.TrajectoryID
	}
	if hookInput.ToolName == "" {
		hookInput.ToolName = windsurfToolName(w.AgentActionName)
	}
	if hookInput.HookEventName == "" {
		hookInput.HookEventName = windsurfHookEvent(w.AgentActionName)
	}
	if len(hookInput.ToolInput) == 0 {
		ti := map[string]string{}
		if w.ToolInfo.FilePath != "" {
			ti["file_path"] = w.ToolInfo.FilePath
		}
		if w.ToolInfo.Command != "" {
			ti["command"] = w.ToolInfo.Command
		}
		if len(w.ToolInfo.Edits) > 0 && w.ToolInfo.Edits[0].NewString != "" {
			ti["content"] = w.ToolInfo.Edits[0].NewString
		}
		if len(ti) > 0 {
			if b, err := json.Marshal(ti); err == nil {
				hookInput.ToolInput = b
			}
		}
	}
}

// windsurfToolName maps Windsurf events to the Claude Code tool name that forge uses for dispatch.
// Windsurf does not distinguish Write from Edit at the event level (both are *_write_code), so both map to Write —
// for enforcement the key is file_path extraction, not the Write/Edit distinction.
//
// windsurfToolName 把 Windsurf 事件映射到 forge 据以分发的 Claude Code 工具
// 名。Windsurf 在事件层并不区分 Write 和 Edit（二者都是 *_write_code），故都
// 映射到 Write——对 enforcement 而言关键是 file_path 抽取，而非 Write/Edit 的
// 区分。
func windsurfToolName(action string) string {
	switch action {
	case "pre_write_code", "post_write_code":
		return "Write"
	case "pre_read_code", "post_read_code":
		return "Read"
	case "pre_run_command", "post_run_command":
		return "Bash"
	}
	return ""
}

func windsurfHookEvent(action string) string {
	switch action {
	case "pre_write_code", "pre_read_code", "pre_run_command":
		return "PreToolUse"
	case "post_write_code", "post_read_code", "post_run_command":
		return "PostToolUse"
	case "session_start":
		return "SessionStart"
	case "session_end":
		return "Stop"
	}
	return ""
}

// (copilotNormalize removed: refactor-data-home locked in five specialized agents, copilot is no longer adapted.
//  If you need to restore it in the future, implement per docs.github.com/en/copilot/reference/hooks-reference.)
//
// (copilotNormalize 删除：refactor-data-home 锁定 5 家专精，copilot 不再适配。
//  若未来需恢复，按 docs.github.com/en/copilot/reference/hooks-reference 实现。)
