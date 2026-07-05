package cli

import "encoding/json"

// normalizeAgentStdin translates non-Claude-Code agent hook stdin into the
// HookInput shape forge extracts from. FORGE_HOOK_AGENT — set by each agent's
// hook command (e.g. `FORGE_HOOK_AGENT=windsurf forge hook task-guard`) —
// selects the dialect. Without this, agent stdin parses to empty
// file_path/command and intercept hooks (task-guard, bash-guard) fail open.
//
// opencode and pi are code-based: their TS extensions build Claude-shape stdin
// directly before spawning forge, so they need no normalizer here.
func normalizeAgentStdin(agent string, stdinData []byte, hookInput *HookInput) {
	switch agent {
	case "windsurf":
		windsurfNormalize(stdinData, hookInput)
	}
}

// windsurfNormalize maps Windsurf Cascade's hook stdin onto HookInput.
//
// Windsurf schema (per docs.windsurf.com/windsurf/cascade/hooks):
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
// We rebuild tool_input as Claude's {file_path, content, command} so the
// existing toolInputFields extraction picks it up unchanged.
func windsurfNormalize(stdinData []byte, hookInput *HookInput) {
	var w struct {
		AgentActionName string `json:"agent_action_name"`
		TrajectoryID    string `json:"trajectory_id"`
		ToolInfo struct {
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

// windsurfToolName maps a Windsurf event to the Claude Code tool name forge
// keys on. Windsurf doesn't split Write vs Edit at the event level (both are
// *_write_code), so both map to Write — file_path extraction is what matters
// for enforcement, not the Write/Edit distinction.
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

// (copilotNormalize 删除：refactor-data-home 锁定 5 家专精，copilot 不再适配。
//  若未来需恢复，按 docs.github.com/en/copilot/reference/hooks-reference 实现。)
