#!/usr/bin/env node
//
// scripts/release.js — Forge 版本发布助手【已退役为应急逃生舱】
//
// 2026-08 起标准发版走 release-please：feat/fix 合入 main 自动开 Release PR，合并
// Release PR 即自动 bump + 打 tag + 串 release.yml（见 RELEASE.md「标准发版」与
// .github/workflows/release-please.yml）。本脚本仅当 release-please 层本身故障时应急；
// 它会同步 bump .release-please-manifest.json（release-please 的版本账本）——不同步
// 的话 release-please 从旧版本起算下一版会撞已存在 tag（internal/ci 守卫也会拦）。
//
// 把发版流程里纯机械、每次重复的三步(决定版本号、改 npm/package.json、建 tag)
// 固化成一条命令。规则用 Conventional Commits 语义化版本,与项目现有 commit
// 风格(fix(scope):/feat(scope):/chore(release):)一致。
//
// 规则(auto 模式的推断依据,从上一个 tag 到 HEAD 扫 commit):
//   BREAKING CHANGE,或 <type>!:   → major
//   feat:                          → minor
//   fix:/perf:/refactor:           → patch
//   docs:/chore:/test:/ci:/style:/build: → 不独立发版(归入下次的 changelog)
//
// 用法:
//   node scripts/release.js             # auto: 从 commit 前缀自动推断 bump 类型
//   node scripts/release.js patch       # 强制 patch
//   node scripts/release.js minor       # 强制 minor
//   node scripts/release.js major       # 强制 major
//   node scripts/release.js --dry-run   # 只打印将要做什么,不改文件不建 tag
//
// 脚本只负责:读当前版本 → 算下一版本 → 同步 bump 四个版本文件(npm/package.json、
// .kimi-plugin/plugin.json、plugins/forge-dsh/package.json、
// .release-please-manifest.json) → commit → tag。
// 不 push。push 触发 .github/workflows/release.yml(goreleaser + npm),是对外
// 发布,留给你确认后手动执行。

const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const PKG = path.join(ROOT, 'npm', 'package.json');
const REL_PKG = path.relative(ROOT, PKG);

// git 子进程一律走参数数组（execFileSync）——模板串拼接 shell 会让 tag 名
//（git 合法字符含 $、反引号、;、|，经 describe 回流）在发版机上执行任意命令。
function git(args) {
  return execFileSync('git', args, { cwd: ROOT, encoding: 'utf8' }).trim();
}

// inferBump 扫描 range 内每个 commit,按 Conventional Commits 前缀定 bump 类型。
// messages 是 git log --pretty=%B 输出按记录分隔符切出的数组,拆成参数是为了
// 让测试能注入构造的 commit message,不必依赖真实 git 历史。
function inferBump(messages) {
  let hasMajor = false;
  let hasMinor = false;
  for (const m of messages) {
    // trim 防御：即使调用方传入带前导换行的 message（git log %B%x1e 的原始形态，
    // 每条 commit 第二条起前缀 \n），subject 也能正确提取。readCommitMessages 已
    // normalize，这里是导出 API 契约的 robustness，避免未来调用方绕过 readCommitMessages。
    const subject = m.trim().split('\n')[0];
    // breaking: subject 形如 "feat(x)!:" 或 body 含 "BREAKING CHANGE:"
    const breakingSubject = /^[a-z]+(\([^)]+\))?!:/.test(subject);
    const breakingBody = /\nBREAKING[ -]CHANGE:/.test(m) || /^BREAKING[ -]CHANGE:/.test(m);
    if (breakingSubject || breakingBody) hasMajor = true;
    if (/^feat(\([^)]+\))?:/.test(subject)) hasMinor = true;
  }
  if (hasMajor) return 'major';
  if (hasMinor) return 'minor';
  return 'patch';
}

// bumpVersion 按 semver 规则递增。patch/minor/major 进位后低位归零。
function bumpVersion(cur, bump) {
  const parts = cur.split('.').map(Number);
  if (parts.length !== 3 || parts.some(isNaN)) {
    throw new Error(`unexpected version format: ${cur} (want X.Y.Z)`);
  }
  const [maj, min, pat] = parts;
  if (bump === 'major') return `${maj + 1}.0.0`;
  if (bump === 'minor') return `${maj}.${min + 1}.0`;
  return `${maj}.${min}.${pat + 1}`;
}

