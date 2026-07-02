// scripts/release.test.js — scripts/release.js 的单元 + 端到端测试。
// 用 node 内置 node:test(node 18+),无需第三方测试框架。
// 跑法:node --test scripts/release.test.js
//
// 重点覆盖 bumpVersion(版本号算错 = 发版事故,最高价值)与 inferBump
// (规则推断的正确性),外加 dry-run 不改 package.json 的端到端守卫。

const { test } = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { execSync } = require('child_process');

const { inferBump, bumpVersion, readCommitMessages } = require('./release.js');

const SCRIPT = path.join(__dirname, 'release.js');
const PKG = path.join(__dirname, '..', 'npm', 'package.json');

// --- bumpVersion:纯函数,版本号计算正确性(发版事故高价值点) ---

test('bumpVersion patch 递增末位', () => {
  assert.strictEqual(bumpVersion('0.22.0', 'patch'), '0.22.1');
});

test('bumpVersion patch 末位进位', () => {
  assert.strictEqual(bumpVersion('0.22.9', 'patch'), '0.22.10');
});

test('bumpVersion minor 递增中位并归零末位', () => {
  assert.strictEqual(bumpVersion('0.22.5', 'minor'), '0.23.0');
});

test('bumpVersion minor 中位进位', () => {
  assert.strictEqual(bumpVersion('0.9.9', 'minor'), '0.10.0');
});

test('bumpVersion major 递增首位并归零低两位', () => {
  assert.strictEqual(bumpVersion('0.22.0', 'major'), '1.0.0');
  assert.strictEqual(bumpVersion('1.2.3', 'major'), '2.0.0');
});

test('bumpVersion 非法版本格式抛错', () => {
  assert.throws(() => bumpVersion('0.22', 'patch'), /unexpected version format/);
  assert.throws(() => bumpVersion('a.b.c', 'patch'), /unexpected version format/);
});

test('bumpVersion 非法 bump 类型当 patch 处理', () => {
  // 未知 bump 走 else 分支(patch 语义),不静默失败
  assert.strictEqual(bumpVersion('0.22.0', 'unknown'), '0.22.1');
});

// --- inferBump:Conventional Commits 规则推断 ---

test('inferBump 只有 fix → patch', () => {
  assert.strictEqual(inferBump(['fix(scoring): a', 'fix(hooks): b']), 'patch');
});

test('inferBump 有 feat → minor', () => {
  assert.strictEqual(inferBump(['feat(cli): new cmd', 'fix(x): y']), 'minor');
});

test('inferBump subject 带 ! → major(breaking)', () => {
  assert.strictEqual(inferBump(['feat(api)!: change contract']), 'major');
  assert.strictEqual(inferBump(['fix(core)!: behavior change']), 'major');
});

test('inferBump body 含 BREAKING CHANGE → major', () => {
  const msg = 'feat(api): redesign\n\nBREAKING CHANGE: drops old endpoint';
  assert.strictEqual(inferBump([msg]), 'major');
});

test('inferBump BREAKING-CHANGE 连字符变体也识别', () => {
  const msg = 'fix(x): y\n\nBREAKING-CHANGE: alt footer';
  assert.strictEqual(inferBump([msg]), 'major');
});

test('inferBump major 优先级高于 minor', () => {
  // 同一批既有 feat(minor)又有 breaking(major)→ major 胜出
  assert.strictEqual(inferBump(['feat: a', 'fix!: b']), 'major');
});

test('inferBump 只有 docs/chore/test → patch(不发版类型按 patch 计)', () => {
  assert.strictEqual(inferBump(['docs: readme', 'chore: deps', 'test: x']), 'patch');
});

test('inferBump 空提交列表 → patch', () => {
  assert.strictEqual(inferBump([]), 'patch');
});

test('inferBump feat 带括号 scope 正确识别', () => {
  assert.strictEqual(inferBump(['feat(taskpipeline): add gate']), 'minor');
});

// inferBump 必须容忍带前导换行的 message —— 这正是 `git log --pretty=%B%x1e`
// 的输出形态：%B 末尾换行 + 记录分隔换行，使第二条 commit 起带前导 \n。
// v0.23.0 发布前 inferBump 对此取 subject="" 漏判 feat → 误推 patch。
test('inferBump message 带前导换行（git log %B 形态）仍识别 feat → minor', () => {
  const msgs = [
    'fix(a): first commit has no leading newline',
    '\nfeat(b): second commit carries leading newline from %B format',
    '\nfix(c): third also carries it',
  ];
  assert.strictEqual(inferBump(msgs), 'minor');
});

test('inferBump message 带前导换行 + breaking subject 仍识别 major', () => {
  assert.strictEqual(inferBump(['\nfeat(api)!: breaking change']), 'major');
});

// readCommitMessages 端到端：在临时 git 仓库造 feat commit，验证返回的 message
// 不带前导换行（trim normalize 生效），且整链路 readCommitMessages → inferBump
// 把 feat 正确推断为 minor。这是 v0.23.0 发布事故的直接回归守卫。
test('readCommitMessages 端到端：trim 前导换行 + feat 推断为 minor', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'release-git-'));
  const run = (args) => execSync(`git ${args}`, { cwd: dir, encoding: 'utf8', stdio: 'ignore' });
  try {
    run('init');
    run('-c user.email=t@t.com -c user.name=T commit --allow-empty -m "chore: baseline"');
    run('tag v1.0.0');
    run('-c user.email=t@t.com -c user.name=T commit --allow-empty -m "feat(scope): add thing"');
    run('-c user.email=t@t.com -c user.name=T commit --allow-empty -m "fix: a bug"');

    const msgs = readCommitMessages('v1.0.0..HEAD', dir);
    assert.ok(msgs.length >= 2, `expected >=2 messages, got ${msgs.length}`);
    for (const m of msgs) {
      assert.ok(!m.startsWith('\n'), `message must not carry leading newline: ${JSON.stringify(m.slice(0, 30))}`);
    }
    assert.strictEqual(inferBump(msgs), 'minor');
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

// --- 端到端:dry-run 不改 package.json ---

test('dry-run auto 打印版本信息且不修改 package.json', () => {
  const before = fs.readFileSync(PKG, 'utf8');
  const out = execSync(`node ${SCRIPT} --dry-run`, { encoding: 'utf8' });
  const after = fs.readFileSync(PKG, 'utf8');

  assert.match(out, /current:/);
  assert.match(out, /bump:/);
  assert.match(out, /next:/);
  assert.match(out, /tag:\s+v\d+\.\d+\.\d+/);
  assert.match(out, /dry-run, no changes/);
  assert.strictEqual(before, after, 'dry-run must not modify npm/package.json');
});

test('dry-run minor 覆盖 auto 推断', () => {
  const out = execSync(`node ${SCRIPT} minor --dry-run`, { encoding: 'utf8' });
  assert.match(out, /bump:\s+minor/);
  assert.match(out, /next:\s+\d+\.\d+\.0/);
});

test('未知参数退出码非零', () => {
  let code = 0;
  try {
    execSync(`node ${SCRIPT} bogus`, { encoding: 'utf8', stdio: 'pipe' });
  } catch (e) {
    code = e.status;
  }
  assert.notStrictEqual(code, 0, 'unknown arg must exit non-zero');
});
