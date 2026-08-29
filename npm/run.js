#!/usr/bin/env node

const { spawn } = require("child_process");

// 通过 optionalDependencies 平台子包定位当前平台的二进制。
// npm install 时按 os/cpu 只装匹配当前平台的一个子包（@agent_forge/forge-<platform>-<arch>），
// 二进制随子包落进 node_modules，无需 install script——npm 12 起 install scripts 默认
// 禁用，平台分包是官方推荐的二进制分发方式（esbuild/rollup/turbo 同模式）。
const exe = process.platform === "win32" ? "forge.exe" : "forge";
const platformPkg = `@agent_forge/forge-${process.platform}-${process.arch}`;

// 支持的平台白名单（goreleaser 构建矩阵：linux/darwin × amd64/arm64 + windows/amd64）。
// 不在此列的平台（如 windows-arm64）无对应子包，require.resolve 必失败——明确 stderr 提示，
// 不混入"子包未装"的静默 approve，避免用户误以为 Forge 正常工作却零拦截。
const SUPPORTED = new Set([
  "darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-x64",
]);
const platKey = `${process.platform}-${process.arch}`;
if (!SUPPORTED.has(platKey)) {
  console.error(`[forge] unsupported platform: ${platKey} (no prebuilt binary); failing open — hooks will not fire.`);
  console.log('{"decision":"approve"}');
  process.exit(0);
}

let binaryPath;
try {
  binaryPath = require.resolve(`${platformPkg}/bin/${exe}`);
} catch (_) {
  // 支持的平台但子包未装（npm 12 + --omit=optional / 安装中断）——静默 approve 避免阻塞 hooks。
  console.log('{"decision":"approve"}');
  process.exit(0);
}

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});

// spawn 失败走 error 事件而非 exit——无监听器会带栈迹非零崩溃，违反本包装器的
// fail-open 契约（宿主表现为 hook 异常而非 approve）。
child.on("error", (e) => {
  console.error(`[forge] spawn failed: ${e.message}; failing open — hooks will not fire.`);
  console.log('{"decision":"approve"}');
  process.exit(0);
});

// exit 的 code 为 null 表示被信号杀死（OOM/SIGKILL）——不能折叠成成功语义；
// 按包装器 fail-open 契约显式 approve 并留 stderr 线索。
child.on("exit", (code, signal) => {
  if (code === null) {
    console.error(`[forge] forge exited on signal ${signal}; failing open — hooks will not fire.`);
    console.log('{"decision":"approve"}');
    process.exit(0);
  }
  process.exit(code || 0);
});
