/**
 * Wiring test: the REAL @deepseek-ai/cordis runtime dispatches DSH's typed
 * interception points through our plugin into the fake forge double.
 * Asserts the full loop: event → matched group → serial hook runs (order +
 * short-circuit via the payload log) → typed decision / agent side effect.
 */
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, rmSync } from "node:fs";
import { Context } from "@deepseek-ai/cordis";
import * as forgePlugin from "./index.js";

import { fileURLToPath } from "node:url";
import { FAKE_BIN as FAKE } from "../test/doubles/forge-bin.mjs";

const LOG = fileURLToPath(new URL("../test/doubles/.wiring-log", import.meta.url));

function makeAgent(cwd = process.cwd()) {
  const calls = { injected: [], steered: [] };
  const agent = {
    session: { id: "sess-1", header: { cwd } },
    inject: (m) => calls.injected.push(m),
    steer: (m) => calls.steered.push(m),
  };
  return { agent, calls };
}

function hookCalls() {
  try {
    return readFileSync(LOG, "utf8").trim().split("\n").map(JSON.parse);
  } catch {
    return [];
  }
}

async function boot(t) {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => {
    delete process.env.FAKE_FORGE_LOG;
  });
  const ctx = new Context();
  ctx.provide("tools", {}); // inject = ["tools"] only needs the service to exist
  await ctx.plugin(forgePlugin, { forgeBin: FAKE, timeoutMs: 5000 });
  return ctx;
}

test("pre-execute: task-guard block denies a write, group short-circuits in spec order", async (t) => {
  const ctx = await boot(t);
  const { agent } = makeAgent();
  const exec = { name: "write", arguments: { file_path: "/p/x.go", content: "y" }, agent };
  const d = await ctx.waterfall("tools/pre-execute", exec, async () => ({ kind: "allow" }));
  assert.deepEqual(d, { kind: "deny", reason: "BLOCKED: no active task (fake)" });
  const hooks = hookCalls().map((c) => c.hook);
  assert.deepEqual(hooks, ["freeze-guard", "task-guard"]); // later gates never ran
  // the payload the hooks saw is Claude-shaped and attributed
  const seen = hookCalls()[0].payload;
  assert.equal(seen.hook_event_name, "PreToolUse");
  assert.equal(seen.tool_name, "Write");
  assert.equal(seen.tool_input.file_path, "/p/x.go");
  assert.equal(seen.forge_agent, "dsh");
  assert.equal(seen.session_id, "sess-1");
});

test("pre-execute: clean bash delegates; skill-trigger advisory reaches agent.inject", async (t) => {
  const ctx = await boot(t);
  const { agent, calls } = makeAgent();
  const exec = { name: "bash", arguments: { command: "go build ./..." }, agent };
  const d = await ctx.waterfall("tools/pre-execute", exec, async () => ({ kind: "allow" }));
  assert.deepEqual(d, { kind: "allow" });
  assert.deepEqual(hookCalls().map((c) => c.hook), ["bash-guard", "hazard-guard", "skill-trigger"]);
  assert.equal(calls.injected.length, 1);
  assert.equal(calls.injected[0].source.plugin, "forge-quality");
  assert.equal(calls.steered.length, 0);
});

test("pre-execute: ungated tool never touches forge", async (t) => {
  const ctx = await boot(t);
  const d = await ctx.waterfall(
    "tools/pre-execute",
    { name: "web_search", arguments: { query: "x" } },
    async () => ({ kind: "allow" }),
  );
  assert.deepEqual(d, { kind: "allow" });
  assert.equal(hookCalls().length, 0);
});

test("pre-execute: pwsh maps to the Bash gate roster (rm -rf denied)", async (t) => {
  const ctx = await boot(t);
  const exec = { name: "pwsh", arguments: { command: "rm -rf /" } };
  const d = await ctx.waterfall("tools/pre-execute", exec, async () => ({ kind: "allow" }));
  assert.deepEqual(d, { kind: "deny", reason: "hazard: rm -rf intercepted (fake)" });
});

test("post-execute: block becomes a feedback error result", async (t) => {
  const ctx = await boot(t);
  const exec = { name: "bash", arguments: { command: "echo quarantine-me" } };
  const result = { isError: false, content: [] };
  const d = await ctx.waterfall("tools/post-execute", exec, result, async () => ({ kind: "accept" }));
  assert.equal(d.kind, "block");
  assert.equal(d.feedback[0].text, "file-sentinel: unauthorized change (fake)");
  // the block variant of PostToolDecision carries no additionalContexts field
  assert.equal(d.additionalContexts, undefined);
});

test("post-execute: allow-path context folds into the downstream accept", async (t) => {
  const ctx = await boot(t);
  const exec = { name: "write", arguments: { file_path: "/p/x.go", content: "y" } };
  const d = await ctx.waterfall("tools/post-execute", exec, { isError: false, content: [] }, async () => ({
    kind: "accept",
    additionalContexts: [{ id: "downstream-ctx" }],
  }));
  assert.equal(d.kind, "accept");
  assert.equal(d.additionalContexts.length, 2);
  assert.equal(d.additionalContexts[0].content[0].text, "consider the test-discipline skill");
  assert.deepEqual(d.additionalContexts[1], { id: "downstream-ctx" });
});

