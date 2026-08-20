/**
 * @agent_forge/forge-dsh — Forge quality gates for DeepSeek Harness.
 *
 * Wires forge's Claude-Code hook roster (mirrored in lib/spec.json from
 * internal/hooks/settings.go ForgeHookSpec — drift-guarded by a Go test in
 * the Forge repo) onto DSH's typed interception points:
 *
 *   tools/pre-execute   → PreToolUse  (block → {kind:'deny'})
 *   tools/post-execute  → PostToolUse (block → {kind:'block', feedback})
 *   agent/pre-step      → UserPromptSubmit (block → {kind:'reject'})
 *   agent/session-start → SessionStart (emit; source 'compact' also fires
 *                         the PostCompact group — DSH rc.7 exposes no
 *                         separate compaction point)
 *   agent/turn-stopping → Stop        (block → agent.steer(reason))
 *
 * Each hook runs as `forge hook <name>` with a Claude-Code-shape stdin
 * payload and speaks forge's claude stdout dialect back (block read from the
 * JSON decision field; every infrastructure failure fails open — a forge
 * outage never locks the agent out of its tools). Verified against
 * @deepseek-ai/dsh 0.1.0-rc.7 type surface.
 *
 * Config (profile patch):
 *   forgeBin  - forge binary name/path (default "forge", env FORGE_BIN wins over default)
 *   timeoutMs - per-hook ceiling, kill + fail open (default 30000)
 *   enabled   - false disables every listener (default true)
 *   debug     - log hook failures to console.error (default false)
 *
 * @module @agent_forge/forge-dsh
 */
import { readFileSync } from "node:fs";
import { runHookGroup, preExecuteDecision, postExecuteDecision, preStepDecision, turnStoppingOutcome, sessionStartOutcome } from "./decisions.js";
import { buildEventPayload, buildToolPayload, matchedCommands, promptText, toCCToolName } from "./tools.js";

const spec = JSON.parse(readFileSync(new URL("./spec.json", import.meta.url), "utf8"));

/** Stable Cordis plugin name. */
const name = "forge-quality";
/** Hard dependency: the tool registry (pre/post-execute waterfalls). */
const inject = ["tools"];

/**
 * @param {object} ctx - Cordis plugin context.
 * @param {object} [config] - { forgeBin?, timeoutMs?, enabled?, debug? }.
 */
