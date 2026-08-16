package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
// opencode is code-based: their TS extensions directly construct Claude-shape stdin before spawning forge,
// so no normalizer is needed here.
//
// opencode 是 code-based：它们的 TS 扩展在 spawn forge 前就直接构造
// Claude-shape stdin，故此处无需 normalizer。
func normalizeAgentStdin(agent string, stdinData []byte, hookInput *HookInput) {
	switch agent {
	case "windsurf":
		windsurfNormalize(stdinData, hookInput)
	case "kimi":
		kimiNormalize(stdinData, hookInput)
	case "reasonix":
		reasonixNormalize(stdinData, hookInput)
	case "cline":
		clineNormalize(stdinData, hookInput)
	}
}

// clineNormalize maps cline's hook stdin (v3.36+ hooks) to HookInput. Documented base
// fields ({clineVersion, hookName, timestamp, taskId, workspaceRoots, userId} plus a
// tool name/parameters object whose exact field names are only partially documented)
// diverge from Claude's shape in four load-bearing ways:
//   - Session identity is taskId (no session_id); project root is workspaceRoots[0]
//     (no cwd field).
//   - Event name is hookName, whose value is the SCRIPT name — PreToolUse/
//     PostToolUse/UserPromptSubmit match Claude, but the SessionStart group hangs on
//     cline's TaskStart, so the event is mapped back (clineHookEvent) or the
//     session-scoped hooks' dispatch (keyed on "SessionStart") would never fire.
//   - Tool names are cline's snake_case roster (write_to_file/insert_content/
//     search_and_replace/read_file/execute_command) — mapped to the Claude names
//     forge dispatches on (clineToCCToolName), same call reasonix makes.
//   - The tool payload's exact key is undocumented: tool_name/toolName/tool and
//     tool_input/toolInput/parameters are all accepted as candidates, first
//     non-empty wins. The payload's file tools carry {path, ...} — aliased to
//     file_path via remapKimiToolInput so FORGE_FILE_PATH resolves.
//
// ToolName and ToolInput are overridden UNCONDITIONALLY when a candidate is present
// (unlike reasonix's fill-empty): the default unmarshal already fills ToolName from a
// same-named snake_case tool_name field — fill-empty would then preserve the raw
// cline name ("write_to_file") and skip the mapping, exactly the fail-open shape the
// normalizer exists to prevent. Cline never sends a Claude-shape payload, so the
// override is safe. Field names beyond the documented base are best-effort
// candidates: an undocumented rename leaves the field empty and the hook degrades to
// its no-payload behavior (no hard failure).
//
// clineNormalize 把 cline 的 hook stdin（v3.36+ hooks）映射到 HookInput。文档化的
// 基础字段（{clineVersion, hookName, timestamp, taskId, workspaceRoots, userId} 加
// 一个字段名仅部分文档化的工具名/参数对象）与 Claude 形状有四处关键差异：
//   - 会话标识是 taskId（无 session_id）；项目根是 workspaceRoots[0]（无 cwd 字段）。
//   - 事件名是 hookName，值为**脚本名**——PreToolUse/PostToolUse/UserPromptSubmit 与
//     Claude 一致，但 SessionStart 组挂在 cline 的 TaskStart 上，故须映射回
//     （clineHookEvent），否则会话级 hook 的分发（以 "SessionStart" 为键）永不触发。
//   - 工具名是 cline 的 snake_case 名册（write_to_file/insert_content/
//     search_and_replace/read_file/execute_command）——映射到 forge 据以分发的
//     Claude 名（clineToCCToolName），与 reasonix 同判断。
//   - 工具 payload 的确切键未文档化：tool_name/toolName/tool 与
//     tool_input/toolInput/parameters 都作为候选接受，首个非空者胜。payload 的文件
//     类工具携带 {path, ...}——经 remapKimiToolInput 别名到 file_path，使
//     FORGE_FILE_PATH 得以解析。
//
// ToolName 与 ToolInput 在候选在场时**无条件覆盖**（区别于 reasonix 的填空）：
// 默认 unmarshal 已从同名的 snake_case tool_name 字段填了 ToolName——填空策略会
// 保留原始 cline 名（"write_to_file"）而跳过映射，恰是 normalizer 要防的 fail-open
// 形态。cline 绝不发送 Claude 形状 payload，故覆盖安全。超出文档化基础的字段名是
// 尽力而为的候选：未文档化的改名会让字段留空、hook 退化到无 payload 行为（不硬
// 失败）。
func clineNormalize(stdinData []byte, hookInput *HookInput) {
	if len(stdinData) == 0 {
		return
	}
	var c struct {
		HookName       string          `json:"hookName"`
		TaskID         string          `json:"taskId"`
		WorkspaceRoots []string        `json:"workspaceRoots"`
		ToolNameSnake  string          `json:"tool_name"`
		ToolNameCamel  string          `json:"toolName"`
		Tool           string          `json:"tool"`
		ToolInputSnake json.RawMessage `json:"tool_input"`
		ToolInputCamel json.RawMessage `json:"toolInput"`
		Parameters     json.RawMessage `json:"parameters"`
		Prompt         string          `json:"prompt"`
		UserPrompt     string          `json:"userPrompt"`
		Question       string          `json:"question"`
	}
	if err := json.Unmarshal(stdinData, &c); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: cline hook stdin JSON parse failed: %v\n", err)
		return
	}
	if hookInput.HookEventName == "" {
		hookInput.HookEventName = clineHookEvent(c.HookName)
	}
	if hookInput.SessionID == "" {
		hookInput.SessionID = c.TaskID
	}
	if hookInput.Cwd == "" && len(c.WorkspaceRoots) > 0 {
		hookInput.Cwd = c.WorkspaceRoots[0]
	}
	// Unconditional override when a candidate exists — see the function comment for
	// why fill-empty here would preserve the unmapped snake_case name the default
	// unmarshal already filled.
	//
	// 候选在场时无条件覆盖——为何此处不能用填空，见函数注释（填空会保留默认
	// unmarshal 已填入的未映射 snake_case 名）。
	if name := firstNonEmpty(c.ToolNameSnake, c.ToolNameCamel, c.Tool); name != "" {
		hookInput.ToolName = clineToCCToolName(name)
	}
	if raw := firstRawJSON(c.ToolInputSnake, c.ToolInputCamel, c.Parameters); len(raw) > 0 {
		hookInput.ToolInput = remapKimiToolInput(raw)
	}
	if hookInput.Prompt == "" {
		hookInput.Prompt = firstNonEmpty(c.Prompt, c.UserPrompt, c.Question)
	}
}

