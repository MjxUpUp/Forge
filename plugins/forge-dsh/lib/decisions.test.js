import test from "node:test";
import assert from "node:assert/strict";
import {
  postExecuteDecision,
  preExecuteDecision,
  preStepDecision,
  runHookGroup,
  runMatchedGroups,
  dropQuietNotes,
  prependStartContexts,
  turnStoppingOutcome,
} from "./decisions.js";

/** Fake runner scripting verdicts per command name. */
const fakeRunner = (table, calls = []) => async (command) => {
  calls.push(command);
  return table[command] ?? { block: false };
};

test("runHookGroup: first block wins and short-circuits the group", async () => {
  const calls = [];
  const outcome = await runHookGroup(["forge hook a", "forge hook b", "forge hook c"], {}, {
    runner: fakeRunner({ "forge hook b": { block: true, reason: "no" } }, calls),
  });
  assert.equal(outcome.blocked.reason, "no");
  assert.deepEqual(calls, ["forge hook a", "forge hook b"]); // c never ran
});

test("runHookGroup: contexts accumulate in order, fail-open errors are silent", async () => {
  const outcome = await runHookGroup(["forge hook a", "forge hook b", "forge hook c"], {}, {
    runner: fakeRunner({
      "forge hook a": { block: false, context: "one" },
      "forge hook b": { block: false, error: "spawn blew up" },
      "forge hook c": { block: false, context: "two" },
    }),
  });
  assert.equal(outcome.blocked, undefined);
  assert.deepEqual(outcome.contexts, ["one", "two"]);
  assert.deepEqual(outcome.errors, ["forge hook b: spawn blew up"]);
});

test("preExecuteDecision: block → deny; pass delegates and injects context", async () => {
  const deny = await preExecuteDecision({ blocked: { reason: "gate" }, contexts: [], errors: [] }, undefined, async () => {
    throw new Error("next() must not run after a block");
  });
  assert.deepEqual(deny, { kind: "deny", reason: "gate" });

  const injected = [];
  const agent = { inject: (m) => injected.push(m) };
  const allow = await preExecuteDecision({ blocked: undefined, contexts: ["advice"], errors: [] }, agent, async () => ({
    kind: "allow",
  }));
  assert.deepEqual(allow, { kind: "allow" });
  assert.equal(injected.length, 1);
  assert.equal(injected[0].content[0].text, "advice");
  assert.equal(injected[0].source.kind, "plugin:forge-quality");
});

test("postExecuteDecision: block → block+feedback only; pre-block contexts fall back to agent.inject", async () => {
  const injected = [];
  const blocked = await postExecuteDecision(
    { blocked: { reason: "compile broke" }, contexts: ["earlier advisory"], errors: [] },
    async () => {
      throw new Error("next() must not run after a block");
    },
    { inject: (m) => injected.push(m) },
  );
  // the PostToolDecision block variant carries NO additionalContexts
  assert.deepEqual(blocked, {
    kind: "block",
    feedback: [{ type: "text", text: "compile broke" }],
  });
  assert.equal(injected.length, 1);
  assert.equal(injected[0].content[0].text, "earlier advisory");

  const folded = await postExecuteDecision(
    { blocked: undefined, contexts: ["test-guard advisory"], errors: [] },
    async () => ({ kind: "accept", additionalContexts: [{ id: "downstream" }] }),
  );
  assert.equal(folded.kind, "accept");
  assert.equal(folded.additionalContexts.length, 2);
  assert.equal(folded.additionalContexts[0].content[0].text, "test-guard advisory"); // ours first
  assert.deepEqual(folded.additionalContexts[1], { id: "downstream" });

  const plain = await postExecuteDecision({ blocked: undefined, contexts: [], errors: [] }, async () => ({ kind: "accept" }));
  assert.deepEqual(plain, { kind: "accept" });
});

