# @agent_forge/forge-dsh

Forge quality gates for **DeepSeek Harness (dsh)** — task gates, read-before-edit,
bash hazard interception, and quality scoring, enforced inside DSH sessions through
the harness's typed interception points.

[Forge](https://github.com/MjxUpUp/Forge) is an AI-code quality gate engine. This
plugin is a thin Cordis wrapper: it forwards DSH tool/session events to the `forge`
CLI and translates forge's verdicts back into DSH decisions. All gate logic lives in
the forge binary — the plugin itself has **zero runtime dependencies**.

## How it works

> The mapping table below and the fail-open contract at the end of this README
> are the **two-sided bridge contract**, mirrored machine-readably in
> [`contract.json`](./contract.json) and enforced by forge's compat snapshot
> (surface `bridges`). Change all three together: `contract.json`, this README,
> and the tests on both sides.

| DSH interception point | forge hook event | A forge block becomes |
|---|---|---|
| `tools/pre-execute` | `PreToolUse` | `{kind:'deny', reason}` |
| `tools/post-execute` | `PostToolUse` | `{kind:'block', feedback}` (error result) |
| `agent/pre-step` | `UserPromptSubmit` | `{kind:'reject'}` |
| `agent/session-start` | `SessionStart`¹ | context queued, delivered with the next `agent/pre-step`² |
| `agent/turn-stopping` | `Stop` | `agent.steer(reason)` → another step |

¹ `source:'compact'` additionally fires forge's `PostCompact` group — the preview
builds expose no dedicated compaction point.

² `agent.inject()` at session start was observed to drop the contexts silently, so they ride the
first pre-step instead; that pre-step waits up to `startWaitMs` (default 1500) for the detached
session-start run, and a rejected step keeps the queue for the next one.

DSH tool names map onto the Claude Code names forge dispatches on
(`write/edit/str_replace_editor→Write/Edit`, `bash/pwsh→Bash`, `read/read_image→Read`,
`skill→Skill`); unmapped tools pass ungated. The wired hook roster mirrors
forge's canonical spec (`lib/spec.json`, drift-guarded by a Go test in the Forge
repo) — freeze-guard, task-guard, assertion-check, read-before-edit, bash-guard,
hazard-guard, auto-compile, workflow-test-guard, file-sentinel, tool-track,
test-nudge, failure-track (PostToolUseFailure), subagent-track (SubagentStop),
task-verify, review-stop, skill-trigger, doc-lint, conventions-write, gate-cmd-form,
task-drift, resume-reinject, compact-resume, conventions-context, and the
session-start group.
failure-track/subagent-track are mirrored in the spec but inert until DSH
exposes the corresponding event points (the preview builds have neither).

## Requirements

- The `forge` CLI on `PATH` (`npm install -g @agent_forge/forge`), with the project
  initialized (`forge init`) for task gates to have state to enforce.
- DeepSeek Harness `0.1.0-rc.7` – `0.2.1-rc.2`, Node.js ≥ 18. The bridge contract is
  pinned to `0.1.0-rc.7` ([`contract.json`](./contract.json)); `0.2.1-rc.2` is the
  desktop build this plugin runs on (pre-tool interception verified live there; a
  full five-event regression across the range is still pending).
- Windows works out of the box: npm lays out `forge` as a `forge.cmd` shim, which
  the plugin spawns through `cmd.exe` (a bare spawn cannot execute it); timeout
  kills tear down the whole child tree via `taskkill /T`.

## Install

```sh
# from npm
dsh plugin --profile web add @agent_forge/forge-dsh

# or straight from the Forge repo (subdirectory install)
dsh plugin --profile web add "github:MjxUpUp/Forge#main&path:/plugins/forge-dsh"

# local development (run from this directory; `link:.` works on Windows too)
dsh plugin --profile web add "link:."
```

The plugin hot-loads into a running dsh — adding or removing the bundle reloads the
profile. If a given build does not pick it up, restart the host once. Start a new
session to pick up the SessionStart context, and check the bridge's state with
`/forge-status` (probe result, wired groups, recent verdicts). To uninstall:
`dsh plugin --profile <name> remove @agent_forge/forge-dsh` (the desktop app's profile
is `desktop`), or keep it installed but inert with `enabled:false`.

## Config

Keys go on the bundle entry in the profile's `cordis.patch.yml` — the user-editable
patch layer. Do not edit the generated `cordis.yml`: the loader rewrites that root on
every boot, so anything written there is lost (the desktop app manages its own profile
under `~/.dsh/profiles/<profile>/`):
```yaml
# ~/.dsh/profiles/<profile>/cordis.patch.yml
- id: forge-quality
  name: "@agent_forge/forge-dsh"
  config:
    forgeBin: forge      # binary name/path; FORGE_BIN only fills in when this key is absent
    timeoutMs: 30000     # per-hook ceiling; timeout kills and FAILS OPEN
    startWaitMs: 1500    # first pre-step waits this long for session-start contexts
    enabled: true        # false unwires every listener
    debug: false         # log fail-open hook errors to console.error
```

## Fail-open contract

Blocks are read from forge's stdout JSON (`decision:"block"`), never inferred from
exit codes: the verdict reason and its `additionalContext` exist only in that JSON,
and forge reuses exit 2 for hard errors as well. Every infrastructure failure (forge
missing, spawn error, unparseable output, timeout) **fails open**: a forge outage
never locks the agent out of its own tools. Such hook-run failures are silent by
design; surface them with `/forge-status` or `debug:true`.

A missing `forge` binary is the one exception to that silence: each session start
probes `forge --version` once (until a probe succeeds — the answer is then cached
for the process lifetime) and, when the probe fails, queues a visible notice
(delivered with the first pre-step) naming the configured `forgeBin`. Without it an uninstalled CLI would look exactly
like a healthy plugin gating nothing. The notice is informational, not a switch —
while the binary is missing, every hook attempt fails open on its own, so gates are
inactive whether or not the probe ran. Installing forge needs no host restart: the
next session probes again and gating resumes.

Two latency trade-offs to know: hooks run **serially** per event (up to six per
group), so a slow hook chain delays the tool call — and a gate that legitimately
exceeds `timeoutMs` (e.g. `auto-compile` cold-building a large project past 30s)
silently loses that run's feedback to fail-open (visible in `/forge-status`).
Raise `timeoutMs` on big projects.

`/forge-status` reports the probe line after the header: `probe: ok (forge version …)`,
`probe: FAILED — gates inactive (…)`, or `not run yet` before the process's first
session start.

## Known watch items (dsh preview)

- **No stop-hook loop guard** in the preview builds (`0.1.0-rc.7` – `0.2.1-rc.2`) —
  a permanently-failing Stop gate steers at every stop boundary. forge's Stop gates
  are self-limiting once their gate passes, but a stuck gate loops the turn.
- **SessionStart context is best-effort** (emit mode, detached): it may miss the
  first request of a very short-lived session — same limitation the official
  bridges document (`TODO(session-start-gating)`).
- DSH is a developer preview; pin this plugin's version alongside your dsh version.

## Development

```sh
npm install   # dev-only: @deepseek-ai/cordis for the wiring tests
npm test      # mapping, runner (incl. cross-OS planSpawn units), decision folding, real-cordis wiring
```

`test/doubles/fake-forge.mjs` stands in for the forge binary; the wiring suite
boots a real cordis runtime and asserts dispatch order, short-circuit semantics,
payload shape, and every decision mapping.
