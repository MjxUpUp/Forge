#!/usr/bin/env node
/**
 * Test double for the forge binary. Behaves per hook name (argv[2] of
 * `fake-forge hook <name>`), and — when FAKE_FORGE_LOG is set — appends one
 * JSON line per invocation ({hook, payload}) for payload assertions.
 *
 * `fake-forge hook batch --event E [--matcher M]` emulates forge's single-entry
 * dispatch: it runs the roster of that group (taken from the plugin's own
 * lib/spec.json) in order, stops at the first block and forwards that hook's
 * stdout verbatim, and merges allow-path contexts into one document — the
 * contract internal/hookdispatch/hook_batch.go implements. Each inner hook is
 * logged exactly like a single run; FAKE_FORGE_BATCH_LOG additionally records one
 * line per batch invocation, and FAKE_FORGE_BATCH_BROKEN=1 makes batch fail like
 * a forge that predates it. FAKE_FORGE_START_DELAY_MS makes SessionStart runs slow (before any
 * hook runs), so tests about "the detached session-start has not finished yet" do not depend on
 * how fast the machine is.
 *
 * Behavior table:
 *   task-guard      → block "BLOCKED: no active task" (exit 2, decision JSON)
 *   hazard-guard    → blocks when tool_input.command contains "rm -rf"
 *   skill-trigger   → pass with additionalContext
 *   block-it        → generic block
 *   garbage         → exit 1 with non-JSON stdout (cobra-style internal error)
 *   hang            → never settles (timeout path)
 *   everything else → silent pass (exit 0, no output)
 */
import { appendFileSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// `forge --version` is the liveness probe: exit before touching stdin (the
// real binary does not read stdin for --version either), and do not log —
// probe calls are not hook runs.
if (process.argv.includes("--version")) {
  // The plugin enables batch dispatch only for a forge of its own version, so the double reports
  // the plugin's version by default; FAKE_FORGE_VERSION simulates a mismatched forge.
  const own = JSON.parse(readFileSync(fileURLToPath(new URL("../../package.json", import.meta.url)), "utf8")).version;
  process.stdout.write(`forge version ${process.env.FAKE_FORGE_VERSION ?? own} (fake)\n`);
  process.exit(0);
}

// Simulates a forge that fails BEFORE reading stdin (unknown hook name after
// a spec/binary version drift, an early panic): exits immediately, leaving
// the parent's stdin write to hit EPIPE.
if (process.argv[3] === "early-exit") {
  process.exit(1);
}

/**
 * What one hook does for one payload → {out, err, code} (or {hang:true}).
 * Shared by the single-hook entry and the `batch` emulation, so both speak the
 * same stdout dialect — the contract the plugin parses.
 */
function behave(hook, payload) {
  const event = payload.hook_event_name ?? "";
  const silent = { out: "", err: "", code: 0 };
  const block = (reason) => ({
    out:
      JSON.stringify({
        decision: "block",
        reason,
        hookSpecificOutput: { hookEventName: event, additionalContext: reason },
      }) + "\n",
    err: reason + "\n",
    code: 2,
  });
  const context = (text) => ({
    out: JSON.stringify({ hookSpecificOutput: { hookEventName: event, additionalContext: text } }) + "\n",
    err: "",
    code: 0,
  });
  switch (hook) {
    case "task-guard":
      return block("BLOCKED: no active task (fake)");
    case "task-verify":
      return block("BLOCKED: task gates not done (fake)");
    case "resume-reinject":
      return String(payload?.prompt ?? "").includes("block-prompt") ? block("prompt rejected by gate (fake)") : silent;
    case "file-sentinel":
      return String(payload?.tool_input?.command ?? "").includes("quarantine-me")
        ? block("file-sentinel: unauthorized change (fake)")
        : silent;
    case "hazard-guard":
      return String(payload?.tool_input?.command ?? "").includes("rm -rf")
        ? block("hazard: rm -rf intercepted (fake)")
        : silent;
    case "skill-trigger":
      return context("consider the test-discipline skill");
    case "task-resume":
      // forge's [forge-session] managed banner (the project-managed signal). Opt-in via env so
      // the many exact-context assertions elsewhere are not perturbed by an extra start context.
      return process.env.FAKE_FORGE_MANAGED === "1"
        ? context("[forge-session] 本项目由 forge 管理（managed）(fake)")
        : silent;
    case "pass-context":
      return context("plain advisory");
    case "block-it":
      return block("gate says no (fake)");
    case "garbage":
      return { out: "Error: something panicked\n", err: "", code: 1 };
    case "hang":
      return { hang: true };
    default:
      return silent;
  }
}

const log = (hook, payload) => {
  if (process.env.FAKE_FORGE_LOG) {
    appendFileSync(process.env.FAKE_FORGE_LOG, JSON.stringify({ hook, payload }) + "\n");
  }
};

/** Roster of `forge hook batch --event E [--matcher M]`, from the plugin's own spec mirror. */
function batchRoster(argv) {
  const flag = (name) => {
    const at = argv.indexOf(name);
    return at === -1 ? "" : (argv[at + 1] ?? "");
  };
  const event = flag("--event");
  const matcher = flag("--matcher");
  const spec = JSON.parse(readFileSync(fileURLToPath(new URL("../../lib/spec.json", import.meta.url)), "utf8"));
  const names = [];
  for (const group of spec[event] ?? []) {
    if (matcher !== "" && (group.matcher ?? "") !== matcher) continue;
    for (const h of group.hooks) names.push(h.command.replace(/^forge hook /, ""));
  }
  return { event, matcher, names };
}

let data = "";
process.stdin.on("data", (d) => (data += d));
process.stdin.on("end", () => {
  let payload = {};
  try {
    payload = JSON.parse(data || "{}");
  } catch {
    // malformed stdin is logged as-is
  }
  const hook = process.argv[3] ?? "";
  const delayMs = payload.hook_event_name === "SessionStart" ? Number(process.env.FAKE_FORGE_START_DELAY_MS ?? 0) : 0;
  setTimeout(() => dispatch(hook, payload), delayMs);
});

function dispatch(hook, payload) {
  if (hook === "batch") {
    // FAKE_FORGE_BATCH_BROKEN simulates a forge that predates `batch` (or breaks on it): the
    // cobra-style unknown-command failure — exit 1, non-JSON stdout.
    if (process.env.FAKE_FORGE_BATCH_BROKEN === "1") {
      process.stdout.write("Error: unknown hook batch\n");
      process.exit(1);
    }
    const { event, matcher, names } = batchRoster(process.argv.slice(2));
    if (process.env.FAKE_FORGE_BATCH_LOG) {
      appendFileSync(process.env.FAKE_FORGE_BATCH_LOG, JSON.stringify({ event, matcher, names }) + "\n");
    }
    const contexts = [];
    for (const name of names) {
      log(name, payload);
      const r = behave(name, payload);
      if (r.hang) {
        setInterval(() => {}, 1000);
        return;
      }
      if (r.code !== 0) {
        // first block / hard error wins: its stdout is forwarded verbatim (the contract this
        // double pins), later hooks do not run
        process.stdout.write(r.out);
        process.stderr.write(r.err);
        process.exit(r.code);
      }
      if (r.out !== "") {
        const ctx = JSON.parse(r.out)?.hookSpecificOutput?.additionalContext;
        if (typeof ctx === "string" && ctx !== "") contexts.push(ctx);
      }
    }
    if (contexts.length > 0) {
      process.stdout.write(
        JSON.stringify({ hookSpecificOutput: { hookEventName: event, additionalContext: contexts.join("\n\n") } }) + "\n",
      );
    }
    process.exit(0);
  }

  log(hook, payload);
  const r = behave(hook, payload);
  if (r.hang) {
    setInterval(() => {}, 1000);
    return;
  }
  process.stdout.write(r.out);
  process.stderr.write(r.err);
  process.exit(r.code);
}
