package taskpipeline

import (
	"fmt"
	"strings"
)

// Gate message contract: gate outcomes carry one of two unambiguous prefixes
// so an agent never has to parse prose to know whether a gate stopped it.
//
//	BLOCKED:  — hard failure, non-zero exit. The gate did NOT pass. Fix and re-run.
//	ADVISORY: — soft signal, exit 0 (the gate still passed). Recorded to checklog;
//	           "should fix, won't block".
//
// A passed gate prints "✅ <gate> — passed" and needs no prefix.
//
// This exists because of the M2 failure mode: a hard read-before-edit error
// ("Read and understand the code before modifying it") was read as soft advice
// and the agent proceeded anyway — the non-zero exit was ignored because the
// wording read like a reminder. The BLOCKED: prefix + explicit "HARD stop, not
// a reminder" makes the contract unmistakable, and pairs with ADVISORY: for the
// soft path. Mirrors the industry "signal-over-noise" / "Block pattern"
// consensus: pass is silent, fail is loud, and the exit code — not the wording —
// is the contract an agent must obey.
const (
	// blockedPrefix marks a hard, non-zero-exit gate failure.
	blockedPrefix = "BLOCKED: "
	// advisoryPrefix marks a soft, exit-0 gate signal (logged, non-blocking).
	advisoryPrefix = "ADVISORY: "
)

// GateBlocked wraps an agent-actionable hard gate failure with the BLOCKED
// contract. The returned error propagates to a non-zero process exit; the
// prefix makes the stop unambiguous so it cannot be misread as soft advice.
// Use for behavioral gates (read-before-edit, work-activity, prerequisites,
// review) — NOT for infrastructure errors (unknown gate id, command execution
// failures), which stay plain fmt.Errorf.
func GateBlocked(format string, args ...any) error {
	return fmt.Errorf(blockedPrefix+format, args...)
}

// IsGateBlocked reports whether err is a hard BLOCKED gate failure. Used by
// callers/hooks that branch on the contract rather than on a bare non-zero exit.
func IsGateBlocked(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), blockedPrefix)
}

// GateAdvisory formats a soft, non-blocking gate signal. The caller still
// returns nil / exit 0; the ADVISORY: prefix tells the agent this is "should
// fix, won't block" — distinct from a BLOCKED hard failure. Intended for
// agent-actionable soft signals (missing tests, docs drift), not internal
// diagnostics (grace-window notes, persist warnings) which stay [forge] note.
func GateAdvisory(format string, args ...any) string {
	return advisoryPrefix + fmt.Sprintf(format, args...)
}
