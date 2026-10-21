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

- The `forge` CLI. The plugin depends on `@agent_forge/forge` (pinned to the plugin's
  own version), so installing it through DSH installs forge and uninstalling removes
  it — nothing to put on `PATH`. The bundled binary is not on `PATH`, so at session
  start the agent is told its absolute path to run `forge …` commands with. Override
  with `forgeBin`, or `FORGE_BIN`, to use a different forge. The project still needs
  `forge init` for task gates to have state to enforce.
- DeepSeek Harness `0.1.0-rc.7` – `0.2.1-rc.2`, Node.js ≥ 18. The bridge contract is
  pinned to `0.1.0-rc.7` ([`contract.json`](./contract.json)); `0.2.1-rc.2` is the
  desktop build this plugin runs on (pre-tool interception verified live there; a
  full five-event regression across the range is still pending).
- Windows works out of the box: the bundled binary is `forge.exe`, and a `forge.cmd`
  shim from a global npm install is spawned through `cmd.exe` (a bare spawn cannot
  execute it); timeout kills tear down the whole child tree via `taskkill /T`.

## Install

`<profile>` is the dsh profile that is actually running. **The desktop app runs the
`desktop` profile** — installing into any other profile (e.g. `web`) succeeds but the
plugin never loads in the app. List your profiles under `~/.dsh/profiles/`.

```sh
# from npm
dsh plugin --profile <profile> add @agent_forge/forge-dsh

# or straight from the Forge repo (subdirectory install)
dsh plugin --profile <profile> add "github:MjxUpUp/Forge#main&path:/plugins/forge-dsh"

# local development (run from this directory; `link:.` works on Windows too)
dsh plugin --profile <profile> add "link:."
```

The plugin hot-loads into a running dsh — adding or removing the bundle reloads the
profile. If a given build does not pick it up, restart the host once. Start a new
session to pick up the SessionStart context, and check the bridge's state with
`/forge-status` (probe result, wired groups, recent verdicts). To uninstall:
`dsh plugin --profile <profile> remove @agent_forge/forge-dsh` — the bundled forge goes
with it — or keep it installed but inert with `enabled:false`.

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
    forgeBin: forge      # binary name/path. Precedence: this key > FORGE_BIN > bundled forge > PATH `forge`
    timeoutMs: 30000     # per-hook ceiling; timeout kills and FAILS OPEN
    startWaitMs: 1500    # first pre-step waits this long for session-start contexts
    welcome: true        # one first-turn line telling the user Forge is active (false = silent)
    locale: auto         # language of the plugin's own messages: en | zh | auto (env FORGE_DSH_LOCALE, then system)
    batch: auto          # one forge process per matcher group: auto (default, needs a forge of the plugin's own version) | true | false
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

Two latency points to know. A matched group can run as **one** `forge hook batch` process
(forge runs the group's hooks in order and stops at the first block) instead of one process
per hook — roughly 2.4× faster on a measured PreToolUse/Bash group (716ms vs 1720ms on a loaded
dev machine). With the default `batch: auto` that happens only once the probe shows a forge of the
plugin's own version: batch asks forge to pick the group by the plugin's matcher string, and a forge
that disagrees would either print nothing on a block (older than the stdout fix) or silently run an
empty group, so any other forge stays on the per-hook path, which depends on hook names only. `batch:
true` forces batch, `false` forces per-hook. Whatever the mode, a batch that fails is never taken as an
allow: that group is re-run per hook for the call (hooks the batch already ran may run again;
observation-only ones are de-duplicated by forge). Three failures in a row switch batch off; a batch
that **times out** keeps just that group per-hook from then on, because batch gives the whole group one
`timeoutMs` and a single hung hook would otherwise starve the gates behind it. `/forge-status` shows
all of this. And a gate that legitimately exceeds `timeoutMs` (e.g. `auto-compile` cold-building a large
project past 30s) silently loses that run's feedback to fail-open (visible in `/forge-status`). Raise
`timeoutMs` on big projects.

A session whose gates keep failing open used to look healthy while checking nothing. After
three fail-open runs in one session the plugin says so **once**, at the next Stop that is not
already steering for a real block: how many runs lost their feedback, where to look
(`/forge-status`) and what to raise (`timeoutMs`). It is advisory — nothing is blocked. The batch
to per-hook downgrade is not counted: that run still executed its gates.

The plugin delivers the bridge and (as a dependency) the forge CLI, **not the skill library**
DSH reads from `~/.agents/skills`. When that directory holds no skill, the first-turn onboarding adds
one line offering `forge skills install --global --target agents`; the model runs it only after
the user agrees, since it writes the home directory. The offer is made once per host process, so
a user who declines is not asked again at every new session. Resume, subagents and
`welcome: false` sessions get no offer.

Whether forge takes over a project is asked once per project — the same on every host. Forge
does not infer consent from "the plugin is installed" (`init-suggest`'s `takeover` preference is
the opt-in), so a DSH install behaves like a Claude Code plugin install here; the model relays
the question from forge's own `init-suggest` advisory, which the receipt filter below never drops.

Status-only receipts that forge prints when it has nothing to say (`no weakening detected`,
`all skills SAFE`, `no .mcp.json` …) are dropped before injection — the host turns every allow
context into a synthetic user message, and these carry no instruction. Anything else, including
notes the plugin has never seen, passes through.

`/forge-status` reports a `dispatch:` line (batch or per-hook) and the probe line after the
header: `probe: ok (forge version …)`, `probe: FAILED — gates inactive (…)`, or `not run
yet` before the process's first session start. It also lists wall time per interception
point (runs, average, max) and each recent run's duration.

## Known watch items (dsh preview)

- **No stop-hook loop guard** in the preview builds (`0.1.0-rc.7` – `0.2.1-rc.2`) —
  a permanently-failing Stop gate steers at every stop boundary. forge's Stop gates
  are self-limiting once their gate passes, but a stuck gate loops the turn.
- **SessionStart context rides the first pre-step**: the detached run is awaited for at
  most `startWaitMs`; a session whose first step arrives sooner than the run finishes
  gets the context with its next step instead of the first.
- DSH is a developer preview; pin this plugin's version alongside your dsh version.

## Development

```sh
npm install --no-package-lock   # @deepseek-ai/cordis for the wiring tests (+ the bundled forge, unused by them)
npm test      # mapping, runner (incl. cross-OS planSpawn units), decision folding, real-cordis wiring
```

`test/doubles/fake-forge.mjs` stands in for the forge binary; the wiring suite
boots a real cordis runtime and asserts dispatch order, short-circuit semantics,
payload shape, and every decision mapping.
