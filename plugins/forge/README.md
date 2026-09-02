# Forge plugin

Forge brings loop-engineering quality gates to your AI coding agent: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion.

## Three-step setup

Forge has two parts: a Go binary (the engine that hooks spawn) and this plugin (the wiring that tells your agent where to call it). Install the binary first, then the plugin — project registration follows the takeover preference: with the shipped default (ask) the init-suggest hook asks once per project on first session; with `forge config set` takeover to auto it auto-initializes every git project silently (see step 3).

### 1. Install the forge binary (required, once per machine)

Hooks spawn `forge ...`, so the binary must be on PATH before the plugin can do anything.

    npm install -g @agent_forge/forge

### 2. Install the plugin (once per agent)

Register the marketplace, then install. This wires the gate set (hooks) at the user level — every project on this machine gets the plugin wiring, with no per-project plugin install. Projects still self-register via forge init (see step 3) — since v1.22 that writes nothing into the project; the protocol and runtime state live at the user level (~/.forge/projects/<key>/).

#### Claude Code

    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

#### Codex (CLI / App)

Codex CLI's plugin marketplace path is not officially confirmed to scan .claude-plugin/ (OpenAI docs do not specify the path). The commands below assume schema compatibility; if they fail, skip this section and run `forge init --agents codex` for user-level gate wiring (`~/.codex/hooks.json` plus the `[features] hooks=true` switch in config.toml). Codex has a hook trust review: you may need to trust the forge hooks once in codex `/hooks`.

    codex plugin marketplace add MjxUpUp/Forge
    codex plugin install forge@forge

#### Cursor

    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

Cursor's plugin model carries skills, not Claude-shape hooks. Run `forge init --agents cursor` for user-level gate wiring (`~/.cursor/hooks.json` — nothing is written into the project).

#### GitHub Copilot CLI

Copilot officially scans .claude-plugin/marketplace.json:

    copilot plugin marketplace add MjxUpUp/Forge
    copilot plugin install forge@forge