test("preStepDecision: block → reject with the reason injected; context prepends into enter", async () => {
  const injected = [];
  const rejected = await preStepDecision(
    { blocked: { reason: "x" }, contexts: ["prior ctx"], errors: [] },
    async () => {
      throw new Error("unreachable");
    },
    { inject: (m) => injected.push(m) },
  );
  assert.deepEqual(rejected, { kind: "reject" });
  // reject carries no reason — the gate's feedback must reach the agent anyway
  assert.deepEqual(
    injected.map((m) => m.content[0].text),
    ["x", "prior ctx"],
  );

  const enter = await preStepDecision(
    { blocked: undefined, contexts: ["resume context"], errors: [] },
    async () => ({ kind: "enter", messages: [{ id: "user-1" }] }),
  );
  assert.equal(enter.kind, "enter");
  assert.equal(enter.messages.length, 2);
  assert.equal(enter.messages[0].content[0].text, "resume context");
  assert.deepEqual(enter.messages[1], { id: "user-1" });

  const downstreamReject = await preStepDecision(
    { blocked: undefined, contexts: ["ignored"], errors: [] },
    async () => ({ kind: "reject" }),
  );
  assert.deepEqual(downstreamReject, { kind: "reject" });
});

test("turnStoppingOutcome: block steers, pass injects", async () => {
  const steered = [];
  await turnStoppingOutcome({ blocked: { reason: "gates not done" }, contexts: [], errors: [] }, {
    steer: (m) => steered.push(m),
  });
  assert.equal(steered.length, 1);
  assert.equal(steered[0].content[0].text, "gates not done");

  const injected = [];
  await turnStoppingOutcome({ blocked: undefined, contexts: ["fyi"], errors: [] }, {
    inject: (m) => injected.push(m),
    steer: () => {
      throw new Error("steer must not run on the pass path");
    },
  });
  assert.equal(injected.length, 1);
});

test("prependStartContexts: enter gets the queue prepended in order and reports delivered", () => {
  const user = { role: "user", content: [{ type: "text", text: "hi" }] };
  const { decision, delivered } = prependStartContexts({ kind: "enter", messages: [user] }, ["a", "b"]);
  assert.equal(delivered, true);
  assert.deepEqual(
    decision.messages.map((m) => m.content[0].text),
    ["a", "b", "hi"],
  );
  assert.equal(decision.messages[0].source.kind, "plugin:forge-quality");
});

test("prependStartContexts: reject and an empty queue deliver nothing (queue is kept)", () => {
  const reject = { kind: "reject" };
  assert.deepEqual(prependStartContexts(reject, ["a"]), { decision: reject, delivered: false });
  const enter = { kind: "enter", messages: [] };
  assert.deepEqual(prependStartContexts(enter, []), { decision: enter, delivered: false });
});

test("prependStartContexts: an enter without messages still delivers (no TypeError)", () => {
  const { decision, delivered } = prependStartContexts({ kind: "enter" }, ["a"]);
  assert.equal(delivered, true);
  assert.deepEqual(decision.messages.map((m) => m.content[0].text), ["a"]);
});

/** Scripted batch runner: records (event, matcher) and answers from a table keyed "event|matcher". */
const fakeBatch = (table, calls = []) => async (event, matcher) => {
  calls.push(`${event}|${matcher}`);
  return table[`${event}|${matcher}`] ?? { block: false };
};

test("runMatchedGroups: one batch spawn per matched group, contexts and block come from its verdict", async () => {
  const calls = [];
  const outcome = await runMatchedGroups(
    "PreToolUse",
    [{ matcher: "Bash", commands: ["forge hook a", "forge hook b"] }],
    {},
    { batchRunner: fakeBatch({ "PreToolUse|Bash": { block: false, context: "merged" } }, calls), batchState: { broken: false } },
  );
  assert.deepEqual(calls, ["PreToolUse|Bash"]);
  assert.deepEqual(outcome.contexts, ["merged"]);
  assert.equal(outcome.blocked, undefined);
  assert.equal(typeof outcome.durationMs, "number");

  const blocked = await runMatchedGroups(
    "PreToolUse",
    [{ matcher: "Write|Edit", commands: ["forge hook a"] }],
    {},
    { batchRunner: fakeBatch({ "PreToolUse|Write|Edit": { block: true, reason: "no task" } }), batchState: { broken: false } },
  );
  assert.equal(blocked.blocked.reason, "no task");
  assert.equal(blocked.blocked.command, "batch PreToolUse/Write|Edit", "block is attributed to the group, the only granularity batch exposes");
});

