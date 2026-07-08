#!/usr/bin/env node
//
// scripts/release.js — Forge 版本发布助手
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
// 脚本只负责:读当前版本 → 算下一版本 → 改 npm/package.json → commit → tag。
// 不 push。push 触发 .github/workflows/release.yml(goreleaser + npm),是对外
// 发布,留给你确认后手动执行。

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const PKG = path.join(ROOT, 'npm', 'package.json');
const REL_PKG = path.relative(ROOT, PKG);

function git(args) {
  return execSync(`git ${args}`, { cwd: ROOT, encoding: 'utf8' }).trim();
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

// readCommitMessages 取 range 内所有 commit 的完整 message。可选 cwdRoot 供测试
// 注入临时仓库(默认用 release.js 所在的项目根),让 readCommitMessages 能端到端测。
function readCommitMessages(range, cwdRoot = ROOT) {
  // %B=完整 message，%x1e=记录分隔符(US)，用来切分多条 commit。
  const log = execSync(`git log ${range} --pretty=%B%x1e`, { cwd: cwdRoot, encoding: 'utf8' }).trim();
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
      lastTag = git('describe --tags --abbrev=0');
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

  if (dryRun) {
    console.log('(dry-run, no changes)');
    return;
  }

  // --- edit npm/package.json (string replace → minimal diff, preserves formatting) ---
  const updated = content.replace(
    /"version":\s*"[^"]+"/,
    `"version": "${next}"`
  );
  if (updated === content) {
    console.error('failed to replace version field (pattern not matched)');
    process.exit(1);
  }
  fs.writeFileSync(PKG, updated);

  // verify round-trip
  const reread = fs.readFileSync(PKG, 'utf8');
  const actual = (reread.match(/"version":\s*"([^"]+)"/) || [])[1];
  if (actual !== next) {
    console.error(`version mismatch after edit: expected ${next}, got ${actual}`);
    process.exit(1);
  }

  // --- commit + tag ---
  git(`add ${REL_PKG}`);
  git(`commit -m "chore(release): bump npm version to ${next}" -m "Co-Authored-By: Claude <noreply@anthropic.com>"`);
  git(`tag v${next}`);

  console.log('');
  console.log('done. review the commit/tag, then push to trigger release:');
  console.log('  git push origin main');
  console.log(`  git push origin v${next}`);
}

module.exports = { inferBump, bumpVersion, readCommitMessages };

if (require.main === module) {
  main();
}
