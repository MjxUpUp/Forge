/**
 * @agent_forge/forge-dsh — model-facing message catalogue.
 *
 * Every line the plugin itself puts into the conversation lives here, in both
 * languages, so the wording of one session is uniform (the old mix: English
 * missing-CLI notice, Chinese welcome) and a new message cannot ship in one
 * language only — a test pins that the two catalogues have the same keys.
 * forge's own hook texts are not translated here; they come from the forge binary.
 *
 * Language: config `locale` > env FORGE_DSH_LOCALE > the system locale. Anything
 * unsupported degrades to English.
 *
 * 插件自己写进对话的每一句话都集中在此，中英各一份：同一会话措辞统一（此前缺 CLI 提示
 * 是英文、欢迎语是中文），且新增消息不可能只有一种语言——测试钉住两份目录键集合相同。
 * forge 自己 hook 的文案由 forge 二进制输出，不在此翻译。
 * 语言：配置 locale > 环境变量 FORGE_DSH_LOCALE > 系统 locale；不支持的一律回落英文。
 *
 * @module messages
 */

/** Supported catalogue languages. */
const LOCALES = new Set(["en", "zh"]);

/**
 * Pick the catalogue language.
 *
 * @param {string|undefined} configured - the profile patch's `locale` ("en" | "zh" | "auto").
 * @param {Record<string, string|undefined>} [env] - process.env override (tests).
 * @param {() => string} [systemLocale] - system locale tag source (tests).
 * @returns {"en"|"zh"}
 */
export function resolveLocale(
  configured,
  env = process.env,
  systemLocale = () => Intl.DateTimeFormat().resolvedOptions().locale,
) {
  for (const candidate of [configured, env.FORGE_DSH_LOCALE]) {
    if (candidate !== undefined && candidate !== "" && candidate !== "auto") {
      return LOCALES.has(candidate) ? candidate : "en";
    }
  }
  try {
    return systemLocale().toLowerCase().startsWith("zh") ? "zh" : "en";
  } catch {
    return "en";
  }
}

const quoted = (path) => (/\s/.test(path) ? `"${path}"` : path);

const en = {
  /** One first-turn line; the user only sees what the model relays, hence the explicit ask. */
  welcome: (pluginVersion, forgeVersion) =>
    `[forge-dsh] Forge quality gates now govern this project (plugin ${pluginVersion}${forgeVersion === undefined ? "" : `, forge ${forgeVersion}`}): ` +
    "create a forge task before editing source, and dangerous Bash commands are intercepted. " +
    "In your first reply, tell the user this in one sentence and mention /forge-status for status and `forge off` to switch the gates off. Say it only once.",
  /** The bundled binary is not on PATH while forge's gate messages say `forge task …`. */
  bundledHint: (binPath) =>
    `[forge-dsh] forge CLI bundled with this plugin: ${quoted(binPath)}. It may not be on PATH — ` +
    "when a gate message or the protocol tells you to run a `forge …` command, " +
    "run it with this path instead of `forge`.",
  missingForge: (forgeBin, error) =>
    `[forge-dsh] forge CLI not found (forgeBin: ${forgeBin}). ` +
    "Forge gates are OFF for this session — every action runs unguarded (fail-open). " +
    "Reinstall the plugin (it bundles forge) or run `npm install -g @agent_forge/forge`, then start a new session; " +
    "if it lives elsewhere, set forgeBin in the profile patch." +
    (error === undefined ? "" : `\nProbe error: ${error}`),
  degraded: (count) =>
    `[forge-dsh] ${count} forge hook runs failed open in this session (a timeout or an unreadable forge reply), so those checks did not run. ` +
    "Run /forge-status to see which, and raise timeoutMs if a gate is timing out. This is advisory; nothing is blocked.",
};

const zh = {
  welcome: (pluginVersion, forgeVersion) =>
    `[forge-dsh] Forge 质量门禁已接管本项目（插件 ${pluginVersion}${forgeVersion === undefined ? "" : `，forge ${forgeVersion}`}）：修改源码前需先建 forge task，危险的 Bash 命令会被拦截。` +
    "请在本会话第一次回复的末尾用一句话告知用户这一点，并提示：/forge-status 查看状态，forge off 关闭门禁。只提这一次。",
  bundledHint: (binPath) =>
    `[forge-dsh] 本插件自带的 forge CLI：${quoted(binPath)}。它可能不在 PATH 上——` +
    "当门禁消息或协议要求你运行 `forge …` 命令时，请用这个路径代替 `forge`。",
  missingForge: (forgeBin, error) =>
    `[forge-dsh] 未找到 forge CLI（forgeBin：${forgeBin}）。` +
    "本会话 Forge 门禁处于关闭状态——所有操作都不受保护（fail-open）。" +
    "请重装本插件（它自带 forge）或运行 `npm install -g @agent_forge/forge`，然后开启新会话；" +
    "如果 forge 在别处，请在 profile patch 里设置 forgeBin。" +
    (error === undefined ? "" : `\n探测错误：${error}`),
  degraded: (count) =>
    `[forge-dsh] 本会话有 ${count} 次 forge hook 运行 fail-open（超时或 forge 回复无法解析），这些检查没有执行。` +
    "运行 /forge-status 查看是哪些，若某个门禁超时请调大 timeoutMs。这只是提示，不会阻断任何操作。",
};

const CATALOGUES = { en, zh };

/**
 * The message catalogue for a language; unknown languages get English.
 *
 * @param {string} locale
 * @returns {typeof en}
 */
export function messagesFor(locale) {
  return CATALOGUES[locale] ?? en;
}
