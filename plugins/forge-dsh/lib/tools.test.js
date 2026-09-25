import test from "node:test";
import assert from "node:assert/strict";
import {
  buildEventPayload,
  buildToolPayload,
  matchedCommands,
  normalizeToolInput,
  pluginMessage,
  promptText,
  sessionBits,
  toCCToolName,
} from "./tools.js";

test("toCCToolName maps the DSH built-ins and passes unknowns through", () => {
  assert.equal(toCCToolName("write"), "Write");
  assert.equal(toCCToolName("edit"), "Edit");
  assert.equal(toCCToolName("str_replace_editor"), "Edit");
  assert.equal(toCCToolName("bash"), "Bash");
  assert.equal(toCCToolName("pwsh"), "Bash");
  assert.equal(toCCToolName("read"), "Read");
  assert.equal(toCCToolName("skill"), "Skill");
  assert.equal(toCCToolName("web_search"), "web_search"); // ungated, by design
});

test("normalizeToolInput keeps CC fields and remaps aliases without shadowing", () => {
  assert.deepEqual(normalizeToolInput({ file_path: "/a", content: "x" }), {
    file_path: "/a",
    content: "x",
  });
  assert.deepEqual(normalizeToolInput({ filePath: "/b", old_string: "o", new_string: "n" }), {
    file_path: "/b",
    old_string: "o",
    new_string: "n",
  });
  // canonical field wins over alias when both are present
  assert.deepEqual(normalizeToolInput({ path: "/alias", file_path: "/real" }), {
    file_path: "/real",
  });
  assert.deepEqual(normalizeToolInput("not-an-object"), {});
  assert.deepEqual(normalizeToolInput(null), {});
});

test("matchedCommands honors empty matcher and anchored alternation", () => {
  const groups = [
    { matcher: "Write|Edit", hooks: [{ command: "forge hook a" }] },
    { matcher: "Bash", hooks: [{ command: "forge hook b" }] },
    { hooks: [{ command: "forge hook c" }] },
  ];
  assert.deepEqual(matchedCommands(groups, "Edit"), ["forge hook a", "forge hook c"]);
  assert.deepEqual(matchedCommands(groups, "Bash"), ["forge hook b", "forge hook c"]);
  assert.deepEqual(matchedCommands(groups, "Read"), ["forge hook c"]);
  // matcher must be anchored: "EditSomething" is not "Edit"
  assert.deepEqual(matchedCommands([{ matcher: "Edit", hooks: [{ command: "x" }] }], "EditSomething"), []);
});

test("sessionBits reads the agent session, with process fallbacks", () => {
  assert.deepEqual(sessionBits({ session: { id: "s1", header: { cwd: "/proj" } } }), {
    sessionId: "s1",
    cwd: "/proj",
  });
  const fallback = sessionBits(undefined);
  assert.equal(fallback.sessionId, "");
  assert.equal(typeof fallback.cwd, "string");
});

test("buildToolPayload emits the Claude-Code shape forge parses", () => {
  const payload = buildToolPayload(
    {
      name: "write",
      arguments: { file_path: "/p/x.go", content: "package main" },
      agent: { session: { id: "s1", header: { cwd: "/proj" } } },
    },
    "PreToolUse",
  );
  assert.deepEqual(payload, {
    session_id: "s1",
    transcript_path: "",
    cwd: "/proj",
    hook_event_name: "PreToolUse",
    tool_name: "Write",
    tool_input: { file_path: "/p/x.go", content: "package main" },
    forge_agent: "dsh",
  });
});

test("buildEventPayload carries extra fields and attribution", () => {
  const payload = buildEventPayload("Stop", undefined, { stop_hook_active: false });
  assert.equal(payload.hook_event_name, "Stop");
  assert.equal(payload.stop_hook_active, false);
  assert.equal(payload.forge_agent, "dsh");
});

test("promptText concatenates text blocks across messages", () => {
  assert.equal(
    promptText([
      { content: [{ type: "text", text: "hello" }, { type: "reasoning", text: "skip" }] },
      { content: [{ type: "text", text: "world" }] },
    ]),
    "hello\nworld",
  );
  assert.equal(promptText(undefined), "");
});

test("pluginMessage is a plugin-sourced user text message", () => {
  const m = pluginMessage("ctx");
  assert.equal(m.role, "user");
  assert.deepEqual(m.source, { kind: "plugin", plugin: "forge-quality" });
  assert.deepEqual(m.content, [{ type: "text", text: "ctx" }]);
  assert.notEqual(pluginMessage("a").id, pluginMessage("b").id);
});
