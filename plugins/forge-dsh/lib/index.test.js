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

/** One pre-step round trip; returns the enter decision's message texts. */
async function preStepTexts(ctx, agent, prompt = "hello") {
  const payload = {
    agent,
    messages: [{ role: "user", content: [{ type: "text", text: prompt }] }],
    turn: 1,
    step: 1,
  };
  const d = await ctx.waterfall("agent/pre-step", payload, async () => ({
    kind: "enter",
    messages: payload.messages,
  }));
  return { decision: d, texts: (d.messages ?? []).map((m) => m.content[0].text) };
}

test("session-start: contexts reach the FIRST pre-step even when it races the detached run", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const ctx = new Context();
  ctx.provide("tools", {});
  await ctx.plugin(forgePlugin, { forgeBin: FAKE, timeoutMs: 5000, startWaitMs: 15000 });
  const { agent, calls } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "startup" });
  // no polling: the pre-step arrives while the session-start group is mid-run
  const first = await preStepTexts(ctx, agent);
  assert.equal(first.texts.at(-1), "hello", "the user prompt stays last");
  // [session-start context(s)..., pre-step's own advisory, user prompt]: the
  // queue lands BEFORE the pre-step advisory and the prompt, never after
  assert.equal(first.texts.length, 3, "one start context + own advisory + prompt");
  assert.deepEqual(first.texts, [
    "consider the test-discipline skill",
    "consider the test-discipline skill",
    "hello",
  ]);
  assert.equal(first.decision.messages[0].source.kind, "plugin:forge-quality");
  assert.equal(calls.injected.length, 0, "session-start no longer relies on agent.inject");
  // delivered exactly once: a later pre-step carries only its own advisory
  const second = await preStepTexts(ctx, agent, "again");
  assert.deepEqual(
    second.texts,
    ["consider the test-discipline skill", "again"],
    "the start queue was drained by the first pre-step",
  );
});

test("session-start: a slow start does not tax later steps — only the first pre-step waits, contexts land once it finishes", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const ctx = new Context();
  ctx.provide("tools", {});
  await ctx.plugin(forgePlugin, { forgeBin: FAKE, timeoutMs: 5000, startWaitMs: 1 });
  const { agent } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "startup" });
  // 1ms wait: the pre-step gives up before the detached run finishes
  const early = await preStepTexts(ctx, agent);
  assert.deepEqual(early.texts, ["consider the test-discipline skill", "hello"], "no start context yet, own advisory only");
  // once the start run settles, the next enter carries the context — exactly once
  const expected = matchedCommands(SPEC.SessionStart, "").length;
  const deadline = Date.now() + 15000;
  const startRuns = () => hookCalls().filter((c) => c.payload.hook_event_name === "SessionStart").length;
  while (Date.now() < deadline && startRuns() < expected) {
    await new Promise((r) => setTimeout(r, 25));
  }
  await new Promise((r) => setTimeout(r, 300));
  const late = await preStepTexts(ctx, agent, "later");
  assert.deepEqual(late.texts, [
    "consider the test-discipline skill",
    "consider the test-discipline skill",
    "later",
  ]);
  const after = await preStepTexts(ctx, agent, "after");
  assert.deepEqual(after.texts, ["consider the test-discipline skill", "after"]);
});

test("session-start: overlapping starts do not stack the same onboarding; id-keyed state survives a fresh agent wrapper", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const ctx = new Context();
  ctx.provide("tools", {});
  await ctx.plugin(forgePlugin, { forgeBin: FAKE, timeoutMs: 5000, startWaitMs: 15000 });
  const { agent } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "startup" });
  ctx.emit("agent/session-start", { agent, source: "startup" });
  // a different wrapper object for the SAME session id (host re-wrapping)
  const wrapper = { ...agent };
  const first = await preStepTexts(ctx, wrapper);
  assert.deepEqual(first.texts, [
    "consider the test-discipline skill",
    "consider the test-discipline skill",
    "hello",
  ], "one deduped start context, not two");
});

test("pre-step: no agent handle and no session-start passes through untouched", async (t) => {
  const ctx = await boot(t);
  const payload = { messages: [{ role: "user", content: [{ type: "text", text: "x" }] }], turn: 1, step: 1 };
  const d = await ctx.waterfall("agent/pre-step", payload, async () => ({ kind: "enter", messages: payload.messages }));
  assert.equal(d.kind, "enter");
  assert.deepEqual(d.messages.map((m) => m.content[0].text), ["consider the test-discipline skill", "x"]);
});

test("session-start: a rejected pre-step keeps the queue for the next one", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const ctx = new Context();
  ctx.provide("tools", {});
  await ctx.plugin(forgePlugin, { forgeBin: FAKE, timeoutMs: 5000, startWaitMs: 15000 });
  const { agent, calls } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "startup" });
  const rejected = await preStepTexts(ctx, agent, "please block-prompt now");
  assert.deepEqual(rejected.decision, { kind: "reject" });
  assert.deepEqual(
    calls.injected.map((m) => m.content[0].text),
    ["prompt rejected by gate (fake)"],
    "reject still surfaces the gate reason via inject and does not drain the start queue",
  );
  const next = await preStepTexts(ctx, agent, "ok now");
  assert.deepEqual(
    next.texts,
    ["consider the test-discipline skill", "consider the test-discipline skill", "ok now"],
    "the start context survived the reject and arrives with the next enter, once",
  );
});

