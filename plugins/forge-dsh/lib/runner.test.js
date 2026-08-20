import test from "node:test";
import assert from "node:assert/strict";
import { runForgeHook } from "./runner.js";

const FAKE = new URL("../test/doubles/fake-forge.mjs", import.meta.url).pathname;

test("block verdict: read from the decision field, reason preferred", async () => {
  const v = await runForgeHook("forge hook block-it", { hook_event_name: "PreToolUse" }, { forgeBin: FAKE });
  assert.equal(v.block, true);
  assert.equal(v.reason, "gate says no (fake)");
});

test("pass with context: exit 0 + bare hookSpecificOutput", async () => {
  const v = await runForgeHook("forge hook pass-context", {}, { forgeBin: FAKE });
  assert.equal(v.block, false);
  assert.equal(v.context, "plain advisory");
});

test("silent pass: empty stdout is a clean allow", async () => {
  const v = await runForgeHook("forge hook silent", {}, { forgeBin: FAKE });
  assert.deepEqual(v, { block: false });
});

test("cobra-style internal error (exit 1, non-JSON) fails OPEN — never a deny", async () => {
  const v = await runForgeHook("forge hook garbage", {}, { forgeBin: FAKE });
  assert.equal(v.block, false);
  assert.equal(v.error, "unparseable forge stdout");
});

test("missing forge binary fails open", async () => {
  const v = await runForgeHook("forge hook task-guard", {}, { forgeBin: "/nonexistent/forge-bin" });
  assert.equal(v.block, false);
  assert.equal(typeof v.error, "string");
});

test("a hung forge is killed past timeoutMs and fails open", async () => {
  const started = Date.now();
  const v = await runForgeHook("forge hook hang", {}, { forgeBin: FAKE, timeoutMs: 300 });
  assert.equal(v.block, false);
  assert.match(v.error ?? "", /timeout/);
  assert.ok(Date.now() - started < 5000, "must not wait for the hung child");
});

test("a forge that exits without reading stdin does NOT crash the host (EPIPE regression)", async () => {
  // 256KB payload — far beyond the 64KB pipe buffer, so the write EPIPEs for
  // certain when the child is already gone. Without the stdin error guard
  // this test process itself would die on an unhandled 'error' event.
  const big = { hook_event_name: "PreToolUse", tool_input: { content: "x".repeat(256 * 1024) } };
  const v = await runForgeHook("forge hook early-exit", big, { forgeBin: FAKE });
  assert.equal(v.block, false); // fail-open, not a crash
});

test("stdin payload arrives as one JSON document", async () => {
  const log = new URL("../test/doubles/.payload-log", import.meta.url).pathname;
  process.env.FAKE_FORGE_LOG = log; // children inherit process.env
  try {
    await runForgeHook(
      "forge hook silent",
      { hook_event_name: "PostToolUse", marker: 42 },
      { forgeBin: FAKE },
    );
  } finally {
    delete process.env.FAKE_FORGE_LOG;
  }
  const { readFileSync, rmSync } = await import("node:fs");
  const lines = readFileSync(log, "utf8").trim().split("\n").map(JSON.parse);
  rmSync(log, { force: true });
  assert.equal(lines.length, 1);
  assert.equal(lines[0].hook, "silent");
  assert.deepEqual(lines[0].payload, { hook_event_name: "PostToolUse", marker: 42 });
});
