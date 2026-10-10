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
import { matchedCommands } from "./tools.js";

const LOG = fileURLToPath(new URL("../test/doubles/.wiring-log", import.meta.url));
// The roster single source of truth — tests derive expected hook order from it.
const SPEC = JSON.parse(readFileSync(new URL("./spec.json", import.meta.url), "utf8"));

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
  assert.deepEqual(hookCalls().map((c) => c.hook), ["bash-guard", "hazard-guard", "gate-cmd-form", "skill-trigger", "task-drift"]);
  assert.equal(calls.injected.length, 1);
  assert.equal(calls.injected[0].source.kind, "plugin:forge-quality");
  // The kind must be producer-owned and must track the plugin's registered Cordis
  // name, so renaming index.js `name` cannot leave the tools.js literal stale
  // while the suite still passes. The hand-maintained copies of that identity
  // (cordis.patch.yml, the README install snippet, package.json's files and
  // dsh.bundle.patch linkage) are deliberately NOT asserted here: they are a text
  // convention, and every text-level check tried for them stayed green for files
  // YAML reads as inserting a different entry, or nothing at all. Tracked as a
  // finding rather than half-enforced by a guard that cannot be honest about it.
  assert.equal(calls.injected[0].source.kind, `plugin:${forgePlugin.name}`);
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
  // Expected roster is DERIVED from spec.json (SessionStart group, then the
  // PostCompact group), never hand-copied: the PostCompact group is
  // [compact-resume, conventions-context], and the old hand-written list
  // stopped at compact-resume — the poll then settled on compact-resume alone
  // and raced the trailing conventions-context run (CI flake on PR #100).
  const expected = [
    ...matchedCommands(SPEC.SessionStart, ""),
    ...matchedCommands(SPEC.PostCompact, ""),
  ].map((command) => command.replace(/^forge hook /, ""));
  // emit listeners are detached — settle only once the WHOLE roster (both
  // groups) has logged; any single hook name is an insufficient signal.
  // Generous ceiling: 8 serial spawns go through cmd.exe + .cmd shim on
  // Windows runners; the green path exits the poll early either way.
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline && hookCalls().length < expected.length) {
    await new Promise((r) => setTimeout(r, 25));
  }
  // Quiet window: the last line lands before its process exits, and the
  // group's inject runs after that — wait it out, then require the log to
  // stay put so a duplicated/extra hook run fails instead of slipping past.
  const settled = hookCalls().length;
  await new Promise((r) => setTimeout(r, 200));
  assert.equal(hookCalls().length, settled, "no further hook runs after the roster settled");
  assert.equal(calls.injected.length, 1);
  const hooks = hookCalls();
  assert.deepEqual(hooks.map((c) => c.hook), expected);
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
  // verdict summary (H1 契约加固): at-a-glance counts over the whole ring buffer
  assert.match(reply.text, /Verdict summary \(last 2 runs\)/);
  assert.match(reply.text, /runs=2  blocked=1  fail-open=0  contexts=/);
});

test("contract.json is the two-sided contract: spec groups ↔ event rows ↔ decision vocabulary", async () => {
  const contract = JSON.parse(readFileSync(new URL("../contract.json", import.meta.url), "utf8"));
  const spec = JSON.parse(readFileSync(new URL("./spec.json", import.meta.url), "utf8"));
  const wired = new Set(Object.keys(spec));
  const named = new Set(contract.events.map((e) => e.forge));
  // PostCompact 的条件触发必须在 session-start 行的 note 里声明
  //（DSH rc.7 没有专压缩点——丢了 note，快照 diff 里就看不见这条隐式映射）。
  assert.ok(
    contract.events.some((e) => e.forge === "SessionStart" && (e.note ?? "").includes("PostCompact")),
    "PostCompact 的条件触发须在 session-start 映射行的 note 里声明",
  );
  for (const f of named) {
    assert.ok(wired.has(f), `contract names forge event ${f} but spec.json wires no such group`);
  }
  for (const f of wired) {
    if (f === "PostCompact" || (contract.inert ?? []).includes(f)) continue;
    assert.ok(named.has(f), `spec.json wires ${f} but contract.json does not name it (add a row, an inert entry, or a note)`);
  }
  for (const e of contract.events) {
    assert.ok(["deny", "block", "reject", "inject", "steer"].includes(e.decision), `unknown decision kind: ${e.decision}`);
  }
  // fail-open 契约至少钉住「decision 只读 stdout」与「基础设施故障放行」两条。
  assert.ok(contract.failOpen.some((s) => s.includes("decision field")), "failOpen 必须钉住 decision 只读 stdout JSON");
  assert.ok(contract.failOpen.some((s) => s.includes("fails open")), "failOpen 必须钉住基础设施故障放行");
});
