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
 *                         separate compaction point). Its contexts are queued
 *                         per agent and delivered with the next pre-step.
 *   agent/turn-stopping → Stop        (block → agent.steer(reason))
 *
 * Each hook runs as `forge hook <name>` with a Claude-Code-shape stdin
 * payload and speaks forge's claude stdout dialect back (block read from the
 * JSON decision field; every infrastructure failure fails open — a forge
 * outage never locks the agent out of its tools). Verified against
 * @deepseek-ai/dsh 0.1.0-rc.7 type surface.
 *
 * Config (profile patch):
 *   forgeBin  - forge binary name/path. Precedence: config > env FORGE_BIN >
 *               the forge bundled with this plugin (dependency, version-locked) >
 *               "forge" on PATH
 *   timeoutMs - per-hook ceiling, kill + fail open (default 30000)
 *   welcome   - false drops the one-line first-turn onboarding (default true)
 *   startWaitMs - how long the first pre-step waits for the session-start
 *               contexts before moving on without them (default 1500)
 *   enabled   - false disables every listener (default true)
 *   debug     - log hook failures to console.error (default false)
 *
 * @module @agent_forge/forge-dsh
 */
import { readFileSync } from "node:fs";
import { bundledForgeHint, resolveBundledForge } from "./bundled.js";
import { probeForge } from "./runner.js";
import { runMatchedGroups, preExecuteDecision, postExecuteDecision, preStepDecision, prependStartContexts, turnStoppingOutcome } from "./decisions.js";
import { buildEventPayload, buildToolPayload, matchedGroups, promptText, toCCToolName, welcomeText, isSubagent, MANAGED_BANNER } from "./tools.js";

