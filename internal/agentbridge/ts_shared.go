package agentbridge

import _ "embed"

// tsSharedForgeSpawn is the TypeScript `runForge` function shared by the opencode
// and pi translators. Both generated TS files spawn `forge hook <name>` with
// Claude-Code-shape stdin and must read forge's block verdict from the JSON
// decision field identically — centralizing it here means a change to the spawn
// protocol (or the block-detection contract) lands in one place.
//
// Embedded from forge_spawn.ts (a real .ts file) rather than a Go raw string so
// the shared snippet is itself TypeScript-valid and type-checked by the
// generator tests (TestGeneratedTSCompiles) — a raw string with embedded
// backticks would be fragile to edit and unverified.
//
// Contract (see internal/cli/hook.go runHook): forge ALWAYS emits one JSON line
// to stdout, {decision:"approve"|"block", hookSpecificOutput?:{additionalContext}}.
// Block is read from decision — NOT an exit code, because cobra surfaces forge's
// internal errors as exit 1, indistinguishable from a deny. Parse failures and
// spawn errors fail open (allow) so a forge outage never locks the agent out of
// its own tooling.
//
//go:embed forge_spawn.ts
var tsSharedForgeSpawn string