test("session-start: source compact also fires PostCompact; contexts ride the next pre-step", async (t) => {
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
  assert.equal(calls.injected.length, 0, "nothing goes through agent.inject");
  const hooks = hookCalls();
  assert.deepEqual(hooks.map((c) => c.hook), expected);
  assert.equal(hooks[0].payload.hook_event_name, "SessionStart");
  assert.equal(hooks[0].payload.source, "compact");
  assert.equal(hooks.at(-1).payload.hook_event_name, "PostCompact");
  const { texts } = await preStepTexts(ctx, agent);
  assert.ok(
    texts.filter((x) => x === "consider the test-discipline skill").length >= 2,
    "the queued session-start contexts ride the next pre-step",
  );
});

test("session-start: a missing forge queues one visible notice per session, delivered with the first pre-step", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const BAD = fileURLToPath(new URL("../test/doubles/no-such-forge", import.meta.url));
  const registered = new Map();
  const ctx = new Context();
  ctx.provide("tools", {});
  ctx.provide("commands", {
    register: (cmd) => {
      registered.set(cmd.name, cmd);
      return () => true;
    },
  });
  await ctx.plugin(forgePlugin, { forgeBin: BAD, timeoutMs: 5000, startWaitMs: 15000 });
  const noticesIn = (texts) => texts.filter((x) => x.includes("forge CLI not found"));
  const { agent, calls } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "startup" });
  const first = await preStepTexts(ctx, agent);
  const notices = noticesIn(first.texts);
  assert.equal(notices.length, 1, "exactly one notice");
  assert.ok(notices[0].includes(BAD), "notice names the configured forgeBin");
  assert.match(notices[0], /npm install -g @agent_forge\/forge/);
  assert.equal(first.texts.at(-1), "hello", "the user prompt stays last");
  assert.equal(calls.injected.length, 0);
  // compact is the SAME session — the notice must not repeat on every boundary
  ctx.emit("agent/session-start", { agent, source: "compact" });
  const afterCompact = await preStepTexts(ctx, agent, "next");
  assert.equal(noticesIn(afterCompact.texts).length, 0);

  const status = registered.get("forge-status");
  const reply = await status.handler();
  assert.match(reply.text, /probe: FAILED/);
  assert.match(reply.text, /gates inactive/);

  // a NEW session gets its own notice: the WeakSet is per agent, not process-wide
  const second = makeAgent();
  second.agent.session.id = "sess-2";
  ctx.emit("agent/session-start", { agent: second.agent, source: "startup" });
  const secondFirst = await preStepTexts(ctx, second.agent);
  assert.equal(noticesIn(secondFirst.texts).length, 1, "the new session is told too");
});

test("bundled forge: used when no forgeBin is configured, and the agent is told its absolute path once", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  const savedBin = process.env.FORGE_BIN;
  delete process.env.FORGE_BIN;
  t.after(() => {
    delete process.env.FAKE_FORGE_LOG;
    if (savedBin !== undefined) process.env.FORGE_BIN = savedBin;
  });
  const registered = new Map();
  const ctx = new Context();
  ctx.provide("tools", {});
  ctx.provide("commands", {
    register: (cmd) => {
      registered.set(cmd.name, cmd);
      return () => true;
    },
  });
  await ctx.plugin(forgePlugin, { resolveBundledForge: () => FAKE, timeoutMs: 5000, startWaitMs: 15000 });
  const { agent } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "startup" });
  const first = await preStepTexts(ctx, agent);
  const hints = first.texts.filter((x) => x.includes("forge CLI bundled with this plugin"));
  assert.equal(hints.length, 1, "exactly one hint");
  assert.ok(hints[0].includes(FAKE), "the hint carries the absolute path");
  assert.equal(first.texts[0], hints[0], "the hint leads the queue, ahead of forge's own contexts");
  assert.ok(hookCalls().length > 0, "hooks ran through the bundled binary");
  // a compact boundary in the same session must not repeat it
  ctx.emit("agent/session-start", { agent, source: "compact" });
  const later = await preStepTexts(ctx, agent, "next");
  assert.equal(later.texts.filter((x) => x.includes("forge CLI bundled")).length, 0);
  const reply = await registered.get("forge-status").handler();
  assert.ok(reply.text.includes(`forgeBin: ${FAKE} (bundled)`), reply.text);
});

test("bundled forge: an explicit forgeBin wins and no hint is given", async (t) => {
  rmSync(LOG, { force: true });
  process.env.FAKE_FORGE_LOG = LOG;
  t.after(() => delete process.env.FAKE_FORGE_LOG);
  const ctx = new Context();
  ctx.provide("tools", {});
  await ctx.plugin(forgePlugin, {
    forgeBin: FAKE,
    resolveBundledForge: () => "/must/not/be/used/forge",
    timeoutMs: 5000,
    startWaitMs: 15000,
  });
  const { agent } = makeAgent();
  ctx.emit("agent/session-start", { agent, source: "startup" });
  const first = await preStepTexts(ctx, agent);
  assert.equal(first.texts.filter((x) => x.includes("bundled with this plugin")).length, 0);
  assert.ok(hookCalls().length > 0, "hooks ran through the configured binary");
});

test("bundled forge: no bundle and no config falls back to PATH forge, reported as such", async (t) => {
  const savedBin = process.env.FORGE_BIN;
  delete process.env.FORGE_BIN;
  t.after(() => {
    if (savedBin !== undefined) process.env.FORGE_BIN = savedBin;
  });
  const registered = new Map();
  const ctx = new Context();
  ctx.provide("tools", {});
  ctx.provide("commands", {
    register: (cmd) => {
      registered.set(cmd.name, cmd);
      return () => true;
    },
  });
  await ctx.plugin(forgePlugin, { resolveBundledForge: () => undefined });
  const reply = await registered.get("forge-status").handler();
  assert.match(reply.text, /forgeBin: forge \(PATH\)/);
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