test("pre-step: prompt text reaches hooks; advisory prepends into enter", async (t) => {
  const ctx = await boot(t);
  const { agent } = makeAgent();
  const payload = {
    agent,
    messages: [{ role: "user", content: [{ type: "text", text: "fix the flaky test" }] }],
    turn: 1,
    step: 1,
  };
  const d = await ctx.waterfall("agent/pre-step", payload, async () => ({
    kind: "enter",
    messages: payload.messages,
  }));
  assert.equal(d.kind, "enter");
  assert.equal(d.messages.length, 2);
  assert.equal(d.messages[0].content[0].text, "consider the test-discipline skill");
  assert.equal(d.messages[1].content[0].text, "fix the flaky test");
  assert.equal(hookCalls()[0].payload.prompt, "fix the flaky test");
});

test("pre-step: a blocking gate rejects the step, reason still reaches the agent", async (t) => {
  const ctx = await boot(t);
  const { agent, calls } = makeAgent();
  const payload = {
    agent,
    messages: [{ role: "user", content: [{ type: "text", text: "please block-prompt now" }] }],
    turn: 1,
    step: 1,
  };
  const d = await ctx.waterfall("agent/pre-step", payload, async () => ({ kind: "enter", messages: [] }));
  assert.deepEqual(d, { kind: "reject" });
  // PreStepDecision's reject carries no reason — it must arrive via inject instead
  assert.deepEqual(
    calls.injected.map((m) => m.content[0].text),
    ["prompt rejected by gate (fake)"],
  );
});

test("turn-stopping: a blocking gate steers another step (serial mode)", async (t) => {
  const ctx = await boot(t);
  const { agent, calls } = makeAgent();
  await ctx.serial("agent/turn-stopping", { agent, turn: 1 });
  assert.equal(calls.steered.length, 1);
  assert.equal(calls.steered[0].content[0].text, "BLOCKED: task gates not done (fake)");
  // short-circuit: review-stop/skill-trigger never ran after task-verify blocked
  assert.deepEqual(hookCalls().map((c) => c.hook), ["task-verify"]);
  assert.equal(hookCalls()[0].payload.stop_hook_active, false);
});

test("session-start: emit-mode context lands via inject; source compact also fires PostCompact", async (t) => {
  const ctx = await boot(t);
  const { agent, calls } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "compact" });
  // emit listeners are detached — poll until the compact group has run too
  // (SessionStart's inject lands BEFORE compact-resume fires, so injected
  // length alone is not a sufficient settle signal).
  const deadline = Date.now() + 5000;
  while (Date.now() < deadline && !hookCalls().some((c) => c.hook === "compact-resume")) {
    await new Promise((r) => setTimeout(r, 25));
  }
  assert.equal(calls.injected.length, 1);
  const hooks = hookCalls();
  assert.deepEqual(
    hooks.map((c) => c.hook),
    ["skill-scan", "mcp-scan", "init-suggest", "task-resume", "skill-trigger", "compact-resume"],
  );
  assert.equal(hooks[0].payload.hook_event_name, "SessionStart");
  assert.equal(hooks[0].payload.source, "compact");
  assert.equal(hooks.at(-1).payload.hook_event_name, "PostCompact");
});

test("disabled config wires no listeners", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const ctx = new Context();
  ctx.provide("tools", {});
  await ctx.plugin(forgePlugin, { forgeBin: FAKE, enabled: false });
  const d = await ctx.waterfall("tools/pre-execute", { name: "write", arguments: {} }, async () => ({ kind: "allow" }));
  assert.deepEqual(d, { kind: "allow" });
  assert.equal(hookCalls().length, 0);
});

test("/forge-status command renders wired groups and recent runs", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const registered = new Map();
  const ctx = new Context();
  ctx.provide("tools", {});
  // fake commands service: capture the registration the way dsh-cmdline would
  ctx.provide("commands", {
    register: (cmd) => {
      registered.set(cmd.name, cmd);
      return () => true;
    },
  });
  await ctx.plugin(forgePlugin, { forgeBin: FAKE, timeoutMs: 5000 });
  const status = registered.get("forge-status");
  assert.ok(status, "forge-status command must be registered when the commands service exists");

  // produce one blocked run and one context run so both rows show up
  await ctx.waterfall("tools/pre-execute", { name: "write", arguments: {} }, async () => ({ kind: "allow" }));
  await ctx.waterfall("tools/pre-execute", { name: "bash", arguments: { command: "ls" }, agent: makeAgent().agent }, async () => ({ kind: "allow" }));

  const reply = await status.handler();
  assert.equal(reply.kind, "success");
  assert.match(reply.text, /PreToolUse: freeze-guard, task-guard/);
  assert.match(reply.text, /forgeBin:/);
  assert.match(reply.text, /pre-execute/);
  assert.match(reply.text, /blocked-by=forge hook task-guard/);
});
