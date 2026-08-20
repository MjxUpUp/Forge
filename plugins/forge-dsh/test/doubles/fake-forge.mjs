#!/usr/bin/env node
/**
 * Test double for the forge binary. Behaves per hook name (argv[2] of
 * `fake-forge hook <name>`), and — when FAKE_FORGE_LOG is set — appends one
 * JSON line per invocation ({hook, payload}) for payload assertions.
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
import { appendFileSync } from "node:fs";

// Simulates a forge that fails BEFORE reading stdin (unknown hook name after
// a spec/binary version drift, an early panic): exits immediately, leaving
// the parent's stdin write to hit EPIPE.
if (process.argv[3] === "early-exit") {
  process.exit(1);
}

let data = "";
process.stdin.on("data", (d) => (data += d));
process.stdin.on("end", () => {
  const hook = process.argv[3] ?? "";
  let payload = {};
  try {
    payload = JSON.parse(data || "{}");
  } catch {
    // malformed stdin is logged as-is
  }
  if (process.env.FAKE_FORGE_LOG) {
    appendFileSync(process.env.FAKE_FORGE_LOG, JSON.stringify({ hook, payload }) + "\n");
  }
  const block = (reason) => {
    process.stdout.write(
      JSON.stringify({
        decision: "block",
        reason,
        hookSpecificOutput: { hookEventName: payload.hook_event_name ?? "", additionalContext: reason },
      }) + "\n",
    );
    process.stderr.write(reason + "\n");
    process.exit(2);
  };
  switch (hook) {
    case "task-guard":
      block("BLOCKED: no active task (fake)");
      break;
    case "task-verify":
      block("BLOCKED: task gates not done (fake)");
      break;
    case "resume-reinject":
      if (String(payload?.prompt ?? "").includes("block-prompt")) {
        block("prompt rejected by gate (fake)");
      }
      process.exit(0);
      break;
    case "file-sentinel":
      if (String(payload?.tool_input?.command ?? "").includes("quarantine-me")) {
        block("file-sentinel: unauthorized change (fake)");
      }
      process.exit(0);
      break;
    case "hazard-guard":
      if (String(payload?.tool_input?.command ?? "").includes("rm -rf")) {
        block("hazard: rm -rf intercepted (fake)");
      }
      process.exit(0);
      break;
    case "skill-trigger":
      process.stdout.write(
        JSON.stringify({
          hookSpecificOutput: {
            hookEventName: payload.hook_event_name ?? "",
            additionalContext: "consider the test-discipline skill",
          },
        }) + "\n",
      );
      process.exit(0);
      break;
    case "pass-context":
      process.stdout.write(
        JSON.stringify({
          hookSpecificOutput: {
            hookEventName: payload.hook_event_name ?? "",
            additionalContext: "plain advisory",
          },
        }) + "\n",
      );
      process.exit(0);
      break;
    case "block-it":
      block("gate says no (fake)");
      break;
    case "garbage":
      process.stdout.write("Error: something panicked\n");
      process.exit(1);
      break;
    case "hang":
      setInterval(() => {}, 1000);
      break;
    default:
      process.exit(0);
  }
});
