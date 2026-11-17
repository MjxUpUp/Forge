// run.js 的包装器失败测试（node --test npm/run.test.js）。
// 契约：平台子包缺失 / 平台不支持时，包装器必须响亮失败（stderr 带修复办法、exit 1、
// 不打印任何「成功」标记），不再有 1.x 的 hook fail-open 行为。拷到无 node_modules
// 的临时目录运行，平台子包必然缺失。
const test = require("node:test");
const assert = require("node:assert");
const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

test("run.js: missing platform package fails loudly (no silent approve)", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "forge-runjs-"));
  const script = path.join(dir, "run.js");
  fs.copyFileSync(path.join(__dirname, "run.js"), script);
  const run = (args, extra = {}) =>
    spawnSync(process.execPath, [script, ...args], {
      env: { ...process.env, HOME: dir, NODE_PATH: "", TMPDIR: dir, ...extra },
      encoding: "utf8",
    });

  const res = run(["--version"]);
  assert.strictEqual(res.status, 1);
  assert.match(res.stderr, /subpackage-missing|is not installed/);
  assert.match(res.stderr, /--include=optional/);
  // 1.x 的 hook fail-open 会在 stdout 打 approve JSON 并 exit 0——两者都不该再出现。
  assert.strictEqual(res.stdout.trim(), "");

  const again = run(["task", "report"]);
  assert.strictEqual(again.status, 1);
  assert.strictEqual(again.stdout.trim(), "");
});