# Forge plugin

Forge brings loop-engineering quality gates to your AI coding agent: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion.

## Three-step setup

Forge has two parts: a Go binary (the engine that hooks and MCP spawn) and this plugin (the wiring that tells your agent where to call it). Install the binary first, then the plugin, then init each project.

### 1. Install the forge binary (required, once per machine)

Hooks and the MCP server spawn `forge ...`, so the binary must be on PATH before the plugin can do anything.

    npm install -g @agent_forge/forge

### 2. Install the plugin (once per agent)

Register the marketplace, then install. This wires the gate set (hooks + MCP) at the user level — every project on this machine gets the plugin wiring, with no per-project plugin install. Project assets (.forge/, protocol, skills) still need forge init (see step 3).

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

Cursor's plugin model carries skills/MCP, not Claude-shape hooks. Run `forge init --agents cursor` in your project for .cursor MCP wiring.

#### GitHub Copilot CLI

Copilot officially scans .claude-plugin/marketplace.json:

    copilot plugin marketplace add MjxUpUp/Forge
    copilot plugin install forge@forge

For .github/instructions + MCP, run `forge init --agents copilot`.

### 3. Initialize each project (once per project)

The plugin wires user-level hooks + MCP. It does NOT create the project-level assets forge needs to run: the `.forge/` pipeline/task state, the `CLAUDE.md`/`AGENTS.md` protocol, and the canonical skills (`/forge-pipeline`, `/forge-quality`, ...). Generate them per project:

    cd your-project
    forge init

Complete setup: binary (machine) -> plugin (agent) -> init (project).

## What the plugin provides

Claude Code (full): hooks (`.claude-plugin/plugin.json`) = PreToolUse/PostToolUse/Stop/SessionStart gates, identical to forge init's `.claude/settings.local.json` but user-level (all projects); MCP (`.mcp.json`) = 15 forge tools (resume/decide/attach + task/board/experience).

Other hosts: the plugin is the distribution entry point (MCP + marketplace listing); per-project gate wiring (hooks, .forge/, protocol) comes from `forge init --agents <host>`.

## Caveat: projects you do not want forge in

User-level hooks fire in every Claude Code project. In projects without `.forge/`, task-guard emits a WARN (`allowed but not tracked`) on each source edit — noisy but non-blocking. To silence, either `forge init` the project or uninstall the plugin.
