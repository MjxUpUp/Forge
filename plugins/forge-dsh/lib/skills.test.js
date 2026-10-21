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

test("agentsSkillsInstalled: a failure other than 'not there' degrades to 'installed' — never nag on something we cannot interpret", () => {
  // injected reader: the error codes are fixed, so this means the same on every OS
  const failing = (code) => () => {
    throw Object.assign(new Error(code), { code });
  };
  assert.equal(agentsSkillsInstalled("/h", failing("ENOENT")), false, "missing directory = not installed");
  for (const code of ["EACCES", "EPERM", "ENOTDIR", "EIO", undefined]) {
    assert.equal(agentsSkillsInstalled("/h", failing(code)), true, `${code}: unreadable, so stay quiet`);
  }
  // not `undefined`: that selects the default parameter, i.e. the REAL home of whoever runs the suite
  assert.equal(agentsSkillsInstalled(null), true, "no home to look in");
  assert.equal(agentsSkillsInstalled(""), true);
});
