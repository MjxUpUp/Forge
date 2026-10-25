#!/usr/bin/env node

const { spawn } = require("child_process");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");

// 通过 optionalDependencies 平台子包定位当前平台的二进制。
// npm install 时按 os/cpu 只装匹配当前平台的一个子包（@agent_forge/forge-<platform>-<arch>），
// 二进制随子包落进 node_modules，无需 install script——npm 12 起 install scripts 默认
// 禁用，平台分包是官方推荐的二进制分发方式（esbuild/rollup/turbo 同模式）。
const exe = process.platform === "win32" ? "forge.exe" : "forge";
const platformPkg = `@agent_forge/forge-${process.platform}-${process.arch}`;

// 基础设施失败的可见性（docs/plans/hook-silent-failure-visibility-2026-10.md）：
// 本包装器自己能看见的四类失败（平台不支持 / 子包缺失 / spawn 失败 / 被信号杀死）
// 都 fail-open，但此前对宿主是「静默 approve」——没人知道 hook 零拦截。现在：
//   - hook 调用（argv[2] === "hook"）：同一宿主进程（ppid）同一原因第一次 stderr 输出
//     带修复办法的提示并 exit 1（宿主把 stderr 展示给用户，且 1 从不阻断），之后同因
//     静默 exit 0。包装器不能消费 stdin（子进程要读），所以会话键取 ppid 而非 session id；
//     `sh -c` 不 exec 末条命令的宿主上 ppid 逐次变化，去重退化为逐次提示，不比现状差。
//   - 非 hook 调用（交互使用）：stderr 总是输出提示，退出码保持 0（既有契约）。
//   - stdout 契约不变：仍打印 {"decision":"approve"}。
// 去重状态与 Go 侧同目录（$TMPDIR/forge-hook-infra/ppid-<pid>/），键带 wrapper: 前缀
// 避免与 Go 侧撞车。FORGE_HOOK_INFRA_DEDUP=0 恢复逐次 exit 1（与 Go 侧同一逃生口）。
// 包装器侧的逃生口无法落 checklog——这些失败发生时 forge 二进制本身缺席或没起来。
//
// node 缺失（`env: node: No such file or directory`）发生在本脚本运行之前，这里无能
// 为力，由 `forge doctor` 的 hook runtime 检查承担。
const isHook = process.argv[2] === "hook";

function claimNotice(cause) {
  try {
    const dir = path.join(os.tmpdir(), "forge-hook-infra", `ppid-${process.ppid}`);
    fs.mkdirSync(dir, { recursive: true });
    const key = crypto.createHash("sha256").update(`wrapper:${cause}`).digest("hex").slice(0, 16);
    fs.closeSync(fs.openSync(path.join(dir, key), "wx"));
    return true;
  } catch (e) {
    // 状态记不下来时宁可多提示，不吞（与 Go 侧 ClaimInfraNotice 同取向）。
    return !(e && e.code === "EEXIST");
  }
}

function failOpen(cause, message, fix) {
  const dedupOn = (process.env.FORGE_HOOK_INFRA_DEDUP || "").trim() !== "0";
  const first = !isHook || !dedupOn || claimNotice(cause);
  console.log('{"decision":"approve"}');
  if (first) {
    console.error(
      `[forge] hook 基础设施故障（${cause}，已放行、不阻断${dedupOn ? "；本会话同因不再提示" : ""}）: ${message}\n` +
        `修复: ${fix}\n（FORGE_HOOK_INFRA_DEDUP=0 恢复逐次报错）`
    );
  }
  process.exit(isHook && first ? 1 : 0);
}

// 支持的平台白名单（goreleaser 构建矩阵：linux/darwin × amd64/arm64 + windows/amd64）。
// 不在此列的平台（如 windows-arm64）无对应子包，require.resolve 必失败——明确 stderr 提示，
// 不混入"子包未装"的静默 approve，避免用户误以为 Forge 正常工作却零拦截。
const SUPPORTED = new Set([
  "darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-x64",
]);
const platKey = `${process.platform}-${process.arch}`;
if (!SUPPORTED.has(platKey)) {
  failOpen(
    "unsupported-platform",
    `unsupported platform: ${platKey} (no prebuilt binary); hooks will not fire`,
    "该平台没有预编译二进制（支持 darwin/linux 的 arm64/x64 与 win32-x64）；改用受支持平台，或从源码构建 forge 放进 PATH。"
  );
}

let binaryPath;
try {
  binaryPath = require.resolve(`${platformPkg}/bin/${exe}`);
} catch (_) {
  // 支持的平台但子包未装（npm 12 + --omit=optional / 安装中断）——fail-open 避免阻塞 hooks，
  // 但不再静默：首次提示 + 修复办法。
  failOpen(
    "subpackage-missing",
    `platform package ${platformPkg} is not installed; hooks will not fire`,
    "重装并保留 optionalDependencies：npm i -g @agent_forge/forge --include=optional（不要带 --omit=optional）。"
  );
}

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});

// spawn 失败走 error 事件而非 exit——无监听器会带栈迹非零崩溃，违反本包装器的
// fail-open 契约（宿主表现为 hook 异常而非 approve）。
child.on("error", (e) => {
  failOpen(
    "spawn-failed",
    `spawn failed: ${e.message}; hooks will not fire`,
    `检查 ${binaryPath} 是否存在且可执行（chmod +x）；仍失败则重装：npm i -g @agent_forge/forge --include=optional。`
  );
});

// exit 的 code 为 null 表示被信号杀死（OOM/SIGKILL）——不能折叠成成功语义；
// 按包装器 fail-open 契约显式 approve 并给出可见提示。
child.on("exit", (code, signal) => {
  if (code === null) {
    failOpen(
      "signal-killed",
      `forge exited on signal ${signal}; hooks will not fire`,
      "forge 被信号终止（常见于内存不足或被安全软件杀掉）；重试，持续复现请带上本行报错反馈。"
    );
  }
  process.exit(code || 0);
});
