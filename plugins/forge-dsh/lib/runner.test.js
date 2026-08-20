import test from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { planSpawn, runForgeHook } from "./runner.js";
import { FAKE_BIN as FAKE } from "../test/doubles/forge-bin.mjs";

test("planSpawn: win32 routes through the shell — npm's forge.cmd shim is not directly spawnable", () => {
  const p = planSpawn("forge", ["hook", "task-guard"], "win32");
  assert.equal(p.shell, true);
  assert.equal(p.file, "forge");
  assert.deepEqual(p.args, ["hook", "task-guard"]);
});

test("planSpawn: win32 pre-quotes a binary path containing spaces", () => {
  const p = planSpawn("C:\\Program Files\\forge\\forge.exe", ["hook", "x"], "win32");
  assert.equal(p.file, '"C:\\Program Files\\forge\\forge.exe"');
});

test("planSpawn: win32 pre-quotes metacharacters even without spaces (cmd token splitters)", () => {
  // & | ( ) < > ^ , ; = each split or redirect an unquoted cmd.exe token —
  // "C:\A&B\forge.cmd" unquoted would execute a stray "B\forge.cmd".
  for (const ch of ["&", "|", "(", ")", "<", ">", "^", ",", ";", "="]) {
    const p = planSpawn(`C:\\A${ch}B\\forge.cmd`, ["hook", "x"], "win32");
    assert.equal(p.file, `"C:\\A${ch}B\\forge.cmd"`, `metachar ${ch} must be quoted`);
  }
  // A plain name (the default "forge") stays unquoted.
  assert.equal(planSpawn("forge", ["hook", "x"], "win32").file, "forge");
});

test("planSpawn: POSIX spawns the binary directly (no shell)", () => {
  for (const platform of ["linux", "darwin", "freebsd"]) {
    const p = planSpawn("/usr/local/bin/forge", ["hook", "x"], platform);
    assert.equal(p.shell, false);
    assert.equal(p.file, "/usr/local/bin/forge");
  }
});

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
  const log = fileURLToPath(new URL("../test/doubles/.payload-log", import.meta.url));
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
