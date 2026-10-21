#!/usr/bin/env node
// verify-dsh-install.js — 发布后验证 @agent_forge/forge-dsh 的「装插件即装本体」。
//
// release.yml 的 npm-verify 先验主包 npm i -g 装回；本脚本补另一条用户路径：
// 在干净目录装 forge-dsh（DSH 的 pnpm 同样靠依赖解析，npm 的解析规则等价），
// 断言插件自带的 bundled.js 能解析出本体二进制、该二进制位于安装目录内、且
// --version 与 tag 一致。依赖钉漏 bump / 平台子包被跳过 / 解析逻辑回归，都在
// 发布后第一时间红灯，而不是等用户装完发现 hook 全部静默 fail-open。
//
// Post-release check that installing @agent_forge/forge-dsh brings the forge CLI
// along: the plugin's own bundled.js must resolve a binary inside the install
// dir whose --version equals the tag.
//
// 用法 / usage:
//   node scripts/verify-dsh-install.js --spec @agent_forge/forge-dsh@1.2.3 --want 1.2.3 \
//        [--registry https://registry.npmjs.org] [--attempts 10] [--sleep-sec 30]

'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync, spawnSync } = require('child_process');
const { pathToFileURL } = require('url');

const PLUGIN_DIR = path.join('node_modules', '@agent_forge', 'forge-dsh');

// verifyInstalled 校验一个已安装的 profile 目录：返回 {ok, error?, bin?, version?}。
// 纯检查、不联网，可用夹具单测。
async function verifyInstalled(dir, want) {
  const bundled = path.join(dir, PLUGIN_DIR, 'lib', 'bundled.js');
  if (!fs.existsSync(bundled)) return { ok: false, error: `插件未安装：缺 ${path.relative(dir, bundled)}` };
  const mod = await import(pathToFileURL(bundled).href);
  const bin = mod.resolveBundledForge();
  if (typeof bin !== 'string' || bin === '') {
    return { ok: false, error: '插件的 resolveBundledForge() 返回空——本体或平台子包未随插件装上（依赖钉缺失或子包被跳过）' };
  }
  const root = fs.realpathSync(dir);
  if (!fs.realpathSync(bin).startsWith(root + path.sep)) {
    return { ok: false, error: `解析到的二进制不在安装目录内（${bin}）——取到了宿主别处的 forge，不是随插件装的那份` };
  }
  let out;
  try {
    out = execFileSync(bin, ['--version'], { encoding: 'utf8', timeout: 30000 });
  } catch (e) {
    return { ok: false, error: `内置二进制无法运行：${String(e.message).split('\n')[0]}` };
  }
  const got = (out.match(/\d+\.\d+\.\d+/) || [])[0];
  if (got !== want) return { ok: false, error: `内置 forge 版本 ${got} != tag ${want}——插件依赖钉与发布版本货不对板`, bin };
  return { ok: true, bin, version: got };
}

function parseArgs(argv) {
  const out = { registry: 'https://registry.npmjs.org', attempts: 10, sleepSec: 30 };
  for (let i = 0; i < argv.length; i += 2) {
    const k = argv[i];
    const v = argv[i + 1];
    if (k === '--spec') out.spec = v;
    else if (k === '--want') out.want = v;
    else if (k === '--registry') out.registry = v;
    else if (k === '--attempts') out.attempts = Number(v);
    else if (k === '--sleep-sec') out.sleepSec = Number(v);
    else throw new Error(`unknown argument ${k}`);
  }
  if (!out.spec || !out.want) throw new Error('--spec and --want are required');
  return out;
}

function sleep(sec) {
  return new Promise((r) => setTimeout(r, sec * 1000));
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'verify-dsh-'));
  try {
    fs.writeFileSync(path.join(dir, 'package.json'), '{"name":"verify-dsh-profile","private":true}\n');
    // --ignore-scripts：forge 本体靠平台子包分发、无 install script，验证不需要生命周期，
    // 也不让刚发布的包代码在 CI 里跑。
    // 传播窗内 registry 读路径可滞后写路径（npm-verify 主包验证已有多轮实录）：
    // 安装失败按固定间隔重试，重试用尽才失败。
    const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
    let installed = false;
    for (let i = 1; i <= args.attempts; i++) {
      const r = spawnSync(
        npm,
        ['install', '--prefix', dir, '--ignore-scripts', '--no-audit', '--no-fund', `--registry=${args.registry}`, args.spec],
        { stdio: 'inherit', shell: process.platform === 'win32' },
      );
      if (r.error) {
        // npm 本身起不来（不在 PATH 等）：重试无意义，立即失败而不是空等 10×30s
        console.error(`::error::无法执行 npm：${r.error.message}`);
        process.exitCode = 1;
        return;
      }
      if (r.status === 0) {
        console.log(`installed ${args.spec} on attempt ${i}/${args.attempts}`);
        installed = true;
        break;
      }
      if (i < args.attempts) {
        console.log(`attempt ${i}/${args.attempts} failed — backoff ${args.sleepSec}s`);
        await sleep(args.sleepSec);
      }
    }
    if (!installed) {
      console.error(`::error::${args.attempts} 次安装 ${args.spec} 均失败——查 registry 传播或 npm job 日志`);
      process.exitCode = 1;
      return;
    }
    const res = await verifyInstalled(dir, args.want);
    if (!res.ok) {
      console.error(`::error::${res.error}`);
      process.exitCode = 1;
      return;
    }
    console.log(`verify-dsh-install passed: bundled forge ${res.version} == tag v${args.want} (${res.bin})`);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

if (require.main === module) {
  main().catch((e) => {
    console.error(`::error::${e.message}`);
    process.exit(1);
  });
}

module.exports = { verifyInstalled, parseArgs };
