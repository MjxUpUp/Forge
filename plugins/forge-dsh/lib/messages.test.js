import test from "node:test";
import assert from "node:assert/strict";
import { messagesFor, resolveLocale } from "./messages.js";

test("resolveLocale: config beats env beats the system locale; unknown values fall through to auto-detection", () => {
  const sys = (tag) => () => tag;
  assert.equal(resolveLocale("en", { FORGE_DSH_LOCALE: "zh" }, sys("zh-CN")), "en", "config wins");
  assert.equal(resolveLocale(undefined, { FORGE_DSH_LOCALE: "en" }, sys("zh-CN")), "en", "env wins over the system");
  assert.equal(resolveLocale(undefined, {}, sys("zh-CN")), "zh");
  assert.equal(resolveLocale(undefined, {}, sys("zh-Hans-CN")), "zh");
  assert.equal(resolveLocale("auto", {}, sys("en-US")), "en", "auto = detect");
  assert.equal(resolveLocale("fr", {}, sys("de-DE")), "en", "unsupported locales degrade to English, never to a crash");
  assert.equal(resolveLocale(undefined, {}, () => {
    throw new Error("Intl unavailable");
  }), "en", "a throwing detector degrades to English");
});

test("messagesFor: every message exists in both languages with the same shape", () => {
  const en = messagesFor("en");
  const zh = messagesFor("zh");
  assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort(), "no message may exist in only one language");
  for (const key of Object.keys(en)) assert.equal(typeof en[key], "function", `${key} is a function in en`);
  assert.equal(messagesFor("xx"), en, "unknown locale → English");
});

test("welcome: names both versions, asks for a one-sentence relay, mentions /forge-status and forge off; degrades without a forge version", () => {
  for (const locale of ["en", "zh"]) {
    const m = messagesFor(locale);
    const full = m.welcome("1.2.3", "1.2.4");
    assert.ok(full.includes("1.2.3") && full.includes("1.2.4"), `${locale}: both versions`);
    assert.match(full, /\/forge-status/);
    assert.match(full, /forge off/);
    const noForge = m.welcome("1.2.3", undefined);
    assert.ok(noForge.includes("1.2.3"));
    assert.ok(!noForge.includes("undefined"), `${locale}: no dangling "undefined" when the probe gave no version`);
  }
  assert.match(messagesFor("zh").welcome("1", "2"), /第一次回复/);
  assert.match(messagesFor("en").welcome("1", "2"), /first reply/);
});

test("bundledHint: names the absolute path, quotes one containing spaces", () => {
  for (const locale of ["en", "zh"]) {
    const m = messagesFor(locale);
    assert.ok(m.bundledHint("/p/forge").includes("/p/forge"));
    assert.ok(m.bundledHint("/Users/a b/forge").includes('"/Users/a b/forge"'), `${locale}: spaces are quoted`);
  }
  assert.match(messagesFor("en").bundledHint("/p/forge"), /instead of `forge`/);
  assert.match(messagesFor("en").bundledHint("/p/forge"), /not be on PATH/);
});

test("missingForge: states the gates are OFF, the fixes, and the probe error when there is one", () => {
  for (const locale of ["en", "zh"]) {
    const m = messagesFor(locale);
    const withError = m.missingForge("/x/forge", "ENOENT");
    assert.ok(withError.includes("/x/forge"), "names the configured forgeBin");
    assert.ok(withError.includes("npm install -g @agent_forge/forge"));
    assert.ok(withError.includes("forgeBin"));
    assert.ok(withError.includes("ENOENT"));
    assert.ok(!m.missingForge("/x/forge", undefined).includes("undefined"));
  }
  assert.match(messagesFor("en").missingForge("/x/forge"), /forge CLI not found/);
  assert.match(messagesFor("en").missingForge("/x/forge"), /fail-open/);
});

test("degraded: says how many runs failed open, that nothing is blocked, and where to look", () => {
  for (const locale of ["en", "zh"]) {
    const text = messagesFor(locale).degraded(4);
    assert.ok(text.includes("4"));
    assert.match(text, /\/forge-status/);
    assert.match(text, /timeoutMs/);
  }
  assert.match(messagesFor("en").degraded(3), /nothing is blocked/);
});

test("skillsHint: offers the install command (asks first — it writes the user's home), quoting a path with spaces", () => {
  for (const locale of ["en", "zh"]) {
    const m = messagesFor(locale);
    const text = m.skillsHint("forge");
    assert.ok(text.includes("forge skills install --global --target agents"), `${locale}: the exact command`);
    assert.ok(text.includes("~/.agents/skills"));
    assert.ok(m.skillsHint("/Users/a b/forge").includes('"/Users/a b/forge" skills install'), `${locale}: spaces are quoted`);
  }
  assert.match(messagesFor("en").skillsHint("forge"), /only after the user agrees/);
  assert.match(messagesFor("zh").skillsHint("forge"), /征得用户同意/);
});
