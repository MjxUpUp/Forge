/**
 * @agent_forge/forge-dsh — is Forge's skill library installed for DSH?
 *
 * Installing the plugin delivers the bridge and (via its dependency) the forge
 * CLI, but not the skill library: DSH reads the agent-neutral `~/.agents/skills`
 * directory, which only `forge skills install --global --target agents` fills.
 * Without it a DSH user silently gets half the product, so the first-turn
 * onboarding offers the install when the directory is empty.
 *
 * 装插件交付的是桥接层和（经依赖）forge CLI，不含技能库：DSH 读跨 agent 的
 * `~/.agents/skills`，只有 `forge skills install --global --target agents` 会填充它。
 * 缺了它 DSH 用户悄悄只拿到半个产品，所以目录为空时首轮引导会提议安装。
 *
 * @module skills
 */
import { readdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

/**
 * Whether `<home>/.agents/skills` holds at least one entry (a link or a copy).
 * A missing directory counts as not installed. Any other failure — an unreadable
 * or odd location — degrades to `true`: this only gates a polite nudge, and
 * nagging on an error the plugin cannot interpret would be worse than staying quiet.
 *
 * @param {string} [home] - home directory (tests); defaults to the user's.
 * @returns {boolean}
 */
export function agentsSkillsInstalled(home = homedir()) {
  if (typeof home !== "string" || home === "") return true;
  try {
    // dotfiles (.DS_Store …) are not skills
    return readdirSync(join(home, ".agents", "skills")).some((name) => !name.startsWith("."));
  } catch (error) {
    return error?.code !== "ENOENT";
  }
}
