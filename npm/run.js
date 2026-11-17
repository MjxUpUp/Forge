#!/usr/bin/env node

const { spawn } = require("child_process");
const fs = require("fs");
const path = require("path");

// 通过 optionalDependencies 平台子包定位当前平台的二进制。
// npm install 时按 os/cpu 只装匹配当前平台的一个子包（@agent_forge/forge-<platform>-<arch>），
// 二进制随子包落进 node_modules，无需 install script——npm 12 起 install scripts 默认
// 禁用，平台分包是官方推荐的二进制分发方式（esbuild/rollup/turbo 同模式）。
//
// 包装器只做一件事：找到二进制并原样转发 argv/stdio/退出码；找不到就报错退出。
// 1.x 的 hook fail-open（approve JSON + 去重提示）随 1.88 删除 hook 层一并移除——
// 现在缺二进制必须是一次响亮的失败，而不是「假装命令成功」。
//
// 解析路径刻意用「本包的同级目录」而不是 require.resolve：require.resolve 会沿
// 祖先目录找 node_modules，用户主目录下残留的旧版 @agent_forge/forge-<platform>
// 会抢先命中（实测 1.42.2 就这样覆盖过新装的 2.0.0），表现为「装了新版却在跑旧版」。
const exe = process.platform === "win32" ? "forge.exe" : "forge";
const platKey = `${process.platform}-${process.arch}`;
const platformPkg = `@agent_forge/forge-${platKey}`;

// 支持的平台白名单（goreleaser 构建矩阵：linux/darwin × amd64/arm64 + windows/amd64）。
// 不在此列的平台（如 windows-arm64）无对应子包——明确 stderr 提示，不静默退出 0。
const SUPPORTED = new Set([
  "darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-x64",
]);

function die(message, fix) {
  console.error(`[forge] ${message}\n修复: ${fix}`);
  process.exit(1);
}

if (!SUPPORTED.has(platKey)) {
  die(
    `unsupported platform: ${platKey} (no prebuilt binary)`,
    "该平台没有预编译二进制（支持 darwin/linux 的 arm64/x64 与 win32-x64）；改用受支持平台，或从源码构建 forge 放进 PATH。"
  );
}

// __dirname 是 …/node_modules/@agent_forge/forge，平台子包是它的兄弟目录。
const binaryPath = path.join(__dirname, "..", `forge-${platKey}`, "bin", exe);
if (!fs.existsSync(binaryPath)) {
  die(
    `platform package ${platformPkg} is not installed`,
    "重装并保留 optionalDependencies：npm i -g @agent_forge/forge --include=optional（不要带 --omit=optional）。"
  );
}

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});

// spawn 失败走 error 事件而非 exit——没有监听器会带栈迹崩溃并把失败说成成功。
child.on("error", (e) => {
  die(
    `spawn failed: ${e.message}`,
    `检查 ${binaryPath} 是否存在且可执行（chmod +x）；仍失败则重装：npm i -g @agent_forge/forge --include=optional。`
  );
});

// exit 的 code 为 null 表示被信号杀死（OOM/SIGKILL）——不能折叠成成功语义。
// 保留子进程的信号终止语义（128+signal），并给出可见提示。
child.on("exit", (code, signal) => {
  if (code === null) {
    console.error(
      `[forge] forge exited on signal ${signal}\n` +
        "修复: forge 被信号终止（常见于内存不足或被安全软件杀掉）；重试，持续复现请带上本行报错反馈。"
    );
    process.exit(signal ? 128 + signal : 1);
  }
  process.exit(code);
});