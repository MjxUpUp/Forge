# @agent_forge/forge-dsh

Forge quality gates for **DeepSeek Harness (dsh)** — task gates, read-before-edit,
bash hazard interception, and quality scoring, enforced inside DSH sessions through
the harness's typed interception points.

[Forge](https://github.com/MjxUpUp/Forge) is an AI-code quality gate engine. This
plugin is a thin Cordis wrapper: it forwards DSH tool/session events to the `forge`
CLI and translates forge's verdicts back into DSH decisions. All gate logic lives in
the forge binary — the plugin itself has **zero runtime dependencies**.

## How it works

| DSH interception point | forge hook event | A forge block becomes |
|---|---|---|
| `tools/pre-execute` | `PreToolUse` | `{kind:'deny', reason}` |
| `tools/post-execute` | `PostToolUse` | `{kind:'block', feedback}` (error result) |
| `agent/pre-step` | `UserPromptSubmit` | `{kind:'reject'}` |
| `agent/session-start` | `SessionStart`¹ | context via `agent.inject()` |
| `agent/turn-stopping` | `Stop` | `agent.steer(reason)` → another step |

¹ `source:'compact'` additionally fires forge's `PostCompact` group — DSH rc.7
exposes no dedicated compaction point.

DSH tool names map onto the Claude Code names forge dispatches on
(`write/edit/str_replace_editor→Write/Edit`, `bash/pwsh→Bash`, `read→Read`,
`skill→Skill`); unmapped tools pass ungated. The wired hook roster mirrors
forge's canonical spec (`lib/spec.json`, drift-guarded by a Go test in the Forge
repo) — freeze-guard, task-guard, assertion-check, read-before-edit, bash-guard,
hazard-guard, auto-compile, workflow-test-guard, file-sentinel, tool-track,
task-verify, review-stop, skill-trigger, and the session-start group.

## Requirements

- The `forge` CLI on `PATH` (`npm install -g @agent_forge/forge`), with the project
  initialized (`forge init`) for task gates to have state to enforce.
- DeepSeek Harness `0.1.0-rc.x` (verified against `0.1.0-rc.7`), Node.js ≥ 18.

## Install

```sh
# from npm
dsh plugin --profile web add @agent_forge/forge-dsh

# or straight from the Forge repo (subdirectory install)
dsh plugin --profile web add "github:MjxUpUp/Forge#main&path:/plugins/forge-dsh"

# local development
dsh plugin --profile web add "link:$(pwd)"
```

Restart `dsh web` after install. Check status inside a session with `/forge-status`.

## Config

```yaml
# profile patch
- insert:
    - id: forge-quality
      name: "@agent_forge/forge-dsh"
      config:
        forgeBin: forge      # binary name/path (env FORGE_BIN overrides the default)
        timeoutMs: 30000     # per-hook ceiling; timeout kills and FAILS OPEN
        enabled: true        # false unwires every listener
        debug: false         # log fail-open hook errors to console.error
```

## Fail-open contract

Blocks are read from forge's stdout JSON (`decision:"block"`), never from exit
codes — a forge internal error (exit 1) is indistinguishable from a deny otherwise.
Every infrastructure failure (forge missing, spawn error, unparseable output,
timeout) **fails open**: a forge outage never locks the agent out of its own tools.
Such failures are silent by design; surface them with `/forge-status` or `debug:true`.

Two latency trade-offs to know: hooks run **serially** per event (up to five per
group), so a slow hook chain delays the tool call — and a gate that legitimately
exceeds `timeoutMs` (e.g. `auto-compile` cold-building a large project past 30s)
silently loses that run's feedback to fail-open (visible in `/forge-status`).
Raise `timeoutMs` on big projects.

## Known watch items (dsh preview)

- **No stop-hook loop guard** in rc.7 — a permanently-failing Stop gate steers at
  every stop boundary. forge's Stop gates are self-limiting once their gate passes,
  but a stuck gate loops the turn.
- **SessionStart context is best-effort** (emit mode, detached): it may miss the
  first request of a very short-lived session — same limitation the official
  bridges document (`TODO(session-start-gating)`).
- DSH is a developer preview; pin this plugin's version alongside your dsh version.

## Development

```sh
npm install   # dev-only: @deepseek-ai/cordis for the wiring tests
npm test      # 33 tests: mapping, runner, decision folding, real-cordis wiring
```

`test/doubles/fake-forge.mjs` stands in for the forge binary; the wiring suite
boots a real cordis runtime and asserts dispatch order, short-circuit semantics,
payload shape, and every decision mapping.
