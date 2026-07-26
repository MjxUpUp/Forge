package cli

import "encoding/json"

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
	}
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
	}
	return ""
}

// (copilotNormalize removed: refactor-data-home locked in five specialized agents, copilot is no longer adapted.
//  If you need to restore it in the future, implement per docs.github.com/en/copilot/reference/hooks-reference.)
//
// (copilotNormalize 删除：refactor-data-home 锁定 5 家专精，copilot 不再适配。
//  若未来需恢复，按 docs.github.com/en/copilot/reference/hooks-reference 实现。)
