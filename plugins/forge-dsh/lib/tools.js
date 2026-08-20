/**
 * @agent_forge/forge-dsh — DSH → Claude-Code vocabulary mapping.
 *
 * forge's hook dispatch keys on Claude Code tool names (Write/Edit/Bash/Read/
 * Skill/Agent) and Claude-shape payloads (HookInput in internal/cli/hook.go).
 * DSH's built-in fs tools already use Claude-style argument field names
 * (file_path/content/old_string/new_string — verified against
 * @deepseek-ai/dsh-tool-fs 0.1.0-rc.7), so tool_input is a light alias pass
 * rather than a deep translation.
 *
 * @module tools
 */

/** DSH built-in tool name → Claude Code tool name forge dispatches on. */
export const CC_TOOL_NAME = {
  write: "Write",
  edit: "Edit",
  str_replace_editor: "Edit", // Minimal preset's editor tool
  bash: "Bash",
  pwsh: "Bash",
  read: "Read",
  read_image: "Read",
  skill: "Skill",
};

/**
 * Map a DSH tool name to its Claude Code equivalent. Unknown names pass
 * through unchanged — no spec matcher will match them, so they sail through
 * the waterfalls ungated (same as an unmatched tool in Claude Code).
 *
 * @param {string} dshName
 * @returns {string}
 */
export function toCCToolName(dshName) {
  return CC_TOOL_NAME[dshName] ?? dshName;
}

/** Field aliases from non-CC argument shapes to Claude's tool_input keys. */
const INPUT_ALIASES = {
  filePath: "file_path",
  path: "file_path",
  newText: "content",
};

/**
 * Normalize one tool's argument object into Claude tool_input shape. DSH fs
 * tools already use file_path/content/old_string/new_string, so this mostly
 * copies; aliases cover camelCase variants seen in community tools. The
 * original arguments object is never mutated.
 *
 * @param {unknown} args
 * @returns {Record<string, unknown>}
 */
export function normalizeToolInput(args) {
  const out = {};
  if (args === null || typeof args !== "object" || Array.isArray(args)) return out;
  for (const [key, value] of Object.entries(args)) {
    out[INPUT_ALIASES[key] ?? key] = value;
  }
  // A bare alias must not shadow an already-correct CC field (args carrying
  // both file_path and path: file_path wins regardless of iteration order).
  for (const [alias, canonical] of Object.entries(INPUT_ALIASES)) {
    if (args[alias] !== undefined && args[canonical] !== undefined) {
      out[canonical] = args[canonical];
    }
  }
  return out;
}

/**
 * Collect the spec hook commands of one event whose matcher accepts ccName.
 * A group with an empty/absent matcher always matches; otherwise the matcher
 * is an alternation regex tested against the Claude tool name (identical to
 * Claude Code's matcher semantics for forge's own spec — every matcher in
 * spec.json is a plain alternation like "Write|Edit").
 *
 * @param {Array<{matcher?: string, hooks: Array<{command: string}>}>} groups
 * @param {string} ccName
 * @returns {string[]} matched commands, in spec order
 */
export function matchedCommands(groups, ccName) {
  const commands = [];
  for (const group of groups ?? []) {
    if (group.matcher !== undefined && group.matcher !== "") {
      if (!new RegExp(`^(?:${group.matcher})$`).test(ccName)) continue;
    }
    for (const hook of group.hooks ?? []) commands.push(hook.command);
  }
  return commands;
}

/**
 * Extract the session identity forge expects from a DSH agent handle.
 * Falls back to process cwd and an empty session id (forge degrades to its
 * legacy global state file for an empty session id — same degradation the
 * official bridges accept when no transcript locator exists).
 *
 * @param {object|undefined} agent
 * @returns {{ sessionId: string, cwd: string }}
 */
export function sessionBits(agent) {
  const session = agent?.session;
  return {
    sessionId: session?.id ?? "",
    cwd: session?.header?.cwd ?? process.cwd(),
  };
}

/**
 * Build the Claude-Code-shape stdin payload for one tool event.
 *
 * @param {object} exec - DSH ToolExecution ({name, arguments, agent?}).
 * @param {"PreToolUse"|"PostToolUse"} event
 * @returns {object}
 */
export function buildToolPayload(exec, event) {
  const { sessionId, cwd } = sessionBits(exec?.agent);
  return {
    session_id: sessionId,
    transcript_path: "",
    cwd,
    hook_event_name: event,
    tool_name: toCCToolName(exec?.name ?? ""),
    tool_input: normalizeToolInput(exec?.arguments),
    forge_agent: "dsh",
  };
}

/**
 * Build the stdin payload for a non-tool event (Stop / SessionStart /
 * UserPromptSubmit / PostCompact).
 *
 * @param {string} event
 * @param {object|undefined} agent
 * @param {object} [extra] - extra fields (prompt, source, stop_hook_active…).
 * @returns {object}
 */
export function buildEventPayload(event, agent, extra = {}) {
  const { sessionId, cwd } = sessionBits(agent);
  return {
    session_id: sessionId,
    transcript_path: "",
    cwd,
    hook_event_name: event,
    forge_agent: "dsh",
    ...extra,
  };
}

/**
 * Concatenate the visible text of user messages into one prompt string
 * (forge's UserPromptSubmit consumers read a flat `prompt` field).
 *
 * @param {Array<{content?: Array<{type: string, text?: string}>}>} messages
 * @returns {string}
 */
export function promptText(messages) {
  const parts = [];
  for (const message of messages ?? []) {
    for (const block of message?.content ?? []) {
      if (block?.type === "text" && typeof block.text === "string") parts.push(block.text);
    }
  }
  return parts.join("\n");
}

let messageSeq = 0;

/**
 * Build one plugin-sourced user message (the shape dsh-llm's
 * createUserMessage freezes: role 'user', text content, plugin source).
 * Constructed literally to keep this package dependency-free — importing
 * @deepseek-ai/dsh-llm would only resolve when the install layout happens to
 * nest it above us.
 *
 * @param {string} text
 * @returns {{id: string, role: "user", content: Array<{type: "text", text: string}>, source: {kind: "plugin", plugin: string}}}
 */
export function pluginMessage(text) {
  messageSeq += 1;
  return {
    id: `forge-dsh-${Date.now()}-${messageSeq}`,
    role: "user",
    content: [{ type: "text", text }],
    source: { kind: "plugin", plugin: "forge-quality" },
  };
}
