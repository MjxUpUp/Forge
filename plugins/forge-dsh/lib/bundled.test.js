import test from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { platformPackage, resolveBundledForge, bundledForgeHint } from "./bundled.js";

/** Lay out a throwaway node_modules tree; returns its root and a `from` URL inside it. */
function fixture(t, layout) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), "forge-dsh-bundled-")));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const [rel, body] of Object.entries(layout)) {
    const file = join(root, rel);
    mkdirSync(join(file, ".."), { recursive: true });
    writeFileSync(file, body);
  }
  mkdirSync(join(root, "node_modules/@agent_forge/forge-dsh/lib"), { recursive: true });
  const from = pathToFileURL(join(root, "node_modules/@agent_forge/forge-dsh/lib/bundled.js")).href;
  return { root, from };
}

test("platformPackage names the five shipped platforms and rejects the rest", () => {
  assert.deepEqual(platformPackage("darwin", "arm64"), { name: "@agent_forge/forge-darwin-arm64", exe: "forge" });
  assert.deepEqual(platformPackage("linux", "x64"), { name: "@agent_forge/forge-linux-x64", exe: "forge" });
  assert.deepEqual(platformPackage("win32", "x64"), { name: "@agent_forge/forge-win32-x64", exe: "forge.exe" });
  assert.equal(platformPackage("win32", "arm64"), undefined);
  assert.equal(platformPackage("freebsd", "x64"), undefined);
});

test("resolveBundledForge: hoisted layout (pnpm nodeLinker=hoisted) resolves the platform binary", (t) => {
  const { root, from } = fixture(t, {
    "node_modules/@agent_forge/forge/package.json": "{}",
    "node_modules/@agent_forge/forge-darwin-arm64/package.json": "{}",
    "node_modules/@agent_forge/forge-darwin-arm64/bin/forge": "bin",
  });
  assert.equal(
    resolveBundledForge({ platform: "darwin", arch: "arm64", from }),
    join(root, "node_modules/@agent_forge/forge-darwin-arm64/bin/forge"),
  );
});

test("resolveBundledForge: platform package nested under forge (isolated linker) still resolves", (t) => {
  const { root, from } = fixture(t, {
    "node_modules/@agent_forge/forge/package.json": "{}",
    "node_modules/@agent_forge/forge/node_modules/@agent_forge/forge-linux-x64/package.json": "{}",
    "node_modules/@agent_forge/forge/node_modules/@agent_forge/forge-linux-x64/bin/forge": "bin",
  });
  assert.equal(
    resolveBundledForge({ platform: "linux", arch: "x64", from }),
    join(root, "node_modules/@agent_forge/forge/node_modules/@agent_forge/forge-linux-x64/bin/forge"),
  );
});

test("resolveBundledForge: win32 resolves forge.exe", (t) => {
  const { root, from } = fixture(t, {
    "node_modules/@agent_forge/forge/package.json": "{}",
    "node_modules/@agent_forge/forge-win32-x64/package.json": "{}",
    "node_modules/@agent_forge/forge-win32-x64/bin/forge.exe": "bin",
  });
  assert.equal(
    resolveBundledForge({ platform: "win32", arch: "x64", from }),
    join(root, "node_modules/@agent_forge/forge-win32-x64/bin/forge.exe"),
  );
});

test("resolveBundledForge: missing forge, missing platform package, or unsupported platform → undefined, never throws", (t) => {
  const none = fixture(t, {});
  assert.equal(resolveBundledForge({ platform: "darwin", arch: "arm64", from: none.from }), undefined);
  const noPlatform = fixture(t, { "node_modules/@agent_forge/forge/package.json": "{}" });
  assert.equal(resolveBundledForge({ platform: "darwin", arch: "arm64", from: noPlatform.from }), undefined);
  const full = fixture(t, {
    "node_modules/@agent_forge/forge/package.json": "{}",
    "node_modules/@agent_forge/forge-darwin-arm64/package.json": "{}",
    "node_modules/@agent_forge/forge-darwin-arm64/bin/forge": "bin",
  });
  assert.equal(resolveBundledForge({ platform: "win32", arch: "arm64", from: full.from }), undefined);
});

test("bundledForgeHint names the absolute path and quotes one containing spaces", () => {
  const plain = bundledForgeHint("/p/forge");
  assert.match(plain, /\/p\/forge/);
  assert.match(plain, /not be on PATH/);
  assert.match(plain, /instead of `forge`/);
  assert.match(bundledForgeHint("/Users/a b/forge"), /"\/Users\/a b\/forge"/);
});