// replaceVersionField 把 JSON 文本里第一个 "version": "..." 替换为 next。返回
// {content, ok}：ok=false 表示模式未匹配（文件缺 version 字段，发版事故级）。抽成
// 纯函数：npm/package.json、.kimi-plugin/plugin.json 与 plugins/forge-dsh/package.json
// 的 version bump 共用同一段替换逻辑，且可被 release.test.js 单测（version 替换是
// 发版高价值点）。
function replaceVersionField(content, next) {
  const re = /"version":\s*"[^"]+"/;
  if (!re.test(content)) return { content, ok: false };
  return { content: content.replace(re, `"version": "${next}"`), ok: true };
}

// replaceManifestVersion 把 .release-please-manifest.json 的根包 "." 版本替换为 next。
// manifest 形状是 {".": "1.34.0"}（release-please 的「上次已发布版本」账本），没有
// "version" 字段，故与 replaceVersionField 分开。返回 {content, ok}：ok=false 表示缺
// "." 键（事故级——release-please 拿不到起算版本，internal/ci 的
// TestReleasePleaseManifest_MatchesNpmVersion 也会拦）。
function replaceManifestVersion(content, next) {
  const re = /("\."\s*:\s*)"[^"]+"/;
  if (!re.test(content)) return { content, ok: false };
  return { content: content.replace(re, `$1"${next}"`), ok: true };
}

// readCommitMessages 取 range 内所有 commit 的完整 message。可选 cwdRoot 供测试
// 注入临时仓库(默认用 release.js 所在的项目根),让 readCommitMessages 能端到端测。
function readCommitMessages(range, cwdRoot = ROOT) {
  // %B=完整 message，%x1e=记录分隔符(US)，用来切分多条 commit。
  const log = execFileSync('git', ['log', range, '--pretty=%B%x1e'], { cwd: cwdRoot, encoding: 'utf8' }).trim();
  // %B 输出 message 后 git 追加换行，再 %x1e，再下条记录前的换行 → split 后第二条起
  // 带前导 \n。必须 trim 每段：否则 inferBump 的 subject = msg.split('\n')[0] 取到空串，
  // feat/fix 前缀全部漏判，inferBump 误推 patch（v0.23.0 发布前踩到：2 个 feat 被当 patch）。
  return log.split('\x1e').map(s => s.trim()).filter(Boolean);
}

