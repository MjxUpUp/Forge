/**
 * @agent_forge/forge-dsh — decision folding (pure logic, no I/O).
 *
 * One forge hook group (a spec event's matched commands) runs serially and
 * collapses to `{ blocked?, contexts, errors }`:
 *   - the first block wins and short-circuits the rest of the group (the
 *     spec's own ordering contract — freeze-guard runs first precisely so its
 *     deny pre-empts task-guard's);
 *   - allow-path additionalContext strings accumulate in order;
 *   - infrastructure failures (fail-open verdicts) are collected for
 *     observability but never influence the outcome.
 *
 * The collapse then maps onto DSH's typed decisions per interception point
 * (see index.js): deny for tools/pre-execute, block+feedback for
 * tools/post-execute, reject for agent/pre-step, steering for
 * agent/turn-stopping.
 *
 * @module decisions
 */

import { runForgeHook } from "./runner.js";
import { pluginMessage } from "./tools.js";

/**
 * @typedef {object} GroupOutcome
 * @property {{reason: string, command: string}|undefined} blocked
 * @property {string[]} contexts - allow-path additionalContext, in order.
 * @property {string[]} errors   - fail-open infrastructure notes.
 */

/**
 * Run one matched hook group serially against one payload.
 *
 * @param {string[]} commands - `forge hook <name>` commands, spec order.
 * @param {object} payload    - Claude-Code-shape stdin payload (shared by the
 *                              whole group, as in Claude Code).
 * @param {object} [opts]     - forwarded to runForgeHook (forgeBin/timeoutMs/cwd).
 * @returns {Promise<GroupOutcome>}
 */
export async function runHookGroup(commands, payload, opts = {}) {
  const run = opts.runner ?? runForgeHook; // DI seam for tests
  const outcome = { blocked: undefined, contexts: [], errors: [] };
  for (const command of commands) {
    const verdict = await run(command, payload, opts);
    if (verdict.error !== undefined) outcome.errors.push(`${command}: ${verdict.error}`);
    if (verdict.block) {
      outcome.blocked = { reason: verdict.reason ?? "denied", command };
      break;
    }
    if (verdict.context !== undefined) outcome.contexts.push(verdict.context);
  }
  return outcome;
}

/**
 * tools/pre-execute: forge block → deny; otherwise delegate. Allow-path
 * context has no channel on PreToolDecision (allow carries nothing), so it is
 * queued into the agent's next pre-step via agent.inject — the seam dsh-agent
 * documents for exactly this ("Queue model-facing context for the next
 * pre-step without waking the driver").
 *
 * @param {GroupOutcome} outcome
 * @param {object|undefined} agent
 * @param {() => Promise<object>} next
 * @returns {Promise<object>} PreToolDecision
 */
export async function preExecuteDecision(outcome, agent, next) {
  if (outcome.blocked !== undefined) {
    return { kind: "deny", reason: outcome.blocked.reason };
  }
  injectContexts(agent, outcome.contexts);
  return next();
}

/**
 * tools/post-execute: forge block → block with the reason as corrective
 * feedback (the post-execute "block turns corrective feedback into an error
 * result" channel). The block variant of PostToolDecision carries NO
 * additionalContexts (that field belongs to accept), so contexts gathered
 * before the block are queued via agent.inject instead of being attached to
 * a decision shape the runtime would drop (or worse, reject on a stricter
 * schema check). Allow-path context folds into the downstream decision AFTER
 * delegating (delegate-then-prepend: returning an enter-style decision
 * without next() would short-circuit every later listener).
 *
 * @param {GroupOutcome} outcome
 * @param {() => Promise<object>} next
 * @param {object|undefined} [agent] - for the block-path context fallback.
 * @returns {Promise<object>} PostToolDecision
 */
export async function postExecuteDecision(outcome, next, agent) {
  if (outcome.blocked !== undefined) {
    injectContexts(agent, outcome.contexts);
    return {
      kind: "block",
      feedback: [{ type: "text", text: outcome.blocked.reason }],
    };
  }
  const decision = await next();
  if (outcome.contexts.length === 0) return decision;
  return {
    ...decision,
    additionalContexts: [
      ...outcome.contexts.map(pluginMessage),
      ...(decision?.additionalContexts ?? []),
    ],
  };
}

/**
 * agent/pre-step (UserPromptSubmit): forge block → reject the step. PreStepDecision's
 * reject variant carries no reason, so the gate's corrective feedback (and any
 * same-group contexts) is queued via agent.inject first — on Claude Code the
 * UserPromptSubmit block reason reaches the user, and it must not silently
 * vanish here. Allow-path context prepends plugin messages into the
 * downstream enter decision (reject passes through untouched).
 *
 * @param {GroupOutcome} outcome
 * @param {() => Promise<object>} next
 * @param {object|undefined} [agent] - for the reject-path reason/context fallback.
 * @returns {Promise<object>} PreStepDecision
 */
export async function preStepDecision(outcome, next, agent) {
  if (outcome.blocked !== undefined) {
    injectContexts(agent, [outcome.blocked.reason, ...outcome.contexts]);
    return { kind: "reject" };
  }
  const decision = await next();
  if (outcome.contexts.length === 0 || decision?.kind !== "enter") return decision;
  return {
    kind: "enter",
    messages: [...outcome.contexts.map(pluginMessage), ...decision.messages],
  };
}

/**
 * agent/turn-stopping (Stop): forge block steers the agent into another step
 * (dsh-agent: "a listener that objects steers and the machine re-reads its
 * inbox: fresh steering runs another step, none closes the turn"); allow-path
 * context is queued via inject.
 *
 * NOTE: DSH rc.7 has no stop-hook loop guard (the official bridges always
 * report stop_hook_active:false for the same reason). forge's Stop hooks are
 * gate-driven and self-limiting (a passing gate stops blocking), but a
 * permanently-failing gate will steer every stop boundary — surfaced in the
 * README as a known watch item.
 *
 * @param {GroupOutcome} outcome
 * @param {object} agent
 * @returns {Promise<void>}
 */
export async function turnStoppingOutcome(outcome, agent) {
  if (outcome.blocked !== undefined) {
    agent?.steer?.(pluginMessage(outcome.blocked.reason));
    return;
  }
  injectContexts(agent, outcome.contexts);
}

/**
 * agent/session-start (emit): every collected context is queued via inject.
 * Best-effort by design — emit listeners are detached, so context may miss
 * the first request of a short-lived session (the official bridges document
 * the same TODO(session-start-gating) limitation).
 *
 * @param {GroupOutcome} outcome
 * @param {object} agent
 */
export function sessionStartOutcome(outcome, agent) {
  injectContexts(agent, outcome.contexts);
}

/** Queue each context string as one plugin-sourced message. */
function injectContexts(agent, contexts) {
  if (contexts.length === 0) return;
  const inject = agent?.inject;
  if (typeof inject !== "function") return;
  for (const text of contexts) {
    try {
      inject.call(agent, pluginMessage(text));
    } catch {
      // a throwing inject must never interrupt the session (bridge isolation rule)
    }
  }
}
