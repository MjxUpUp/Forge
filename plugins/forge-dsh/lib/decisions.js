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

import { runForgeBatch, runForgeHook } from "./runner.js";
import { pluginMessage } from "./tools.js";

/**
 * @typedef {object} GroupOutcome
 * @property {{reason: string, command: string}|undefined} blocked
 * @property {string[]} contexts - allow-path additionalContext, in order.
 * @property {string[]} errors   - fail-open infrastructure notes.
 * @property {number} [durationMs] - wall time of the group run (for /forge-status).
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
  const started = Date.now();
  const outcome = { blocked: undefined, contexts: [], errors: [], durationMs: 0 };
  for (const command of commands) {
    const verdict = await run(command, payload, opts);
    if (verdict.error !== undefined) outcome.errors.push(`${command}: ${verdict.error}`);
    if (verdict.block) {
      outcome.blocked = { reason: verdict.reason ?? "denied", command };
      break;
    }
    if (verdict.context !== undefined) outcome.contexts.push(verdict.context);
  }
  outcome.durationMs = Date.now() - started;
  return outcome;
}

/**
 * Status-only receipts forge prints when it has nothing to say ("no weakening
 * detected", "all skills SAFE", "no .mcp.json" …). The host injects every
 * allow-path context into the conversation as a synthetic user message, so each
 * of these costs the model a turn-sized distraction for zero information. Only
 * receipts confirmed to carry no instruction are listed; an allowlist of "what
 * the model must see" would silently drop any new advisory, so unknown notes
 * pass through. A note that tells anyone to DO something carries `Advisory:` or
 * is a directive and never matches here.
 *
 * forge 在「无事可报」时输出的纯状态回执。宿主把 allow 路径的上下文注入成合成 user
 * 消息，每条都白白占用模型一次注意力。只列已确认不含任何指令的回执；未知的一律放行
 * （白名单会静默吞掉新增的 advisory）。要求任何人做事的提示带 `Advisory:` 或是指令，
 * 不会命中这里。
 */
const QUIET_NOTES = [
  /^\[assertion-check\] no weakening detected/,
  /^\[auto-compile\] (?:no source touched|research-mode session)/,
  /^\[skill-scan\] (?:all skills SAFE|no ~\/\.claude\/skills)/,
  /^\[mcp-scan\] (?:no |\.mcp\.json unreadable|项目级 \.mcp\.json 无 config 层风险信号)/,
  /^\[workflow-test-guard\] /,
];

/**
 * Remove the quiet receipts from one context string. Judged per paragraph (a
 * batch merges the group's contexts with a blank line between them); a paragraph
 * is quiet when its first line matches. Returns "" when nothing is left.
 *
 * @param {string} text
 * @returns {string}
 */
export function dropQuietNotes(text) {
  return text
    .split("\n\n")
    .filter((paragraph) => !QUIET_NOTES.some((re) => re.test(paragraph)))
    .join("\n\n");
}

/**
 * Run the matched spec groups of one event — one `forge hook batch` process per
 * group instead of one process per hook (the spec's matcher only SELECTS the
 * group; forge owns the roster, so plugin/forge roster drift cannot desync).
 *
 * Fail-open stays the invariant, with one guard: a batch that cannot answer is
 * never taken as an allow. Forges older than the batch stdout fix print nothing
 * on a block (exit 2, reason on stderr only), which the runner reports as
 * "exited with code 2 and no output" — an error verdict that looks exactly like a
 * silent allow. So any non-timeout batch error re-runs THAT group serially (the
 * legacy per-hook path, whose contract every forge version honours) and flips
 * `opts.batchState.broken` so later groups skip batch. A timeout is the one
 * error that does not fall back: the serial re-run would only repeat the stall.
 *
 * `opts.batch === false` forces serial from the start.
 *
 * @param {string} event - forge event name.
 * @param {Array<{matcher: string, commands: string[]}>} groups - matched spec groups, spec order.
 * @param {object} payload - Claude-Code-shape stdin payload.
 * @param {object} [opts] - forgeBin/timeoutMs plus test seams `batchRunner`, `runner`, `batchState`.
 * @returns {Promise<GroupOutcome>}
 */
export async function runMatchedGroups(event, groups, payload, opts = {}) {
  const started = Date.now();
  const batchRun = opts.batchRunner ?? runForgeBatch; // DI seam for tests
  const state = opts.batchState ?? { broken: false };
  const outcome = { blocked: undefined, contexts: [], errors: [], durationMs: 0 };
  for (const group of groups) {
    const label = `batch ${event}${group.matcher === "" ? "" : `/${group.matcher}`}`;
    let part;
    if (opts.batch !== false && !state.broken) {
      const verdict = await batchRun(event, group.matcher, payload, opts);
      if (verdict.error === undefined) {
        part = {
          blocked: verdict.block ? { reason: verdict.reason ?? "denied", command: label } : undefined,
          contexts: verdict.context === undefined ? [] : [verdict.context],
          errors: [],
        };
      } else if (verdict.error.startsWith("timeout after")) {
        part = { blocked: undefined, contexts: [], errors: [`${label}: ${verdict.error}`] };
      } else {
        state.broken = true;
        outcome.errors.push(`${label}: ${verdict.error} — batch unsupported by this forge, running hooks one by one`);
      }
    }
    if (part === undefined) part = await runHookGroup(group.commands, payload, opts);
    outcome.errors.push(...part.errors);
    outcome.contexts.push(...part.contexts.map(dropQuietNotes).filter((text) => text !== ""));
    if (part.blocked !== undefined) {
      outcome.blocked = part.blocked;
      break;
    }
  }
  outcome.durationMs = Date.now() - started;
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
 * agent/session-start (emit) contexts ride the NEXT pre-step instead of
 * agent.inject. Emit listeners are detached and inject at session start was
 * observed to drop the contexts silently (audit 2026-10-20: all four lost,
 * not delayed, while pre-step messages in the same session were delivered), so
 * the queued strings are prepended into the first `enter` decision. A reject
 * (or any non-enter decision) leaves the queue to the caller — it must retain
 * the strings for the next pre-step.
 *
 * 会话启动的上下文改走下一次 pre-step 的 enter 决策（而非 agent.inject）：
 * 实测 inject 在会话启动时静默丢弃，pre-step 消息则稳定送达。
 *
 * @param {object} decision - PreStepDecision (after preStepDecision folding).
 * @param {string[]} pending - queued session-start contexts, delivery order.
 * @returns {{decision: object, delivered: boolean}} delivered=true means the
 *   caller may clear the queue.
 */
export function prependStartContexts(decision, pending) {
  if (pending.length === 0 || decision?.kind !== "enter") {
    return { decision, delivered: false };
  }
  return {
    decision: { ...decision, messages: [...pending.map(pluginMessage), ...(decision.messages ?? [])] },
    delivered: true,
  };
}

/** Queue each context string as one producer-attributed user message. */
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
