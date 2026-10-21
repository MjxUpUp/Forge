/**
 * @agent_forge/forge-dsh — bundled forge binary resolution.
 *
 * The plugin depends on `@agent_forge/forge` (version-locked to the plugin by the
 * release train), so installing the plugin through DSH's pnpm also installs the
 * forge CLI, and uninstalling removes it. That package ships no install script —
 * its binary arrives as a per-platform optionalDependency
 * (`@agent_forge/forge-<os>-<arch>`, os/cpu-filtered by the package manager) — so
 * resolving it here needs no PATH entry and skips the `forge` node shim
 * (~200ms per spawn).
 *
 * 插件依赖 `@agent_forge/forge`（与插件同版本发版），经 DSH 的 pnpm 安装插件时
 * 本体随之安装、卸载插件时随之移除。本体二进制来自平台子包，这里直接解析其
 * 绝对路径：不依赖 PATH，也省掉 node shim 的每次约 200ms。
 *
 * @module bundled
 */
import { createRequire } from "node:module";

/** Platforms the forge npm package ships a binary for (npm/run.js SUPPORTED). */
const SUPPORTED = new Set(["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-x64"]);

/**
 * Platform subpackage name and executable file name for one os/arch, or
 * undefined when forge ships no binary for it.
 *
 * @param {string} [platform] - process.platform override (tests).
 * @param {string} [arch] - process.arch override (tests).
 * @returns {{name: string, exe: string}|undefined}
 */
export function platformPackage(platform = process.platform, arch = process.arch) {
  const key = `${platform}-${arch}`;
  if (!SUPPORTED.has(key)) return undefined;
  return { name: `@agent_forge/forge-${key}`, exe: platform === "win32" ? "forge.exe" : "forge" };
}

/**
 * Absolute path of the bundled forge binary, or undefined when it is not
 * installed (dependency omitted, optional subpackage skipped, unsupported
 * platform). Never throws — an unresolvable bundle simply falls back to the
 * PATH `forge`, and the missing-CLI probe covers the rest.
 *
 * Resolution tries the platform package as seen from `@agent_forge/forge`
 * first (correct under pnpm's isolated linker, where the subpackage is only
 * reachable from the main package), then directly from the plugin (hoisted
 * layout, which is what DSH profiles use).
 *
 * @param {object} [opts]
 * @param {string} [opts.platform]
 * @param {string} [opts.arch]
 * @param {string|URL} [opts.from] - module URL resolution starts from (tests).
 * @returns {string|undefined}
 */
export function resolveBundledForge(opts = {}) {
  const pkg = platformPackage(opts.platform, opts.arch);
  if (pkg === undefined) return undefined;
  const target = `${pkg.name}/bin/${pkg.exe}`;
  const pluginRequire = createRequire(opts.from ?? import.meta.url);
  try {
    const mainPackageJson = pluginRequire.resolve("@agent_forge/forge/package.json");
    return createRequire(mainPackageJson).resolve(target);
  } catch {
    // main package absent or its subpackage not reachable from it — try the plugin's own view
  }
  try {
    return pluginRequire.resolve(target);
  } catch {
    return undefined;
  }
}

/**
 * One line telling the agent where forge lives. The bundled binary is not on
 * PATH, while forge's own gate messages say `forge task start …`; gates are
 * driven by the agent, so it needs the absolute path to run them.
 *
 * @param {string} binPath - absolute path from resolveBundledForge.
 * @returns {string}
 */
export function bundledForgeHint(binPath) {
  const shown = /\s/.test(binPath) ? `"${binPath}"` : binPath;
  return (
    `[forge-dsh] forge CLI bundled with this plugin: ${shown}. It may not be on PATH — ` +
    "when a gate message or the protocol tells you to run a `forge …` command, " +
    `run it with this path instead of \`forge\`.`
  );
}
