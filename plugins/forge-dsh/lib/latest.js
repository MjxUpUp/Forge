/**
 * @agent_forge/forge-dsh — "is a newer plugin published?" for /forge-status.
 *
 * Version drift between an installed plugin and the repo is silent: an old
 * install keeps working while missing fixes (the audit found a 1.74.1 install
 * lacking everything since). /forge-status is the one place the user looks when
 * something seems off, so it asks the registry once, with a short timeout, and
 * shows the answer. It is a read-only GET issued only when the user runs the
 * command, never at session start, and every failure (offline, private registry,
 * odd response) degrades to "unknown" — a status command must not throw.
 *
 * 已装插件与仓库的版本漂移是静默的：旧版一直能用，只是缺修复（审计里 1.74.1 的装机就缺了
 * 此后的一切）。/forge-status 是用户觉得不对劲时唯一会看的地方，所以在这里问一次 registry
 * （短超时）并展示结果。只在用户执行该命令时发一次只读 GET，绝不在会话启动时发；任何失败
 * （离线、私有 registry、响应异常）都降级为「未知」——状态命令不能抛错。
 *
 * @module latest
 */

const DEFAULT_REGISTRY = "https://registry.npmjs.org";
const PACKAGE_PATH = "@agent_forge%2Fforge-dsh";

/** A published plugin version: plain semver, optionally with a pre-release/build tag, bounded length. */
const PLAIN_VERSION = /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]{1,40})?$/;

/**
 * Whether a string is safe to show as a version. The registry's answer ends up in
 * /forge-status text, which the model can read, so anything that is not plain semver
 * (newlines, escape sequences, prose) is refused rather than echoed.
 *
 * @param {unknown} value
 * @returns {boolean}
 */
export function isPlainVersion(value) {
  return typeof value === "string" && value.length <= 64 && PLAIN_VERSION.test(value);
}

/**
 * Compare the numeric core (major.minor.patch) of two versions. A pre-release
 * suffix is ignored; an unparseable input has no order.
 *
 * @param {string|undefined} a
 * @param {string|undefined} b
 * @returns {-1|0|1|undefined} negative when a is older than b.
 */
export function compareVersions(a, b) {
  const core = (v) => {
    const m = /^(\d+)\.(\d+)\.(\d+)/.exec(String(v ?? ""));
    return m === null ? undefined : [Number(m[1]), Number(m[2]), Number(m[3])];
  };
  const left = core(a);
  const right = core(b);
  if (left === undefined || right === undefined) return undefined;
  for (let i = 0; i < 3; i += 1) {
    if (left[i] !== right[i]) return left[i] < right[i] ? -1 : 1;
  }
  return 0;
}

/**
 * The registry's latest published plugin version, or undefined on any failure.
 *
 * @param {object} [opts]
 * @param {string} [opts.registry="https://registry.npmjs.org"]
 * @param {number} [opts.timeoutMs=3000]
 * @param {typeof fetch} [opts.fetch] - injected for tests; defaults to the global fetch.
 * @param {boolean} [opts.noGlobalFetch] - tests: pretend the runtime has no fetch.
 * @returns {Promise<string|undefined>}
 */
export async function fetchLatestVersion(opts = {}) {
  const doFetch = opts.fetch ?? (opts.noGlobalFetch ? undefined : globalThis.fetch);
  if (typeof doFetch !== "function") return undefined;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), opts.timeoutMs ?? 3000);
  timer.unref?.();
  try {
    // inside the try: a misconfigured registry value must not make /forge-status throw
    const registry = String(opts.registry ?? DEFAULT_REGISTRY).replace(/\/+$/, "");
    const res = await doFetch(`${registry}/${PACKAGE_PATH}/latest`, { signal: controller.signal });
    if (!res?.ok) return undefined;
    const body = await res.json();
    return isPlainVersion(body?.version) ? body.version : undefined;
  } catch {
    return undefined;
  } finally {
    clearTimeout(timer);
  }
}
