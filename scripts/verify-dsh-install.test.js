'use strict';

const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { verifyInstalled, parseArgs } = require('./verify-dsh-install.js');

const BUNDLED_SRC = path.join(__dirname, '..', 'plugins', 'forge-dsh', 'lib', 'bundled.js');
const posixOnly = { skip: process.platform === 'win32' ? 'POSIX shell-script binary double' : false };

// 夹具：一个装好 forge-dsh（用真实 bundled.js）+ forge 本体 + 当前平台子包的 profile 目录。
function profile(t, { version = '9.9.9', withForge = true, withPlatform = true } = {}) {
  const dir = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'verify-dsh-test-')));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const nm = path.join(dir, 'node_modules', '@agent_forge');
  fs.mkdirSync(path.join(nm, 'forge-dsh', 'lib'), { recursive: true });
  fs.writeFileSync(path.join(nm, 'forge-dsh', 'package.json'), '{"name":"@agent_forge/forge-dsh","type":"module"}');
  fs.copyFileSync(BUNDLED_SRC, path.join(nm, 'forge-dsh', 'lib', 'bundled.js'));
  if (withForge) {
    fs.mkdirSync(path.join(nm, 'forge'), { recursive: true });
    fs.writeFileSync(path.join(nm, 'forge', 'package.json'), '{"name":"@agent_forge/forge"}');
  }
  if (withPlatform) {
    const plat = `forge-${process.platform}-${process.arch}`;
    fs.mkdirSync(path.join(nm, plat, 'bin'), { recursive: true });
    fs.writeFileSync(path.join(nm, plat, 'package.json'), `{"name":"@agent_forge/${plat}"}`);
    const exe = path.join(nm, plat, 'bin', process.platform === 'win32' ? 'forge.exe' : 'forge');
    fs.writeFileSync(exe, `#!/bin/sh\necho "forge version ${version} (fake)"\n`, { mode: 0o755 });
  }
  return dir;
}

test('verifyInstalled passes when the bundled binary resolves inside the dir and reports the tag version', posixOnly, async (t) => {
  const dir = profile(t);
  const r = await verifyInstalled(dir, '9.9.9');
  assert.equal(r.ok, true, r.error);
  assert.equal(r.version, '9.9.9');
  assert.ok(r.bin.startsWith(dir), 'binary lives under the install dir');
});

test('verifyInstalled fails on a version that does not match the tag', posixOnly, async (t) => {
  const dir = profile(t, { version: '9.9.8' });
  const r = await verifyInstalled(dir, '9.9.9');
  assert.equal(r.ok, false);
  assert.match(r.error, /9\.9\.8 != tag 9\.9\.9/);
});

test('verifyInstalled fails when the forge package or its platform subpackage did not come along', async (t) => {
  // (a main package without the platform subpackage = optional dep skipped; (b) neither came along)
  for (const opts of [{ withPlatform: false }, { withForge: false, withPlatform: false }]) {
    const r = await verifyInstalled(profile(t, opts), '9.9.9');
    assert.equal(r.ok, false);
    assert.match(r.error, /resolveBundledForge\(\) 返回空/);
  }
});

test('verifyInstalled fails when the plugin itself is not installed', async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'verify-dsh-empty-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const r = await verifyInstalled(dir, '9.9.9');
  assert.equal(r.ok, false);
  assert.match(r.error, /插件未安装/);
});

test('parseArgs requires spec and want and rejects unknown flags', () => {
  const ok = parseArgs(['--spec', '@agent_forge/forge-dsh@1.2.3', '--want', '1.2.3', '--attempts', '3', '--sleep-sec', '1']);
  assert.deepEqual(ok, { registry: 'https://registry.npmjs.org', attempts: 3, sleepSec: 1, spec: '@agent_forge/forge-dsh@1.2.3', want: '1.2.3' });
  assert.throws(() => parseArgs(['--want', '1.2.3']), /required/);
  assert.throws(() => parseArgs(['--spec', 'x', '--want', '1', '--bogus', 'y']), /unknown argument/);
});
