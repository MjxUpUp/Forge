// run.js 的包装器侧基础设施失败可见性测试（node --test npm/run.test.js）。
// 与 internal/cli/npm_runjs_infra_test.go 同契约：hook 调用同因首次 exit 1 + 修复办法，
// 同一父进程再发静默 exit 0；非 hook 调用恒 exit 0 且有 stderr 提示；去重可用
// FORGE_HOOK_INFRA_DEDUP=0 关闭。拷到无 node_modules 的临时目录运行，平台子包必然缺失。
const test = require("node:test");
const assert = require("node:assert");
const { spawnSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

test("run.js: platform package missing is visible once per host process", { skip: process.platform === "win32" }, () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "forge-runjs-"));
  const script = path.join(dir, "run.js");
  fs.copyFileSync(path.join(__dirname, "run.js"), script);
  const run = (args, extra = {}) =>
    spawnSync(process.execPath, [script, ...args], {
      env: { ...process.env, HOME: dir, NODE_PATH: "", TMPDIR: dir, FORGE_HOOK_INFRA_DEDUP: "", ...extra },
      encoding: "utf8",
    });

  const first = run(["hook", "batch"]);
  assert.strictEqual(first.status, 1);
  assert.match(first.stderr, /subpackage-missing/);
  assert.match(first.stderr, /--include=optional/);
  assert.match(first.stdout, /"decision":"approve"/);

  const second = run(["hook", "batch"]);
  assert.strictEqual(second.status, 0);
  assert.strictEqual(second.stderr.trim(), "");

  assert.strictEqual(run(["hook", "batch"], { FORGE_HOOK_INFRA_DEDUP: "0" }).status, 1);

  const interactive = run(["--version"]);
  assert.strictEqual(interactive.status, 0);
  assert.match(interactive.stderr, /subpackage-missing/);
});
