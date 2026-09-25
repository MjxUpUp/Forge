package toolusage

import (
	"testing"
	"time"
)

func TestComputeID_StableAndDistinct(t *testing.T) {
	ts := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	a := ToolCall{ToolName: "Bash", TaskRef: "feat/x", SessionID: "s1", Timestamp: ts}
	b := ToolCall{ToolName: "Bash", TaskRef: "feat/x", SessionID: "s1", Timestamp: ts}
	// Same identity fields => same ID (deterministic).
	if computeID(a) != computeID(b) {
		t.Fatal("computeID must be deterministic for identical identity fields")
	}
	// Different tool name => different ID.
	c := a
	c.ToolName = "Read"
	if computeID(a) == computeID(c) {
		t.Fatal("computeID must differ when tool name differs")
	}
	// Different timestamp => different ID.
	d := a
	d.Timestamp = ts.Add(time.Second)
	if computeID(a) == computeID(d) {
		t.Fatal("computeID must differ when timestamp differs")
	}
	// 12-char hex.
	if len(computeID(a)) != 12 {
		t.Errorf("computeID must be 12 hex chars, got %q (len %d)", computeID(a), len(computeID(a)))
	}
}

func TestEnsureID_BackfillsAndPreserves(t *testing.T) {
	ts := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	// Legacy entry (no ID) gets backfilled with the computed ID.
	legacy := ToolCall{ToolName: "Bash", TaskRef: "feat/x", SessionID: "s1", Timestamp: ts}
	got := ensureID(legacy)
	if got == "" {
		t.Fatal("ensureID must backfill an empty ID")
	}
	if got != computeID(legacy) {
		t.Fatal("ensureID backfill must match computeID for a legacy entry")
	}
	// Existing ID is preserved, not recomputed.
	const preset = "preset1234567"
	withID := legacy
	withID.ID = preset
	if g := ensureID(withID); g != preset {
		t.Fatalf("ensureID must preserve existing ID, got %q want %q", g, preset)
	}
}