test("runMatchedGroups: several matched groups run in order and the first block short-circuits the rest", async () => {
  const calls = [];
  const outcome = await runMatchedGroups(
    "PostToolUse",
    [
      { matcher: "g1", commands: [] },
      { matcher: "g2", commands: [] },
      { matcher: "g3", commands: [] },
    ],
    {},
    {
      batchRunner: fakeBatch({ "PostToolUse|g1": { block: false, context: "c1" }, "PostToolUse|g2": { block: true, reason: "stop" } }, calls),
      batchState: { broken: false },
    },
  );
  assert.deepEqual(calls, ["PostToolUse|g1", "PostToolUse|g2"]);
  assert.deepEqual(outcome.contexts, ["c1"]);
  assert.equal(outcome.blocked.reason, "stop");
});

test("runMatchedGroups: a batch that cannot answer (old forge: block with empty stdout) falls back to the serial roster — never a silent allow", async () => {
  const batchState = { broken: false };
  const serial = [];
  const outcome = await runMatchedGroups(
    "PreToolUse",
    [{ matcher: "Write|Edit", commands: ["forge hook freeze-guard", "forge hook task-guard"] }],
    {},
    {
      batchRunner: fakeBatch({ "PreToolUse|Write|Edit": { block: false, error: "forge exited with code 2 and no output" } }),
      runner: async (command) => {
        serial.push(command);
        return command === "forge hook task-guard" ? { block: true, reason: "BLOCKED: no task" } : { block: false };
      },
      batchState,
    },
  );
  assert.equal(outcome.blocked.reason, "BLOCKED: no task", "the serial retry still enforces the gate");
  assert.deepEqual(serial, ["forge hook freeze-guard", "forge hook task-guard"]);
  assert.equal(batchState.broken, true, "later groups skip batch entirely");
  assert.ok(outcome.errors.some((e) => e.includes("batch unsupported")), "the downgrade is visible in the outcome");

  // once broken, batch is not even attempted
  const calls = [];
  await runMatchedGroups("PreToolUse", [{ matcher: "Bash", commands: ["forge hook a"] }], {}, {
    batchRunner: fakeBatch({}, calls),
    runner: async () => ({ block: false }),
    batchState,
  });
  assert.deepEqual(calls, []);
});

test("runMatchedGroups: a timeout is NOT a batch failure — no serial retry, fail open with the error noted", async () => {
  const batchState = { broken: false };
  const serial = [];
  const outcome = await runMatchedGroups("PreToolUse", [{ matcher: "Bash", commands: ["forge hook a"] }], {}, {
    batchRunner: fakeBatch({ "PreToolUse|Bash": { block: false, error: "timeout after 30000ms" } }),
    runner: async (command) => {
      serial.push(command);
      return { block: false };
    },
    batchState,
  });
  assert.deepEqual(serial, [], "re-running a group that just timed out would only double the stall");
  assert.equal(batchState.broken, false);
  assert.deepEqual(outcome.errors, ["batch PreToolUse/Bash: timeout after 30000ms"]);
  assert.equal(outcome.blocked, undefined);
});

test("runMatchedGroups: batch:false runs the roster serially from the start", async () => {
  const serial = [];
  const outcome = await runMatchedGroups("PreToolUse", [{ matcher: "Bash", commands: ["forge hook a", "forge hook b"] }], {}, {
    batch: false,
    batchRunner: async () => {
      throw new Error("batch must not run");
    },
    runner: async (command) => {
      serial.push(command);
      return command === "forge hook b" ? { block: false, context: "from b" } : { block: false };
    },
  });
  assert.deepEqual(serial, ["forge hook a", "forge hook b"]);
  assert.deepEqual(outcome.contexts, ["from b"]);
});

