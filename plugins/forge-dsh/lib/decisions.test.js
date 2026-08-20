import test from "node:test";
import assert from "node:assert/strict";
import {
  postExecuteDecision,
  preExecuteDecision,
  preStepDecision,
  runHookGroup,
  sessionStartOutcome,
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
  assert.equal(injected[0].source.plugin, "forge-quality");
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

test("sessionStartOutcome queues every context; a throwing inject is swallowed", async () => {
  const injected = [];
  sessionStartOutcome({ blocked: undefined, contexts: ["a", "b"], errors: [] }, {
    inject: (m) => injected.push(m),
  });
  assert.equal(injected.length, 2);
  // never throws even when agent.inject does
  sessionStartOutcome({ blocked: undefined, contexts: ["a"], errors: [] }, {
    inject: () => {
      throw new Error("boom");
    },
  });
});
