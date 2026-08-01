# Forge plugin

Forge brings loop-engineering quality gates to your AI coding agent: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion.

## Three-step setup

Forge has two parts: a Go binary (the engine that hooks spawn) and this plugin (the wiring that tells your agent where to call it). Install the binary first, then the plugin, then init each project.

### 1. Install the forge binary (required, once per machine)

Hooks spawn `forge ...`, so the binary must be on PATH before the plugin can do anything.

    npm install -g @agent_forge/forge

### 2. Install the plugin (once per agent)

Register the marketplace, then install. This wires the gate set (hooks) at the user level — every project on this machine gets the plugin wiring, with no per-project plugin install. Projects still self-register via forge init (see step 3) — since v1.22 that writes nothing into the project; the protocol and runtime state live at the user level (~/.forge/projects/<key>/).

#### Claude Code

    /plugin marketplace add %[1]s
    /plugin install forge@forge

#### Codex (CLI / App)

Codex CLI's plugin marketplace path is not officially confirmed to scan .claude-plugin/ (OpenAI docs do not specify the path). The commands below assume schema compatibility; if they fail, skip this section and run `forge init --agents codex` for user-level gate wiring (`~/.codex/hooks.json` plus the `[features] hooks=true` switch in config.toml). Codex has a hook trust review: you may need to trust the forge hooks once in codex `/hooks`.

    codex plugin marketplace add %[1]s
    codex plugin install forge@forge

#### Cursor

    /plugin marketplace add %[1]s
    /plugin install forge@forge

Cursor's plugin model carries skills, not Claude-shape hooks. Run `forge init --agents cursor` for user-level gate wiring (`~/.cursor/hooks.json` — nothing is written into the project).

#### GitHub Copilot CLI

Copilot officially scans .claude-plugin/marketplace.json:

    copilot plugin marketplace add %[1]s
    copilot plugin install forge@forge

Copilot has no user-level hook channel, so the forge bridge (`forge init --agents copilot`) is a no-op — no hooks, and no user-level location for guidance. The marketplace install above is a distribution entry point only.

#### Kimi Code

Kimi Code reads the plugin manifest committed at the repo root (`.kimi-plugin/plugin.json`) — no marketplace registration needed:

    /plugins install https://github.com/%[1]s

This wires the full hook set (PreToolUse/PostToolUse/Stop/SessionStart/PostCompact/UserPromptSubmit) at the user level. Alternative without the plugin: `forge init --agents kimi` writes the same hooks into `~/.kimi-code/config.toml` (marker-section merge). When both exist, `forge init` strips the config.toml section — the plugin wins and hooks never double-run.

### 3. Initialize each project (once per project)

The plugin wires user-level hooks. What it does NOT do is tell forge which directories are forge projects. `forge init` registers the project in the global registry (`~/.forge/projects.json`) and, on first run, lays down the user-level protocol assets (the quality protocol in `~/.claude/CLAUDE.md` + `~/.codex/AGENTS.md`, the `/forge-quality` skill, protocol.yml + runtime state under `~/.forge/projects/<key>/`). Since v1.22 it writes nothing into the project directory itself:

    cd your-project
    forge init

Complete setup: binary (machine) -> plugin (agent) -> init (project). To git-share one protocol across a team, use `forge init --project` (team mode) — it keeps the instruction assets (.forge/protocol.yml, CLAUDE.md, AGENTS.md) inside the project for committing.

## What the plugin provides

Claude Code (full): hooks (`.claude-plugin/plugin.json`) = PreToolUse/PostToolUse/Stop/SessionStart gates — the same hook set `forge init` registers into the user-level `~/.claude/settings.json` (all projects). When the plugin is installed, `forge init` skips its own settings.json registration — the plugin wins and hooks never double-run.