test("dropQuietNotes: status-only receipts are dropped, real advisories and unknown notes are kept", () => {
  // the receipts forge prints when there is nothing to say (observed as synthetic user messages)
  for (const quiet of [
    "[assertion-check] no weakening detected (advisory)",
    "[auto-compile] no source touched (compile self-check delegated to agent)",
    "[auto-compile] research-mode session, advisory suppressed (set by Edit|Write of source)",
    "[skill-scan] all skills SAFE (advisory, 21 rules)",
    "[skill-scan] no ~/.claude/skills (advisory)",
    "[mcp-scan] no .mcp.json (advisory)",
    "[mcp-scan] no git project (no project-level .mcp.json to scan, advisory)",
    "[workflow-test-guard] go not on PATH, skipping internal/ci guard tests",
  ]) {
    assert.equal(dropQuietNotes(quiet), "", `should drop: ${quiet}`);
  }
  // anything that tells the model or the user to DO something stays
  for (const loud of [
    "[auto-compile] Advisory: 已修改源码——请用你技术栈的编译命令确认编译通过",
    "[skill-scan] Advisory: 发现风险 skill——x请核查",
    "[forge-session] 本项目由 forge 管理（managed）",
    "[init-suggest] Advisory: 检测到 git 项目 'x' 未启用 forge。请询问用户",
    "[bash-guard] WARN: Bash write without active task",
    "a note from a hook this list has never heard of",
  ]) {
    assert.equal(dropQuietNotes(loud), loud, `should keep: ${loud}`);
  }
});

test("dropQuietNotes: works per paragraph — a merged batch context keeps its real parts in order", () => {
  const merged = [
    "[skill-scan] all skills SAFE (advisory, 21 rules)",
    "[forge-session] 本项目由 forge 管理（managed）",
    "[mcp-scan] no .mcp.json (advisory)",
    "[forge] conventions: stack go",
  ].join("\n\n");
  assert.equal(dropQuietNotes(merged), "[forge-session] 本项目由 forge 管理（managed）\n\n[forge] conventions: stack go");
  // a multi-line paragraph is judged as a whole (its first line carries the tag)
  assert.equal(dropQuietNotes("[forge] conventions: line one\n- line two\n- line three"), "[forge] conventions: line one\n- line two\n- line three");
});

test("runMatchedGroups: quiet receipts never reach the outcome's contexts, in batch or serial", async () => {
  const batch = await runMatchedGroups("PreToolUse", [{ matcher: "Write|Edit", commands: [] }], {}, {
    batchRunner: fakeBatch({
      "PreToolUse|Write|Edit": { block: false, context: "[assertion-check] no weakening detected (advisory)\n\n[forge] conventions: x" },
    }),
    batchState: { broken: false },
  });
  assert.deepEqual(batch.contexts, ["[forge] conventions: x"]);

  const onlyQuiet = await runMatchedGroups("PreToolUse", [{ matcher: "Bash", commands: [] }], {}, {
    batchRunner: fakeBatch({ "PreToolUse|Bash": { block: false, context: "[mcp-scan] no .mcp.json (advisory)" } }),
    batchState: { broken: false },
  });
  assert.deepEqual(onlyQuiet.contexts, [], "a context that is nothing but receipts disappears entirely");

  const serial = await runMatchedGroups("PreToolUse", [{ matcher: "Bash", commands: ["forge hook a", "forge hook b"] }], {}, {
    batch: false,
    runner: async (command) =>
      command === "forge hook a"
        ? { block: false, context: "[skill-scan] all skills SAFE (advisory, 21 rules)" }
        : { block: false, context: "[bash-guard] WARN: x" },
  });
  assert.deepEqual(serial.contexts, ["[bash-guard] WARN: x"]);
});