const spec = JSON.parse(readFileSync(new URL("./spec.json", import.meta.url), "utf8"));
const pluginVersion = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8")).version;

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
  const explicitBin = config.forgeBin ?? process.env.FORGE_BIN;
  let bundledBin;
  if (explicitBin === undefined) {
    try {
      bundledBin = (config.resolveBundledForge ?? resolveBundledForge)(); // config seam: tests
    } catch {
      bundledBin = undefined; // an unresolvable bundle must never break plugin load
    }
  }
  let binSource = explicitBin !== undefined ? "configured" : bundledBin !== undefined ? "bundled" : "PATH";
  const opts = {
    forgeBin: explicitBin ?? bundledBin ?? "forge",
    timeoutMs: config.timeoutMs ?? 30000,
    // One `forge hook batch` process per matcher group instead of one per hook (~half the
    // per-tool-call latency). batch:false forces the legacy per-hook path; a forge that cannot
    // answer batch flips batchState.broken and the plugin degrades to per-hook on its own.
    batch: config.batch !== false,
    batchState: { broken: false },
  };
  const debug = config.debug === true;
  const welcome = config.welcome !== false;
  const startWaitMs = Number.isFinite(config.startWaitMs) && config.startWaitMs >= 0 ? config.startWaitMs : 1500;
  // Recent-run ring buffer for /forge-status — fail-open verdicts are silent
  // by design, so this (plus debug logging) is the only way to see them.
  const recentRuns = [];
  const noteErrors = (label, outcome) => {
    recentRuns.push({
      label,
      blocked: outcome.blocked?.command ?? null,
      contexts: outcome.contexts.length,
      errors: outcome.errors,
      ms: outcome.durationMs ?? 0,
      at: Date.now(),
    });
    if (recentRuns.length > 50) recentRuns.shift();
    if (debug && outcome.errors.length > 0) {
      console.error(`[forge-dsh] ${label} fail-open: ${outcome.errors.join("; ")}`);
    }
  };

  // Session-start contexts (forge's own onboarding + the missing-forge notice)
  // are queued per agent and prepended into that agent's next pre-step.
  // agent.inject at session start was observed to drop them silently, while
  // pre-step messages in the same session were delivered. The session-start
  // run is detached and takes a few hundred ms, so the first pre-step may
  // arrive before it finishes: `ready` lets that pre-step wait a bounded time
  // (startWaitMs) instead of pushing the onboarding to turn 2.
  // Keyed by session id when the handle has one (the host may hand each event a
  // fresh wrapper object for the same session), by object identity otherwise.
  const startStates = new WeakMap();
  const startStatesById = new Map();
  const startStateOf = (agent) => {
    if (agent === null || typeof agent !== "object") return undefined;
    const id = agent.session?.id;
    const byId = typeof id === "string" && id !== "";
    let state = byId ? startStatesById.get(id) : startStates.get(agent);
    if (state === undefined) {
      state = { pending: [], ready: Promise.resolve(), waited: true };
      if (byId) startStatesById.set(id, state);
      else startStates.set(agent, state);
    }
    return state;
  };
  // Only the first pre-step after a session-start waits: a slow start run must
  // not tax every later step by startWaitMs; its contexts simply land on the
  // first enter after it finishes.
  const waitStart = async (state) => {
    if (state === undefined || state.waited) return;
    state.waited = true;
    let timer;
    const timeout = new Promise((resolve) => {
      timer = setTimeout(resolve, startWaitMs);
      timer.unref?.();
    });
    try {
      await Promise.race([state.ready, timeout]);
    } finally {
      clearTimeout(timer);
    }
  };

  // A missing forge binary used to be completely silent: every hook failed
  // open, so the plugin looked installed but gated nothing. Probe once per
  // session start (retrying on later sessions, since installing forge needs no
  // host restart) and surface the failure through the same start queue. Once a
  // probe succeeds the answer is cached for the process lifetime; forgeBin is
  // static config per plugin instance, so it cannot go stale.
  let forgeReady = false;
  let lastProbe;
  let probeInFlight;
  const missingForgeNotice = (error) =>
    `[forge-dsh] forge CLI not found (forgeBin: ${opts.forgeBin}). ` +
    "Forge gates are OFF for this session — every action runs unguarded (fail-open). " +
    "Reinstall the plugin (it bundles forge) or run `npm install -g @agent_forge/forge`, then start a new session; " +
    "if it lives elsewhere, set forgeBin in the profile patch." +
    (error === undefined ? "" : `\nProbe error: ${error}`);
  const ensureForge = async (agent) => {
    if (forgeReady) return true;
    if (probeInFlight === undefined) {
      probeInFlight = probeForge({ forgeBin: opts.forgeBin, timeoutMs: Math.min(opts.timeoutMs, 5000) })
        .finally(() => {
          probeInFlight = undefined;
        });
    }
    let probe = await probeInFlight;
    // A bundled binary that resolved but will not run (wrong arch, lost exec bit,
    // truncated download) must not void the gates when a working forge is on PATH.
    if (!probe.ok && binSource === "bundled") {
      const onPath = await probeForge({ forgeBin: "forge", timeoutMs: Math.min(opts.timeoutMs, 5000) });
      if (onPath.ok) {
        if (debug) console.error(`[forge-dsh] bundled forge unusable (${probe.error}); using PATH forge`);
        opts.forgeBin = "forge";
        binSource = "PATH";
        probe = onPath;
      }
    }
    lastProbe = probe;
    if (probe.ok) {
      forgeReady = true;
      return true;
    }
    if (debug) console.error(`[forge-dsh] forge probe failed: ${probe.error}`);
    const state = startStateOf(agent);
    if (state !== undefined && !state.noticed) {
      state.noticed = true;
      state.pending.push(missingForgeNotice(probe.error));
    }
    return false;
  };

  // PreToolUse — deny short-circuits the tool call.
  ctx.effect(() => ctx.on("tools/pre-execute", async (exec, next) => {
    const groups = matchedGroups(spec.PreToolUse, toCCToolName(exec?.name ?? ""));
    if (groups.length === 0) return next();
    const outcome = await runMatchedGroups("PreToolUse", groups, buildToolPayload(exec, "PreToolUse"), opts);
    noteErrors("pre-execute", outcome);
    return preExecuteDecision(outcome, exec?.agent, next);
  }), "forge: pre-execute");

  // PostToolUse — block turns the gate's reason into an error result.
  ctx.effect(() => ctx.on("tools/post-execute", async (exec, result, next) => {
    const groups = matchedGroups(spec.PostToolUse, toCCToolName(exec?.name ?? ""));
    if (groups.length === 0) return next();
    const outcome = await runMatchedGroups("PostToolUse", groups, buildToolPayload(exec, "PostToolUse"), opts);
    noteErrors("post-execute", outcome);
    return postExecuteDecision(outcome, next, exec?.agent);
  }), "forge: post-execute");

  // UserPromptSubmit — the prompt rides the step's claimed messages.
  ctx.effect(() => ctx.on("agent/pre-step", async (payload, next) => {
    const state = startStateOf(payload?.agent);
    const startWait = waitStart(state);
    const groups = matchedGroups(spec.UserPromptSubmit, "");
    let decision;
    if (groups.length === 0) {
      decision = await next();
    } else {
      const eventPayload = buildEventPayload("UserPromptSubmit", payload?.agent, {
        prompt: promptText(payload?.messages),
      });
      const outcome = await runMatchedGroups("UserPromptSubmit", groups, eventPayload, opts);
      noteErrors("pre-step", outcome);
      decision = await preStepDecision(outcome, next, payload?.agent);
    }
    await startWait;
    if (state === undefined) return decision;
    // a reject keeps the queue for the next pre-step; only an enter clears it
    const delivery = prependStartContexts(decision, state.pending);
    if (delivery.delivered) state.pending.length = 0;
    return delivery.decision;
  }), "forge: pre-step");

  // SessionStart — emit mode: detached, contexts queued per agent and
  // delivered by the next pre-step (see startStates above).
  // source 'compact' doubles as PostCompact (DSH exposes no dedicated point).
  ctx.effect(() => ctx.on("agent/session-start", (payload) => {
    const agent = payload?.agent;
    const source = payload?.source;
    const state = startStateOf(agent);
    const queue = (outcome) => {
      if (state !== undefined) {
        // overlapping starts must not stack the same onboarding twice
        for (const text of outcome.contexts) if (!state.pending.includes(text)) state.pending.push(text);
      } else if (debug && outcome.contexts.length > 0) {
        console.error("[forge-dsh] session-start contexts dropped: no agent handle to queue them on");
      }
    };
    const run = (async () => {
      const forgeOk = await ensureForge(agent);
      // The bundled binary is not on PATH while forge's gate messages say
      // `forge task …`, so the agent is told where it is. After a compaction the
      // earlier hint is summarised away, so it is re-sent.
      if (source === "compact" && state !== undefined) state.hinted = false;
      if (forgeOk && binSource === "bundled" && state !== undefined && !state.hinted) {
        state.hinted = true;
        state.pending.push(bundledForgeHint(opts.forgeBin));
      }
      const start = await runMatchedGroups(
        "SessionStart",
        matchedGroups(spec.SessionStart, ""),
        buildEventPayload("SessionStart", agent, { source }),
        opts,
      );
      noteErrors("session-start", start);
      queue(start);
      // Welcome — queue order: welcome, path hint, forge's own contexts. One
      // human-facing line per session: startup only (resume/compact are not a
      // first meeting), never for delegated children (they report to their
      // parent), and only when forge itself announced the project as managed —
      // a working binary alone does not mean the gates apply here (no `forge
      // init`, or `forge off`).
      const isStartup = source === undefined || source === "startup";
      const managed = start.contexts.some((text) => text.includes(MANAGED_BANNER));
      if (forgeOk && welcome && isStartup && managed && !isSubagent(agent) && state !== undefined && !state.welcomed) {
        state.welcomed = true;
        const forgeVersion = (lastProbe?.version?.match(/\d+\.\d+\.\d+/) ?? [])[0];
        state.pending.unshift(welcomeText(pluginVersion, forgeVersion));
      }
      if (source === "compact") {
        const compact = await runMatchedGroups(
          "PostCompact",
          matchedGroups(spec.PostCompact, ""),
          buildEventPayload("PostCompact", agent, {}),
          opts,
        );
        noteErrors("post-compact", compact);
        queue(compact);
      }
    })().catch((error) => {
      if (debug) console.error(`[forge-dsh] session-start failed open: ${error}`);
    });
    // overlapping session-starts (startup then compact) both gate the pre-step
    if (state !== undefined) {
      state.waited = false;
      state.ready = Promise.all([state.ready, run]);
    }
  }), "forge: session-start");

  // Stop — a blocking gate steers the agent into another step.
  ctx.effect(() => ctx.on("agent/turn-stopping", async (payload) => {
    const agent = payload?.agent;
    const outcome = await runMatchedGroups(
      "Stop",
      matchedGroups(spec.Stop, ""),
      buildEventPayload("Stop", agent, { stop_hook_active: false }),
      opts,
    );
    noteErrors("turn-stopping", outcome);
    return turnStoppingOutcome(outcome, agent);
  }), "forge: turn-stopping");

  // /forge-status — wired groups + verdict summary + recent runs (the only
  // place fail-open infrastructure errors surface without debug:true).
  const commands = ctx.get("commands");
  if (commands !== undefined) {
    ctx.effect(() => commands.register({
      name: "forge-status",
      description: "Forge quality gates: wired hook groups and recent runs",
      handler: async () => {
        const groups = Object.entries(spec)
          .map(([event, gs]) => `  ${event}: ${gs.flatMap((g) => g.hooks.map((h) => h.command.replace("forge hook ", ""))).join(", ")}`)
          .join("\n");
        // Verdict summary over the whole ring buffer (50 runs): the at-a-glance
        // health line — how many runs forge blocked, and how many hook runs
        // FAILED OPEN (infra errors, e.g. timeout) and thus lost that run's
        // feedback. A rising fail-open count on a big project means timeoutMs
        // needs raising.
        const summary = recentRuns.reduce(
          (acc, r) => {
            acc.runs += 1;
            if (r.blocked) acc.blocked += 1;
            if (r.errors.length > 0) acc.failOpen += 1;
            acc.contexts += r.contexts;
            return acc;
          },
          { runs: 0, blocked: 0, failOpen: 0, contexts: 0 },
        );
        const runs = recentRuns.length === 0
          ? "  (no hook runs yet)"
          : recentRuns.slice(-10).map((r) => {
              const parts = [`  ${new Date(r.at).toISOString()} ${r.label}`];
              if (r.blocked) parts.push(`blocked-by=${r.blocked}`);
              if (r.contexts > 0) parts.push(`contexts=${r.contexts}`);
              parts.push(`${r.ms}ms`);
              if (r.errors.length > 0) parts.push(`FAIL-OPEN: ${r.errors.join("; ")}`);
              return parts.join("  ");
            }).join("\n");
        // Per-interception-point wall time over the ring buffer: the number behind "why is every
        // tool call slow" (one batch spawn per group should sit well under the per-hook sum).
        const byLabel = new Map();
        for (const r of recentRuns) {
          const t = byLabel.get(r.label) ?? { runs: 0, total: 0, max: 0 };
          t.runs += 1;
          t.total += r.ms;
          t.max = Math.max(t.max, r.ms);
          byLabel.set(r.label, t);
        }
        const timing = byLabel.size === 0
          ? "  (no hook runs yet)"
          : [...byLabel].map(([label, t]) => `  ${label}  runs=${t.runs}  avg=${Math.round(t.total / t.runs)}ms  max=${t.max}ms`).join("\n");
        const dispatchLine = !opts.batch
          ? "per-hook (batch:false)"
          : opts.batchState.broken
            ? "per-hook (this forge cannot answer `forge hook batch` — upgrade forge for one process per group)"
            : "batch (one forge process per matcher group)";
        const probeLine = lastProbe === undefined
          ? "not run yet (no session start in this process)"
          : lastProbe.ok
            ? `ok${lastProbe.version === undefined ? "" : ` (${lastProbe.version.slice(0, 80)})`}`
            : `FAILED — gates inactive (${lastProbe.error})`;
        return {
          kind: "success",
          text: `# Forge quality gates\n\nforgeBin: ${opts.forgeBin} (${binSource})  timeoutMs: ${opts.timeoutMs}\ndispatch: ${dispatchLine}\nprobe: ${probeLine}\n\n## Wired groups\n${groups}\n\n## Verdict summary (last ${recentRuns.length} runs)\n  runs=${summary.runs}  blocked=${summary.blocked}  fail-open=${summary.failOpen}  contexts=${summary.contexts}\n\n## Timing per interception point (last ${recentRuns.length} runs)\n${timing}\n\n## Recent runs (last 10)\n${runs}`,
        };
      },
    }), "forge: status command");
  }
}

export { apply, inject, name };