// clineHookEvent maps cline hook script names (== hookName values) to the Claude
// event names forge's hooks dispatch on. TaskStart carries the SessionStart group
// (see clineEventMappings); the tool/prompt events share Claude's names; unknown
// future names pass through unchanged so an unlisted cline event degrades to its
// no-payload behavior instead of being misfiled.
//
// clineHookEvent 把 cline hook 脚本名（== hookName 值）映射到 forge hook 据以分发的
// Claude 事件名。TaskStart 载着 SessionStart 组（见 clineEventMappings）；工具/
// prompt 事件与 Claude 同名；未知的未来名原样透传，使未列出的 cline 事件退化到无
// payload 行为而非被错归。
func clineHookEvent(hookName string) string {
	switch hookName {
	case "TaskStart":
		return "SessionStart"
	default:
		return hookName
	}
}

// clineToCCToolName maps cline's snake_case tool names to the Claude Code names forge
// dispatches on. write_to_file→Write; insert_content/search_and_replace→Edit (the
// path-extraction hooks care about file_path, not the Write/Edit distinction);
// read_file→Read (the reads-log record keys on exactly "Read"); execute_command→Bash.
// Unknown names pass through unchanged.
//
// clineToCCToolName 把 cline 的 snake_case 工具名映射到 forge 据以分发的 Claude Code
// 名。write_to_file→Write；insert_content/search_and_replace→Edit（基于路径的 hook
// 关心 file_path 而非 Write/Edit 之别）；read_file→Read（reads-log 记录恰以 "Read"
// 为键）；execute_command→Bash。未知名原样透传。
func clineToCCToolName(name string) string {
	switch name {
	case "write_to_file":
		return "Write"
	case "insert_content", "search_and_replace":
		return "Edit"
	case "read_file":
		return "Read"
	case "execute_command":
		return "Bash"
	}
	return name
}