function apply(ctx, config = {}) {
  if (config.enabled === false) return;
  const opts = {
    forgeBin: config.forgeBin ?? process.env.FORGE_BIN ?? "forge",
    timeoutMs: config.timeoutMs ?? 30000,
  };
  const debug = config.debug === true;
  // Recent-run ring buffer for /forge-status — fail-open verdicts are silent
  // by design, so this (plus debug logging) is the only way to see them.
  const recentRuns = [];
  const noteErrors = (label, outcome) => {
    recentRuns.push({
      label,
      blocked: outcome.blocked?.command ?? null,
      contexts: outcome.contexts.length,
      errors: outcome.errors,
      at: Date.now(),
    });
    if (recentRuns.length > 50) recentRuns.shift();
    if (debug && outcome.errors.length > 0) {
      console.error(`[forge-dsh] ${label} fail-open: ${outcome.errors.join("; ")}`);
    }
  };

  // PreToolUse — deny short-circuits the tool call.
  ctx.effect(() => ctx.on("tools/pre-execute", async (exec, next) => {
    const commands = matchedCommands(spec.PreToolUse, toCCToolName(exec?.name ?? ""));
    if (commands.length === 0) return next();
    const outcome = await runHookGroup(commands, buildToolPayload(exec, "PreToolUse"), opts);
    noteErrors("pre-execute", outcome);
    return preExecuteDecision(outcome, exec?.agent, next);
  }), "forge: pre-execute");

  // PostToolUse — block turns the gate's reason into an error result.
  ctx.effect(() => ctx.on("tools/post-execute", async (exec, result, next) => {
    const commands = matchedCommands(spec.PostToolUse, toCCToolName(exec?.name ?? ""));
    if (commands.length === 0) return next();
    const outcome = await runHookGroup(commands, buildToolPayload(exec, "PostToolUse"), opts);
    noteErrors("post-execute", outcome);
    return postExecuteDecision(outcome, next, exec?.agent);
  }), "forge: post-execute");

  // UserPromptSubmit — the prompt rides the step's claimed messages.
  ctx.effect(() => ctx.on("agent/pre-step", async (payload, next) => {
    const commands = matchedCommands(spec.UserPromptSubmit, "");
    if (commands.length === 0) return next();
    const eventPayload = buildEventPayload("UserPromptSubmit", payload?.agent, {
      prompt: promptText(payload?.messages),
    });
    const outcome = await runHookGroup(commands, eventPayload, opts);
    noteErrors("pre-step", outcome);
    return preStepDecision(outcome, next, payload?.agent);
  }), "forge: pre-step");

  // SessionStart — emit mode: detached, contexts queued via agent.inject.
  // source 'compact' doubles as PostCompact (DSH exposes no dedicated point).
  ctx.effect(() => ctx.on("agent/session-start", (payload) => {
    const agent = payload?.agent;
    const source = payload?.source;
    (async () => {
      const start = await runHookGroup(
        matchedCommands(spec.SessionStart, ""),
        buildEventPayload("SessionStart", agent, { source }),
        opts,
      );
      noteErrors("session-start", start);
      sessionStartOutcome(start, agent);
      if (source === "compact") {
        const compact = await runHookGroup(
          matchedCommands(spec.PostCompact, ""),
          buildEventPayload("PostCompact", agent, {}),
          opts,
        );
        noteErrors("post-compact", compact);
        sessionStartOutcome(compact, agent);
      }
    })().catch((error) => {
      if (debug) console.error(`[forge-dsh] session-start failed open: ${error}`);
    });
  }), "forge: session-start");

  // Stop — a blocking gate steers the agent into another step.
  ctx.effect(() => ctx.on("agent/turn-stopping", async (payload) => {
    const agent = payload?.agent;
    const outcome = await runHookGroup(
      matchedCommands(spec.Stop, ""),
      buildEventPayload("Stop", agent, { stop_hook_active: false }),
      opts,
    );
    noteErrors("turn-stopping", outcome);
    return turnStoppingOutcome(outcome, agent);
  }), "forge: turn-stopping");

  // /forge-status — wired groups + recent runs (the only place fail-open
  // infrastructure errors surface without debug:true).
  const commands = ctx.get("commands");
  if (commands !== undefined) {
    ctx.effect(() => commands.register({
      name: "forge-status",
      description: "Forge quality gates: wired hook groups and recent runs",
      handler: async () => {
        const groups = Object.entries(spec)
          .map(([event, gs]) => `  ${event}: ${gs.flatMap((g) => g.hooks.map((h) => h.command.replace("forge hook ", ""))).join(", ")}`)
          .join("\n");
        const runs = recentRuns.length === 0
          ? "  (no hook runs yet)"
          : recentRuns.slice(-10).map((r) => {
              const parts = [`  ${new Date(r.at).toISOString()} ${r.label}`];
              if (r.blocked) parts.push(`blocked-by=${r.blocked}`);
              if (r.contexts > 0) parts.push(`contexts=${r.contexts}`);
              if (r.errors.length > 0) parts.push(`FAIL-OPEN: ${r.errors.join("; ")}`);
              return parts.join("  ");
            }).join("\n");
        return {
          kind: "success",
          text: `# Forge quality gates\n\nforgeBin: ${opts.forgeBin}  timeoutMs: ${opts.timeoutMs}\n\n## Wired groups\n${groups}\n\n## Recent runs (last 10)\n${runs}`,
        };
      },
    }), "forge: status command");
  }
}

export { apply, inject, name };