For projects initialized by pre-v1.22 forge versions, `forge init` auto-dedupes the leftover duplicates when the plugin is installed — Claude Code would otherwise double-run hooks. This covers both the legacy project-level (`.claude/settings.local.json` hooks) and the legacy user-level (`~/.claude`/`$CLAUDE_CONFIG_DIR` `settings.local.json` forge hooks, left over from a historical global `forge init` in the home dir or an old global install). Existing projects are migrated automatically by the init-suggest SessionStart hook via `forge plugin dedupe --keep-empty` (which also cleans the user-level file). `settings.local.json` (both levels) is preserved as an empty `{}` shell — it is user-placed gitignored config, never silently deleted (the user-level file is always preserved regardless of `--keep-empty`, since it is the user's global config). autoSync also converges every other legacy project-level forge write (`.forge/hooks/`, the forge sections in project CLAUDE.md/AGENTS.md): an unmodified `.forge/protocol.yml` is migrated to the DataDir, while a user-modified one is kept in place as the team-shared override layer.

Other hosts: the plugin is the distribution entry point (marketplace listing); user-level gate wiring (hooks in each agent's user-level config) comes from `forge init --agents <host>`.

## Caveat: projects you do not want forge in

User-level hooks fire in every Claude Code project. In git projects not yet initialized with forge, the **init-suggest** SessionStart hook detects this and prompts the agent to ask the user whether to run `forge init` (one-shot `suggested` marker so it asks only once). Since v1.22 `forge init` writes nothing into the project, accepting costs the repo nothing. To permanently silence the prompt for a specific project, run `forge suggest decline` there. To remove forge machine-wide, uninstall the plugin or run `forge uninstall` (add `--restore` to roll user-level files back to their pre-forge bytes, from `~/.forge/backups/`).

## Supported hosts (out of the box)

| Host | Plugin install | Gate wiring | Notes |
|------|----------------|-------------|-------|
| **Claude Code** | `plugin.json` marketplace | automatic (user-level) | full hooks; auto-init via `init-suggest` SessionStart hook |
| **Codex (CLI / App)** | marketplace (path not officially confirmed) | `forge init --agents codex` | if marketplace path fails, fall back to manual |
| **Cursor** | marketplace | `forge init --agents cursor` | Cursor plugin model carries skills, not Claude-shape hooks; user-level `~/.cursor/hooks.json`, zero project writes |
| **GitHub Copilot (CLI / VS Code)** | marketplace | none (no user-level channel) | bridge is a no-op — marketplace is a distribution entry point only |
| **Windsurf** | `forge init --agents windsurf` | user-level Cascade hooks | `~/.codeium/windsurf/hooks.json` + `memories/global_rules.md` via `internal/agentbridge/windsurf.go` |
| **Kimi Code** | repo-root `.kimi-plugin/plugin.json` (`/plugins install https://github.com/MjxUpUp/Forge`) | automatic (user-level) | full event set (PreToolUse/PostToolUse/Stop/SessionStart/PostCompact/UserPromptSubmit), exit-2 block protocol; fallback `forge init --agents kimi` (config.toml marker section, stripped when the plugin is installed) |
| **OpenCode / Kiro / Cline / Gemini CLI / Mistral Vibe / Trae / Nanobot / Hermes / Antigravity / OpenClaw** | (manual, see `install.sh`) | `forge init --agents <host>` if supported | install.sh script provides one-step symlink-style per-skill/folder install for 14 hosts |

For experimental / bleeding-edge hosts, run `./plugins/forge/install.sh --help` for the full supported platform list.

## Distribution model

Forge ships as an npm binary (`@agent_forge/forge`) plus a marketplace plugin (this directory). All supported agent hosts use the same single marketplace install command — there is no per-skill vs folder symlink split because plugin marketplaces already give a unified delivery surface. This contrasts with single-skill tools (whose 14-host `install.sh` uses per-skill/folder symlinks as the actual installation primitive).

When this model stops being sufficient (e.g. agents whose marketplace can not resolve `hooks`), `forge plugin pack --agent <host>` lets us generate host-specific packs; until then, one marketplace path serves all supported agents.

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
