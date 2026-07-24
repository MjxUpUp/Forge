# Forge plugin

Forge brings loop-engineering quality gates to your AI coding agent: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion.

## Three-step setup

Forge has two parts: a Go binary (the engine that hooks spawn) and this plugin (the wiring that tells your agent where to call it). Install the binary first, then the plugin, then init each project.

### 1. Install the forge binary (required, once per machine)

Hooks spawn `forge ...`, so the binary must be on PATH before the plugin can do anything.

    npm install -g @agent_forge/forge

### 2. Install the plugin (once per agent)

Register the marketplace, then install. This wires the gate set (hooks) at the user level — every project on this machine gets the plugin wiring, with no per-project plugin install. Project assets (.forge/, protocol, skills) still need forge init (see step 3).

#### Claude Code

    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

#### Codex (CLI / App)

Codex CLI's plugin marketplace path is not officially confirmed to scan .claude-plugin/ (OpenAI docs do not specify the path). The commands below assume schema compatibility; if they fail, skip this section and run `forge init --agents codex` for full .codex gate wiring.

    codex plugin marketplace add MjxUpUp/Forge
    codex plugin install forge@forge

#### Cursor

    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

Cursor's plugin model carries skills, not Claude-shape hooks. Run `forge init --agents cursor` in your project for .cursor gate wiring.

#### GitHub Copilot CLI

Copilot officially scans .claude-plugin/marketplace.json:

    copilot plugin marketplace add MjxUpUp/Forge
    copilot plugin install forge@forge

For .github/instructions gate wiring, run `forge init --agents copilot`.

### 3. Initialize each project (once per project)

The plugin wires user-level hooks. It does NOT create the project-level assets forge needs to run: the `.forge/` task state, the `CLAUDE.md`/`AGENTS.md` protocol, and the canonical skills (`/forge-quality`, ...). Generate them per project:

    cd your-project
    forge init

Complete setup: binary (machine) -> plugin (agent) -> init (project).

## What the plugin provides

Claude Code (full): hooks (`.claude-plugin/plugin.json`) = PreToolUse/PostToolUse/Stop/SessionStart gates, identical to forge init's `.claude/settings.local.json` but user-level (all projects).

Because the plugin already wires user-level hooks, `forge init` auto-dedupes the duplicates when the plugin is installed — Claude Code would otherwise double-run hooks. This covers both the project-level (`.claude/settings.local.json` hooks) and the user-level (`~/.claude`/`$CLAUDE_CONFIG_DIR` `settings.local.json` forge hooks, left over from a historical global `forge init` in the home dir or an old global install). Existing projects are migrated automatically by the init-suggest SessionStart hook via `forge plugin dedupe --keep-empty` (which also cleans the user-level file). `settings.local.json` (both levels) is preserved as an empty `{}` shell — it is user-placed gitignored config, never silently deleted (the user-level file is always preserved regardless of `--keep-empty`, since it is the user's global config).

Other hosts: the plugin is the distribution entry point (marketplace listing); per-project gate wiring (hooks, .forge/, protocol) comes from `forge init --agents <host>`.

## Caveat: projects you do not want forge in

User-level hooks fire in every Claude Code project. In git projects without `.forge/`, the **init-suggest** SessionStart hook detects this and prompts the agent to ask the user whether to run `forge init` (one-shot `suggested` marker so it asks only once). To permanently silence the prompt for a specific project, run `forge suggest decline` there. To remove forge entirely from a project, `forge init --reset` (clean) or uninstall the plugin.

## Supported hosts (out of the box)

| Host | Plugin install | Per-project gate wiring | Notes |
|------|----------------|------------------------|-------|
| **Claude Code** | `plugin.json` marketplace | automatic (user-level) | full hooks; auto-init via `init-suggest` SessionStart hook |
| **Codex (CLI / App)** | marketplace (path not officially confirmed) | `forge init --agents codex` | if marketplace path fails, fall back to manual |
| **Cursor** | marketplace | `forge init --agents cursor` | Cursor plugin model carries skills, not Claude-shape hooks |
| **GitHub Copilot (CLI / VS Code)** | marketplace + `.copilot-plugin/` | `forge init --agents copilot` (CLI) | VS Code auto-discovers `.copilot-plugin/plugin.json` if you open this repo |
| **Windsurf** | (mirrored `buildWindsurfHooks` in code) | (Cascade hooks) | mirrors Claude SessionStart + write hooks via `internal/agentbridge/windsurf.go` |
| **OpenCode / Kiro / Cline / Gemini CLI / Mistral Vibe / Trae / Nanobot / Hermes / Antigravity / OpenClaw** | (manual, see `install.sh`) | `forge init --agents <host>` if supported | install.sh script provides one-step symlink-style per-skill/folder install for 14 hosts inspired by [Understand-Anything](https://github.com/Egonex-AI/Understand-Anything) |

For experimental / bleeding-edge hosts, run `./plugins/forge/install.sh --help` for the full supported platform list.

## Distribution model

Forge ships as an npm binary (`@agent_forge/forge`) plus a marketplace plugin (this directory). All supported agent hosts use the same single marketplace install command — there is no per-skill vs folder symlink split because plugin marketplaces already give a unified delivery surface. This contrasts with single-skill tools (e.g. [Understand-Anything](https://github.com/Egonex-AI/Understand-Anything) 14-host `install.sh` with per-skill/folder symlinks) where the symlink style is the actual installation primitive.

When this model stops being sufficient (e.g. agents whose marketplace can not resolve `hooks`), `forge plugin pack --agent <host>` lets us generate host-specific packs; until then, one marketplace path serves all supported agents.

## Developing locally (cache copy, not symlinks)

Claude Code plugin cache (`~/.claude/plugins/cache/forge/forge/<version>/`) does **not** follow symlinks — `Search`/`Glob` tools in the agent skip symlinked dirs. To test local plugin changes:

1. Rebuild after changes: `go build ./...`
2. Find current version: `cat plugins/forge/.claude-plugin/plugin.json | jq -r .version`
3. Copy the freshly-built assets into the cache, replacing `<VERSION>`:

```bash
VERSION=$(jq -r .version plugins/forge/.claude-plugin/plugin.json)
rm -rf "$HOME/.claude/plugins/cache/forge/forge/$VERSION"
mkdir -p "$HOME/.claude/plugins/cache/forge/forge/$VERSION"
cp -R plugins/forge/* "$HOME/.claude/plugins/cache/forge/forge/$VERSION/"
```

4. Start a fresh Claude Code session (existing sessions keep old prompts in context).
5. Verify by opening any git project — the `init-suggest` SessionStart hook should fire.

This pattern was inspired by Understand-Anything CLAUDE.md (2026-07-04): symlinks do not work because Claude Search/Glob tools can not follow them.