Copilot officially supports lifecycle hooks, and this plugin ships its own `hooks.json` at the plugin root (copilot's documented plugin-hook location) — the marketplace install above wires the gate set (PreToolUse/PostToolUse/Stop/SessionStart/UserPromptSubmit) directly, no extra step. Two copilot-specific behaviors to know: `agentStop` (Stop) blocks only via the hook's stdout decision, and `userPromptSubmitted` command-hook output is dropped by copilot — context injection on that event is a no-op there (the hooks still run and record). The forge bridge without the plugin (`forge init --agents copilot`) remains a no-op — no user-level channel that forge writes.

**VS Code caveat**: VS Code auto-detects a plugin's format by its manifest marker — `.claude-plugin/plugin.json` means Claude format, and Claude-format hooks are read only from `hooks/hooks.json`, while the plugin-root `hooks.json` is the Copilot-format location (code.visualstudio.com/docs/agent-customization/agent-plugins). This plugin ships the Claude marker, so on VS Code the root `hooks.json` may not load; the Copilot CLI accepts both locations and is unaffected. Closing the VS Code gap would require shipping `hooks/hooks.json`, which would double-fire hooks on Claude Code — pending live verification on both hosts, treat VS Code hook wiring as unverified.

#### Kimi Code

Kimi Code reads the plugin manifest committed at the repo root (`.kimi-plugin/plugin.json`) — no marketplace registration needed:

    /plugins install https://github.com/MjxUpUp/Forge

This wires the full hook set (PreToolUse/PostToolUse/Stop/SessionStart/PostCompact/UserPromptSubmit) at the user level. Alternative without the plugin: `forge init --agents kimi` writes the same hooks into `~/.kimi-code/config.toml` (marker-section merge). When both exist, `forge init` strips the config.toml section — the plugin wins and hooks never double-run.

#### Reasonix

Reasonix reads the NATIVE plugin manifest committed at `plugins/forge/reasonix-plugin.json` (reasonix's limited Claude compatibility does NOT resolve the hooks field of `.claude-plugin/plugin.json`, so a native manifest is required — reasonix prefers it when both are present). Install from GitHub:

    reasonix plugin install https://github.com/MjxUpUp/Forge/tree/main/plugins/forge

This wires the hook set (PreToolUse/PostToolUse/Stop/SessionStart) at the user level (machine-wide, `%APPDATA%\reasonix` on Windows). Alternative without the plugin: `forge init --agents reasonix` writes the same hooks into `<reasonix home>/settings.json` (flat-schema merge). When both exist, `forge init` strips the settings.json hooks — the plugin wins and hooks never double-run.

### 3. Project registration — automatic for plugin users

The plugin wires user-level hooks. What it does NOT do is tell forge which directories are forge projects. For plugin users this step needs no action: the **init-suggest** SessionStart hook detects a user-level installed plugin (installing it IS the opt-in) and silently runs `forge init` on the first session in any git project — the project joins the global registry (`~/.forge/projects.json`) and gets the user-level protocol assets (quality protocol in `~/.claude/CLAUDE.md` + `~/.codex/AGENTS.md`, protocol.yml + runtime state under `~/.forge/projects/<key>/`). Since v1.22 init writes nothing into the project directory itself, so the takeover costs the repo nothing.

Manual `forge init` remains for:

- **Repair / re-register**: auto-takeover failed (the hook surfaces an advisory with the error tail), the project predates the plugin, or the project dir was moved (registry entries are path-keyed; `forge registry prune` cleans stale entries).
- **npm users without the plugin**: the init-suggest hook prompts the agent to ask before initializing (one-shot per project).
- **Team mode**: `forge init --project` keeps the instruction assets (.forge/protocol.yml, CLAUDE.md, AGENTS.md) inside the project for committing — the way to git-share one protocol across a team.

Complete setup: binary (machine) -> plugin (agent) -> registration (automatic).

## What the plugin provides

Claude Code (full): hooks (`.claude-plugin/plugin.json`) = PreToolUse/PostToolUse/Stop/SessionStart gates — the same hook set `forge init` registers into the user-level `~/.claude/settings.json` (all projects). When the plugin is installed, `forge init` skips its own settings.json registration — the plugin wins and hooks never double-run.

It also ships the embedded canonical skill library at `skills/<skill>/` (53 skills, one dir per skill with SKILL.md) — Claude Code loads plugin skills by convention, no manifest field needed. The tree is regenerated by `forge plugin pack` from the single source of truth (`skills/` in the forge repo), so it always matches what `forge skills` serves; stale skill dirs are converged away on regeneration.

For projects initialized by pre-v1.22 forge versions, `forge init` auto-dedupes the leftover duplicates when the plugin is installed — Claude Code would otherwise double-run hooks. This covers both the legacy project-level (`.claude/settings.local.json` hooks) and the legacy user-level (`~/.claude`/`$CLAUDE_CONFIG_DIR` `settings.local.json` forge hooks, left over from a historical global `forge init` in the home dir or an old global install). Existing projects are migrated automatically by the init-suggest SessionStart hook via `forge plugin dedupe --keep-empty` (which also cleans the user-level file). `settings.local.json` (both levels) is preserved as an empty `{}` shell — it is user-placed gitignored config, never silently deleted (the user-level file is always preserved regardless of `--keep-empty`, since it is the user's global config). autoSync also converges every other legacy project-level forge write (`.forge/hooks/`, the forge sections in project CLAUDE.md/AGENTS.md): an unmodified `.forge/protocol.yml` is migrated to the DataDir, while a user-modified one is kept in place as the team-shared override layer.

Other hosts: the plugin is the distribution entry point (marketplace listing); user-level gate wiring (hooks in each agent's user-level config) comes from `forge init --agents <host>`.

## Caveat: projects you do not want forge in

User-level hooks fire in every Claude Code project. Since P2 the takeover default is **ask**: the **init-suggest** SessionStart hook asks once per project on first session (consent → `forge init`, decline → `forge off`) — installing the plugin grants the capability, not takeover of every repo. Prefer zero-friction? `forge config set` takeover to auto restores silent auto-takeover of every git project (`FORGE_TAKEOVER=auto` per-invocation). Projects with their own harness (spec-kit, project-level `.claude` wiring, `.cursor/rules`) are detected and yielded to automatically (`forge on` overrides). Since v1.22 `forge init` writes nothing into the project, takeover costs the repo nothing. Per-project opt-out beats any default: run `forge off` in a project to keep it out permanently — auto-takeover, `FORGE_AUTO_INIT`, and the prompt all go silent there, and `forge init` refuses until you run `forge on` (`forge suggest decline` still works as the legacy alias; `forge off --commit` writes a committed `.forge-decline` team declaration). To remove forge machine-wide, uninstall the plugin or run `forge uninstall` (add `--restore` to roll user-level files back to their pre-forge bytes, from `~/.forge/backups/`). If you move or delete a project directory, clean up its stale registry entry with `forge registry prune`.

## Supported hosts (out of the box)

| Host | Plugin install | Gate wiring | Notes |
|------|----------------|-------------|-------|
| **Claude Code** | `plugin.json` marketplace | automatic (user-level) | full hooks; auto-init via `init-suggest` SessionStart hook |
| **Codex (CLI / App)** | marketplace (path not officially confirmed) | `forge init --agents codex` | if marketplace path fails, fall back to manual |
| **Cursor** | marketplace | `forge init --agents cursor` | Cursor plugin model carries skills, not Claude-shape hooks; user-level `~/.cursor/hooks.json`, zero project writes |
| **GitHub Copilot (CLI / VS Code)** | marketplace | automatic via plugin `hooks.json` (CLI); VS Code unverified | plugin-root hooks.json (Wave 2c); VS Code reads Claude-format hooks from `hooks/hooks.json` only (see caveat above); `forge init --agents copilot` remains a no-op |
| **Windsurf** | `forge init --agents windsurf` | user-level Cascade hooks | `~/.codeium/windsurf/hooks.json` + `memories/global_rules.md` via `internal/agentbridge/windsurf.go` |
| **Kimi Code** | repo-root `.kimi-plugin/plugin.json` (`/plugins install https://github.com/MjxUpUp/Forge`) | automatic (user-level) | full event set (PreToolUse/PostToolUse/Stop/SessionStart/PostCompact/UserPromptSubmit), exit-2 block protocol; fallback `forge init --agents kimi` (config.toml marker section, stripped when the plugin is installed) |
| **Reasonix** | `plugins/forge/reasonix-plugin.json` (`reasonix plugin install https://github.com/MjxUpUp/Forge/tree/main/plugins/forge`) | automatic (user-level) | native manifest (Claude compat does not resolve hooks); fallback `forge init --agents reasonix` (settings.json flat hooks, stripped when the plugin is installed) |
| **Cline** | (none — not a marketplace host) | `forge init --agents cline` | wrapper scripts in `~/Documents/Cline/Rules/Hooks/` (cline v3.36+ file hooks: PreToolUse/PostToolUse/UserPromptSubmit/TaskStart); macOS/Linux only at runtime; cline has no Stop event, so Stop-group gates cannot enforce there |
| **OpenCode** | (none — not a marketplace host) | `forge init --agents opencode` | user-level TS plugin (`~/.config/opencode/plugins/forge.ts`); only tool hooks fire (Pre/PostToolUse) — SessionStart/Stop/UserPromptSubmit group gates cannot run there |
| **CodeBuddy** | `.codebuddy-plugin/` marketplace (pack generated by init) | `forge init --agents codebuddy` | Claude Code-compatible hook model; CodeBuddy's settings.json has no hooks field, so the generated plugin pack is the wiring |
| **DeepSeek Harness (dsh)** | `dsh plugin --profile web add "github:MjxUpUp/Forge#main&path:/plugins/forge-dsh"` (npm: `@agent_forge/forge-dsh`) | automatic via the bundle's Cordis wrapper | full event set mapped onto dsh's typed interception points (tools/pre-execute / post-execute, agent/pre-step / session-start / turn-stopping); PostCompact fires via session-start `source:'compact'`; `forge init --agents dsh` is a deliberate no-op (no user-level config file exists to write); status inside a session: `/forge-status` |
| **ZCode (Z.ai)** | marketplace fallback reads `.claude-plugin/plugin.json` when `.zcode-plugin/` is absent (loads, but unverified end-to-end) | `forge init --agents zcode` | Claude-compatible hook protocol by design (snake_case stdin aliases, `hookSpecificOutput.additionalContext`, exit-2 block) — user-level `~/.zcode/cli/config.json` merge with `hooks.enabled` forced true; no PostCompact/SubagentStop events (compaction rides SessionStart `source=compact`); Stop blocks force-end after 3 consecutive rounds; project-level hooks are not executed by ZCode (team sharing goes through the plugin); protocol rows are docs-derived, wire verification pending |

The twelve rows above are the supported hosts — each has a translator in `internal/agentbridge/`. `install.sh` / `install.ps1` in this directory are only fallback installers for the forge **binary** itself (a curl-pipe npm wrapper); they do no agent wiring.

## Distribution model

Forge ships as an npm binary (`@agent_forge/forge`) plus a marketplace plugin (this directory). Hosts with a marketplace use the same single plugin install command; hosts without one (Windsurf, OpenCode, Cline, …) are wired at the user level by `forge init --agents <host>` instead. The `install.sh` / `install.ps1` scripts here only install the binary — they do no agent wiring.

When this model stops being sufficient (e.g. agents whose marketplace can not resolve `hooks`), `forge plugin pack` lets us generate host-specific packs; until then, one marketplace path plus the user-level bridge serves all supported agents.

## Developing locally (cache copy, not symlinks)

Claude Code plugin cache (`~/.claude/plugins/cache/forge/forge/<version>/`) does **not** follow symlinks — `Search`/`Glob` tools in the agent skip symlinked dirs. The plugin manifest deliberately omits `version` (git SHA drives updates), so do NOT try to read it with `jq -r .version` (yields `null`) — locate the cache dir by listing it (usually a single entry named after the git SHA). To test local plugin changes:

1. Rebuild after changes: `go build ./...`
2. Locate the cache dir by listing: `ls ~/.claude/plugins/cache/forge/forge/`
3. Replace its contents with the freshly-built assets:

```bash
CACHE_DIR=$(ls -d "$HOME"/.claude/plugins/cache/forge/forge/*/ | head -1)
rm -rf "$CACHE_DIR"
mkdir -p "$CACHE_DIR"
cp -R plugins/forge/* "$CACHE_DIR"
```

4. Start a fresh Claude Code session (existing sessions keep old prompts in context).
5. Verify by opening any git project — the `init-suggest` SessionStart hook should fire.

Rationale: Claude Search/Glob tools can not follow symlinks, so the cache copy above replaces rather than links.
