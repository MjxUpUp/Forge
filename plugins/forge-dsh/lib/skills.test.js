import test from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmdirSync, unlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { agentsSkillsInstalled } from "./skills.js";

test("agentsSkillsInstalled: a missing or empty skills directory means not installed", (t) => {
  const home = mkdtempSync(join(tmpdir(), "forge-dsh-skills-"));
  // one cleanup, deepest first (t.after hooks run in registration order)
  t.after(() => {
    for (const dir of [join(home, ".agents", "skills"), join(home, ".agents"), home]) {
      try {
        rmdirSync(dir);
      } catch {
        // already gone
      }
    }
  });
  assert.equal(agentsSkillsInstalled(home), false, "no ~/.agents at all");
  mkdirSync(join(home, ".agents", "skills"), { recursive: true });
  assert.equal(agentsSkillsInstalled(home), false, "directory exists but is empty");
});

test("agentsSkillsInstalled: any entry in ~/.agents/skills counts as installed (a link or a copy)", (t) => {
  const home = mkdtempSync(join(tmpdir(), "forge-dsh-skills-"));
  const dir = join(home, ".agents", "skills");
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "one-skill"), "x");
  t.after(() => {
    unlinkSync(join(dir, "one-skill"));
    rmdirSync(dir);
    rmdirSync(join(home, ".agents"));
    rmdirSync(home);
  });
  assert.equal(agentsSkillsInstalled(home), true);
});

test("agentsSkillsInstalled: dotfiles such as .DS_Store are not skills", (t) => {
  const home = mkdtempSync(join(tmpdir(), "forge-dsh-skills-"));
  const dir = join(home, ".agents", "skills");
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, ".DS_Store"), "x");
  t.after(() => {
    unlinkSync(join(dir, ".DS_Store"));
    rmdirSync(dir);
    rmdirSync(join(home, ".agents"));
    rmdirSync(home);
  });
  assert.equal(agentsSkillsInstalled(home), false);
});

test("agentsSkillsInstalled: an unreadable location degrades to 'installed' — never nag on a failure we cannot interpret", () => {
  assert.equal(agentsSkillsInstalled(join(tmpdir(), "definitely", "not", "a", "home", "\0bad")), true);
  assert.equal(agentsSkillsInstalled(undefined), true);
});