// firstRawJSON returns the first non-empty RawMessage argument (nil when all are
// empty). The string twin firstNonEmpty already lives in hook.go.
//
// firstRawJSON 返回首个非空的 RawMessage 实参（全空时返回 nil）。字符串版的
// firstNonEmpty 已在 hook.go。
func firstRawJSON(raws ...json.RawMessage) json.RawMessage {
	for _, r := range raws {
		if len(r) > 0 {
			return r
		}
	}
	return nil
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
		Cwd           string          `json:"cwd"`
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
	hookInput.Cwd = k.Cwd
	hookInput.ToolName = k.ToolName
	hookInput.ToolInput = remapKimiToolInput(k.ToolInput)
	// kimi 的 tool_output 是纯字符串（非 Claude tool_response 的对象），skill-trigger
	// 按对象解析会失败 → ctx.ToolOutput 恒 nil：PostToolUse 上 exit_code/输出关键词类
	// 触发条件在 kimi 下全灭。包装成对象形状让下游解析成功（见 wrapKimiToolOutput）。
	hookInput.ToolOutput = wrapKimiToolOutput(k.ToolOutput)
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

// wrapKimiToolOutput wraps kimi's plain-string tool_output into an object shape so
// skill-trigger's object parse (json.Unmarshal into ctx.ToolOutput) succeeds instead
// of silently yielding nil — the gap that killed every PostToolUse trigger on kimi
// (verification-driver's test_command_failed, compile-fix-loop's output keywords).
// If the string content is itself serialized JSON of an object, that object is used
// directly (preserving exit_code etc.); otherwise it becomes {"output": "..."}
// (matchKeywords already reads the "output" key). Object-shaped payloads pass through.
//
// wrapKimiToolOutput 把 kimi 纯字符串形式的 tool_output 包装成对象形状，让
// skill-trigger 的对象解析（json.Unmarshal 进 ctx.ToolOutput）成功而非静默得 nil
// ——正是这个缺口灭掉了 kimi 上所有 PostToolUse 触发（verification-driver 的
// test_command_failed、compile-fix-loop 的输出关键词）。字符串内容本身若是对象的
// 序列化 JSON 则直接采用（保留 exit_code 等）；否则包装成 {"output": "..."}
// （matchKeywords 已检查 output 键）。对象形状原样透传。
func wrapKimiToolOutput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return raw // 已是对象/数组形状
	}
	if trimmed := strings.TrimSpace(s); strings.HasPrefix(trimmed, "{") {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
			return json.RawMessage(trimmed)
		}
	}
	wrapped, err := json.Marshal(map[string]string{"output": s})
	if err != nil {
		return raw
	}
	return wrapped
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
// Windsurf schema (verified against docs.windsurf.com/windsurf/cascade/hooks):
//
// Windsurf schema（已对 docs.windsurf.com/windsurf/cascade/hooks 核实）：
//
// Common fields present on EVERY event (so trajectory_id is always available as
// the session identifier):
//
// 每个 event 都有的公共字段（故 trajectory_id 恒可作为会话标识）：
//
//	{
//	  "agent_action_name": "pre_write_code",   // event name
//	  "trajectory_id":     "<session id>",      // overall Cascade conversation id
//	  "execution_id":      "<turn id>",         // single agent turn id
//	  "timestamp":         "<ISO 8601>",
//	  "model_name":        "Claude Sonnet 4",
//	  "tool_info": { ... }                      // event-specific, see below
//	}
//
// Per-event tool_info (per the official docs):
// 各 event 的 tool_info（按官方文档）：
//   - pre/post_read_code, pre/post_write_code: {file_path, edits[]?}
//   - pre/post_run_command:  {command_line, cwd}   — NOTE: the field is
//     command_line, not command. Older forge versions read tool_info.command,
//     which never exists in the documented payload, so bash-guard/hazard-guard
//     silently saw an empty command (fail-open). Both are read now, with
//     command_line preferred and command kept as a defensive fallback.
//   - pre_user_prompt:       {user_prompt}         — no cwd/session path field
//   - post_cascade_response: {response}            — no cwd/session path field
//
// pre_user_prompt / post_cascade_response carry NO cwd or project-path field in
// the documented payload (cwd is documented only for pre/post_run_command, inside
// tool_info). That is covered structurally rather than by payload mapping: Cascade
// runs hook commands with working_directory defaulting to the workspace root, and
// forge's hooks take their project context from the process cwd (FORGE_CWD =
// os.Getwd() in runHook), so the SessionStart/Stop groups wired onto these events
// still operate on the right project. Uncertainty: the docs show only example
// payloads, not a versioned schema — if a Windsurf version omits trajectory_id or
// renames a field, the affected field simply stays empty and the hook degrades to
// its existing no-payload behavior (no hard failure).
//
// pre_user_prompt / post_cascade_response 的文档化 payload 中没有 cwd 或项目路径
// 字段（cwd 只在 pre/post_run_command 的 tool_info 里有）。这一点靠结构而非
// payload 映射兜住：Cascade 以 working_directory 默认 workspace root 执行 hook
// 命令，而 forge 的 hook 从进程 cwd 取项目上下文（runHook 里 FORGE_CWD =
// os.Getwd()），故挂在这些 event 上的 SessionStart/Stop 组仍作用于正确项目。
// 不确定性：文档只给示例 payload 而非版本化 schema——若某 Windsurf 版本缺
// trajectory_id 或改了字段名，对应字段即为空，hook 退化到既有的无 payload
// 行为（不硬失败）。
//
// 这里把 tool_input 重建成 Claude 的 {file_path, content, command}，让既有
// toolInputFields 抽取逻辑无需改动即可拿到。
func windsurfNormalize(stdinData []byte, hookInput *HookInput) {
	var w struct {
		AgentActionName string `json:"agent_action_name"`
		TrajectoryID    string `json:"trajectory_id"`
		ToolInfo        struct {
			FilePath    string `json:"file_path"`
			CommandLine string `json:"command_line"` // documented field for *_run_command
			Command     string `json:"command"`      // defensive legacy fallback (undocumented)
			UserPrompt  string `json:"user_prompt"`  // pre_user_prompt
			Edits       []struct {
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
	if hookInput.Prompt == "" {
		hookInput.Prompt = w.ToolInfo.UserPrompt
	}
	if len(hookInput.ToolInput) == 0 {
		ti := map[string]string{}
		if w.ToolInfo.FilePath != "" {
			ti["file_path"] = w.ToolInfo.FilePath
		}
		// command_line is the documented field (docs.windsurf.com/windsurf/cascade/hooks,
		// pre/post_run_command examples); command is kept as a defensive fallback for
		// payloads from versions whose shape predates the docs.
		//
		// command_line 是文档化字段（docs.windsurf.com/windsurf/cascade/hooks 的
		// pre/post_run_command 示例）；command 作为防御性 fallback 保留，覆盖形态
		// 早于文档的版本 payload。
		command := w.ToolInfo.CommandLine
		if command == "" {
			command = w.ToolInfo.Command
		}
		if command != "" {
			ti["command"] = command
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
	// Cascade has no session_start/session_end: the SessionStart group hangs on
	// pre_user_prompt, the Stop group on post_cascade_response (see
	// buildWindsurfHooks). The legacy session_* cases stay so hook configs written
	// by older forge versions still normalize correctly.
	//
	// Cascade 没有 session_start/session_end：SessionStart 组挂 pre_user_prompt，
	// Stop 组挂 post_cascade_response（见 buildWindsurfHooks）。保留遗留
	// session_* case，让旧版 forge 写入的 hook 配置仍能正确归一化。
	case "pre_user_prompt", "session_start":
		return "SessionStart"
	case "post_cascade_response", "post_cascade_response_with_transcript", "session_end":
		return "Stop"
	}
	return ""
}

// reasonixNormalize maps the reasonix plugin-hook stdin to HookInput. reasonix's payload is
// camelCase and diverges from Claude Code in two load-bearing ways (verified by probing a live
// reasonix install — a tee wrapper captured real PreToolUse/PostToolUse/SessionStart/Stop payloads):
//   - Field names are camelCase: {event, sessionId, cwd, toolName, toolArgs} vs Claude's
//     {hook_event_name, session_id, cwd, tool_name, tool_input}. A direct unmarshal into HookInput
//     fills only Cwd (the one field whose name matches); SessionID, HookEventName, ToolName and
//     ToolInput all stay empty, so every path/command-based hook (task-guard, read-before-edit,
//     bash-guard, file-sentinel) failed open — the hooks fired once the snake_case matchers were
//     translated (see buildReasonixHooks) but parsed empty tool fields and could not enforce.
//   - Tool names are snake_case: write_file/edit_file/multi_edit/move_file/bash/read_file
//     (reasonix's [sandbox] roster) vs Claude's Write/Edit/Bash/Read. forge dispatches on the CC
//     name (e.g. hook.go records a read in the reads-log only when ToolName == "Read"), so
//     reasonixToCCToolName maps them back.
//
// toolArgs carries {path, old_string, new_string, command, ...}; path is the file path (Claude's
// file_path), aliased via remapKimiToolInput so FORGE_FILE_PATH resolves. runHook calls this AFTER
// the default unmarshal (like windsurf), filling only fields still empty so a Claude-shape payload
// is never clobbered.
//
// reasonixNormalize 把 reasonix plugin-hook 的 stdin 映射到 HookInput。reasonix 的 payload 是
// camelCase，在两处关键点上与 Claude Code 不同（已对真实 reasonix 安装探测——tee 包装器捕获了
// 真实的 PreToolUse/PostToolUse/SessionStart/Stop payload）：
//   - 字段名是 camelCase：{event, sessionId, cwd, toolName, toolArgs} 对比 Claude 的
//     {hook_event_name, session_id, cwd, tool_name, tool_input}。直接 unmarshal 进 HookInput 只填
//     得上 Cwd（唯一名字匹配的字段）；SessionID、HookEventName、ToolName、ToolInput 全空，故每个
//     基于路径/命令的 hook（task-guard、read-before-edit、bash-guard、file-sentinel）都 fail
//     open——snake_case matcher 翻译后 hook 确实触发了（见 buildReasonixHooks），但解析出空的
//     工具字段，无法 enforce。
//   - 工具名是 snake_case：write_file/edit_file/multi_edit/move_file/bash/read_file
//     （reasonix 的 [sandbox] 名册）对比 Claude 的 Write/Edit/Bash/Read。forge 按 CC 名分发（如
//     hook.go 仅当 ToolName == "Read" 时在 reads-log 记一次读），故 reasonixToCCToolName 把它们
//     映射回去。
//
// toolArgs 携带 {path, old_string, new_string, command, ...}；path 是文件路径（Claude 的
// file_path），经 remapKimiToolInput 别名，使 FORGE_FILE_PATH 得以解析。runHook 在默认 unmarshal
// 之后调用本函数（同 windsurf），仅填充仍为空的字段，故绝不覆盖 Claude-shape payload。
func reasonixNormalize(stdinData []byte, hookInput *HookInput) {
	if len(stdinData) == 0 {
		return
	}
	var r struct {
		Event     string          `json:"event"`
		SessionID string          `json:"sessionId"`
		Cwd       string          `json:"cwd"`
		ToolName  string          `json:"toolName"`
		ToolArgs  json.RawMessage `json:"toolArgs"`
	}
	if err := json.Unmarshal(stdinData, &r); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: reasonix hook stdin JSON parse failed: %v\n", err)
		return
	}
	if hookInput.HookEventName == "" {
		hookInput.HookEventName = r.Event
	}
	if hookInput.SessionID == "" {
		hookInput.SessionID = r.SessionID
	}
	if hookInput.Cwd == "" {
		hookInput.Cwd = r.Cwd
	}
	if hookInput.ToolName == "" {
		hookInput.ToolName = reasonixToCCToolName(r.ToolName)
	}
	if len(hookInput.ToolInput) == 0 {
		// reasonix's file tools carry {path, ...}; alias path→file_path so the path-based hooks
		// (read-before-edit, task-guard's .forge/* self-protection) resolve FORGE_FILE_PATH. Other
		// fields (old_string/new_string/command) pass through untouched.
		//
		// reasonix 的文件工具携带 {path, ...}；把 path 别名到 file_path，使基于路径的 hook
		// （read-before-edit、task-guard 的 .forge/* 自保护）能解析 FORGE_FILE_PATH。其余字段
		// （old_string/new_string/command）原样透传。
		hookInput.ToolInput = remapKimiToolInput(r.ToolArgs)
	}
}

// reasonixToCCToolName maps reasonix's snake_case tool names back to the Claude Code names forge
// dispatches on. reasonix's file-writers split into write_file/edit_file/multi_edit/move_file
// (config.toml [sandbox]); forge treats edit_file/multi_edit/move_file as Edit (the path-extraction
// hooks — read-before-edit, task-guard — care about file_path, not the Write/Edit distinction, the
// same call windsurf makes collapsing both write events to Write). bash→Bash, read_file→Read (the
// reads-log record at hook.go keys on exactly "Read"). Unknown names pass through unchanged.
//
// reasonixToCCToolName 把 reasonix 的 snake_case 工具名映射回 forge 据以分发的 Claude Code 名。
// reasonix 的文件写入器拆成 write_file/edit_file/multi_edit/move_file（config.toml [sandbox]）；
// forge 把 edit_file/multi_edit/move_file 当 Edit（基于路径的 hook——read-before-edit、
// task-guard——关心 file_path 而非 Write/Edit 之别，与 windsurf 把两个 write 事件都归到 Write 同
// 判断）。bash→Bash、read_file→Read（hook.go 的 reads-log 记录恰以 "Read" 为键）。未知名原样透传。
func reasonixToCCToolName(name string) string {
	switch name {
	case "write_file":
		return "Write"
	case "edit_file", "multi_edit", "move_file":
		return "Edit"
	case "bash":
		return "Bash"
	case "read_file":
		return "Read"
	}
	return name
}

// (copilotNormalize removed: refactor-data-home locked in five specialized agents, copilot is no longer adapted.
//  If you need to restore it in the future, implement per docs.github.com/en/copilot/reference/hooks-reference.)
//
// (copilotNormalize 删除：refactor-data-home 锁定 5 家专精，copilot 不再适配。
//  若未来需恢复，按 docs.github.com/en/copilot/reference/hooks-reference 实现。)
