# Forge plugin

Forge brings loop-engineering quality gates to your AI coding agent: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion.

## Install by host

### Claude Code

Register the marketplace, then install. This wires the full gate set (hooks + MCP) for Claude Code:

    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

### Codex (CLI / App)

Codex CLI's plugin marketplace path is not officially confirmed to scan .claude-plugin/ (OpenAI docs do not specify the path). The commands below assume schema compatibility; if they fail, skip this section and run `forge init --agents codex` for full .codex gate wiring.

    codex plugin marketplace add MjxUpUp/Forge
    codex plugin install forge@forge

### Cursor

    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

Cursor's plugin model carries skills/MCP, not Claude-shape hooks. Run `forge init --agents cursor` in your project for .cursor MCP wiring.

### GitHub Copilot CLI

Copilot officially scans .claude-plugin/marketplace.json:

    copilot plugin marketplace add MjxUpUp/Forge
    copilot plugin install forge@forge

For .github/instructions + MCP, run `forge init --agents copilot`.

## Requirements

`forge` must be on PATH (hooks and the MCP server spawn `forge ...`). Install:

    npm install -g @mjxupup/forge

## What the plugin provides (Claude Code)

- hooks (`.claude-plugin/plugin.json`): PreToolUse/PostToolUse/Stop/SessionStart gates - identical to forge init's `.claude/settings.local.json`.
- MCP (`.mcp.json`): 15 forge tools (resume/decide/attach + task/board/experience).

Other hosts: the plugin is the distribution entry point; per-project gate wiring comes from `forge init --agents <host>`.