function main() {
  const args = process.argv.slice(2);
  let dryRun = false;
  let bump = null; // null = auto
  for (const a of args) {
    if (a === '--dry-run' || a === '-n') {
      dryRun = true;
    } else if (a === 'auto') {
      bump = null;
    } else if (a === 'patch' || a === 'minor' || a === 'major') {
      bump = a;
    } else {
      console.error(`unknown argument: ${a}`);
      console.error('usage: node scripts/release.js [auto|patch|minor|major] [--dry-run]');
      process.exit(2);
    }
  }

  // --- current version ---
  const content = fs.readFileSync(PKG, 'utf8');
  const cur = (content.match(/"version":\s*"([^"]+)"/) || [])[1];
  if (!cur) {
    console.error(`cannot read "version" from ${PKG}`);
    process.exit(1);
  }

  // --- decide bump ---
  if (!bump) {
    let lastTag = '';
    try {
      lastTag = git(['describe', '--tags', '--abbrev=0']);
    } catch {
      lastTag = ''; // no tags yet
    }
    const range = lastTag ? `${lastTag}..HEAD` : 'HEAD';
    bump = inferBump(readCommitMessages(range));
    console.log(`auto-inferred bump: ${bump} (scanned ${range})`);
  }

  const next = bumpVersion(cur, bump);
  console.log(`current: ${cur}`);
  console.log(`bump:    ${bump}`);
  console.log(`next:    ${next}`);
  console.log(`tag:     v${next}`);

  // --- tag preflight: fail fast BEFORE any file is written ---
  // 发版序列非事务：tag 已存在时 git tag 在 commit 之后才失败，留下无 tag 的
  // release commit 半成品——逃生舱场景最常见路径就是重跑。预检让它在零副作用
  // 阶段就退出。
  try {
    execFileSync('git', ['rev-parse', '-q', '--verify', `refs/tags/v${next}`], { cwd: ROOT, encoding: 'utf8', stdio: 'pipe' });
    console.error(`tag v${next} 已存在——上次发版可能未完成；如需重发请先手动处理该 tag（git tag -d v${next}）再重跑`);
    process.exit(1);
  } catch {
    // 不存在 = 预检通过
  }

  if (dryRun) {
    console.log('(dry-run, no changes — would bump npm/package.json + .kimi-plugin/plugin.json + plugins/forge-dsh/package.json + .release-please-manifest.json)');
    return;
  }

  // --- compute all four bumps first; write only after every pattern matched ---
  // 先算后写：任一文件模式不匹配时直接退出，不留「改了一半」的工作区（逃生舱场景
  // 高压，半 bump 工作区容易被手滑 commit，制造 npm↔manifest 失同步）。
  const pkgNext = replaceVersionField(content, next);
  if (!pkgNext.ok) {
    console.error(`failed to replace version field in ${REL_PKG} (pattern not matched)`);
    process.exit(1);
  }

  // --- sync .kimi-plugin/plugin.json version to release ---
  // kimi 的 committed manifest version 字段跟 forge release 走（非刻意独立）：每次
  // 发版把 plugin.json 的 version 同步到 next，Go 守卫测试
  // TestKimiPluginManifestVersionTracksRelease 钉住 plugin.json==package.json。kimi
  // 的 staleness 检测仍读 installed.json 的 github.ref tag（不受影响），此处仅让
  // committed 展示元数据不再滞后撒谎。
  const KIMI_PLUGIN = path.join(ROOT, '.kimi-plugin', 'plugin.json');
  const REL_KIMI = path.relative(ROOT, KIMI_PLUGIN);
  const kimiNext = replaceVersionField(fs.readFileSync(KIMI_PLUGIN, 'utf8'), next);
  if (!kimiNext.ok) {
    console.error(`failed to replace version field in ${REL_KIMI} (pattern not matched)`);
    process.exit(1);
  }

  // --- sync plugins/forge-dsh/package.json version to release ---
  // dsh 插件随主发布火车 lockstep 发版（release-please extra-files 统一 bump，
  // TestReleasePleaseManifest_DshPluginTracksTrain 钉住 dsh==manifest）。逃生舱
  // 发版必须同样 bump 它，否则该守卫变红、release.yml 的 tag 对账门禁也会拦。
  const DSH_PKG = path.join(ROOT, 'plugins', 'forge-dsh', 'package.json');
  const REL_DSH = path.relative(ROOT, DSH_PKG);
  const dshNext = replaceVersionField(fs.readFileSync(DSH_PKG, 'utf8'), next);
  if (!dshNext.ok) {
    console.error(`failed to replace version field in ${REL_DSH} (pattern not matched)`);
    process.exit(1);
  }

  // --- sync .release-please-manifest.json (release-please 的版本账本) ---
  // release-please 按 manifest 的 "." 起算下一版本；逃生舱发版不同步它，下个 Release
  // PR 会从旧版本起算、撞已存在 tag（internal/ci 的
  // TestReleasePleaseManifest_MatchesNpmVersion 守卫 npm/package.json == manifest）。
  const MANIFEST = path.join(ROOT, '.release-please-manifest.json');
  const REL_MANIFEST = path.relative(ROOT, MANIFEST);
  const manifestNext = replaceManifestVersion(fs.readFileSync(MANIFEST, 'utf8'), next);
  if (!manifestNext.ok) {
    console.error(`failed to replace "." version in ${REL_MANIFEST} (pattern not matched)`);
    process.exit(1);
  }

  // --- all four patterns matched: write them all ---
  fs.writeFileSync(PKG, pkgNext.content);
  fs.writeFileSync(KIMI_PLUGIN, kimiNext.content);
  fs.writeFileSync(DSH_PKG, dshNext.content);
  fs.writeFileSync(MANIFEST, manifestNext.content);

  // --- verify round-trip (all four files must read back exactly next) ---
  for (const [file, rel] of [[PKG, REL_PKG], [KIMI_PLUGIN, REL_KIMI], [DSH_PKG, REL_DSH]]) {
    const actual = (fs.readFileSync(file, 'utf8').match(/"version":\s*"([^"]+)"/) || [])[1];
    if (actual !== next) {
      console.error(`version mismatch in ${rel}: expected ${next}, got ${actual}`);
      process.exit(1);
    }
  }
  const manifestActual = JSON.parse(fs.readFileSync(MANIFEST, 'utf8'))['.'];
  if (manifestActual !== next) {
    console.error(`version mismatch in ${REL_MANIFEST}: expected ${next}, got ${manifestActual}`);
    process.exit(1);
  }

  // --- commit + tag ---
  git(['add', REL_PKG, REL_KIMI, REL_DSH, REL_MANIFEST]);
  git(['commit', '-m', `chore(release): bump npm version to ${next}`, '-m', 'Co-Authored-By: Claude <noreply@anthropic.com>']);
  git(['tag', `v${next}`]);

  console.log('');
  console.log('done. review the commit/tag, then push to trigger release:');
  console.log('  git push origin main');
  console.log(`  git push origin v${next}`);
}

module.exports = { inferBump, bumpVersion, readCommitMessages, replaceVersionField, replaceManifestVersion };

if (require.main === module) {
  main();
}
