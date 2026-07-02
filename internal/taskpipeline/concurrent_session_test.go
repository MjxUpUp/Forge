package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// saveIncompleteStateFor writes an incomplete TaskState to .forge/tasks/.
func saveIncompleteStateFor(t *testing.T, dir, ref string) {
	t.Helper()
	s := &TaskState{TaskRef: ref, Branch: "feat/x", StartedAt: time.Now()}
	if err := SaveTaskState(dir, s); err != nil {
		t.Fatalf("save %s: %v", ref, err)
	}
}

// TestActiveTaskRef_SessionIsolation verifies the PRIMARY concurrency fix:
// two sessions on a shared checkout each resolve their own active task via a
// session-scoped active-task-ref file, and clearing one does not touch the other.
func TestActiveTaskRef_SessionIsolation(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)
	saveIncompleteStateFor(t, dir, "feat/a")
	saveIncompleteStateFor(t, dir, "feat/b")

	// Two sessions each set their own active ref.
	if err := SetActiveTaskRef(dir, "sess-A", "feat/a"); err != nil {
		t.Fatalf("SetActiveTaskRef A: %v", err)
	}
	if err := SetActiveTaskRef(dir, "sess-B", "feat/b"); err != nil {
		t.Fatalf("SetActiveTaskRef B: %v", err)
	}

	a, err := ActiveTaskState(dir, "sess-A")
	if err != nil || a == nil {
		t.Fatalf("sess-A should resolve feat/a, got %v %v", a, err)
	}
	if a.TaskRef != "feat/a" {
		t.Errorf("sess-A TaskRef = %q, want feat/a", a.TaskRef)
	}

	b, err := ActiveTaskState(dir, "sess-B")
	if err != nil || b == nil {
		t.Fatalf("sess-B should resolve feat/b, got %v %v", b, err)
	}
	if b.TaskRef != "feat/b" {
		t.Errorf("sess-B TaskRef = %q, want feat/b", b.TaskRef)
	}

	// Clearing session A must not affect session B.
	if err := ClearActiveTaskRef(dir, "sess-A"); err != nil {
		t.Fatalf("ClearActiveTaskRef A: %v", err)
	}
	b2, _ := ActiveTaskState(dir, "sess-B")
	if b2 == nil || b2.TaskRef != "feat/b" {
		t.Errorf("sess-B should still resolve feat/b after sess-A cleared, got %v", b2)
	}

	// Session A now has no active ref → fallback scan is ambiguous (2 incomplete) → nil.
	a2, _ := ActiveTaskState(dir, "sess-A")
	if a2 != nil {
		t.Errorf("sess-A after clear should be nil (ambiguous fallback), got %v", a2.TaskRef)
	}
}

// TestActiveTaskRef_EmptySession_LegacyFile verifies the backward-compat path:
// empty sessionID writes/reads the legacy global .forge/active-task-ref file and
// coexists with session-scoped files without interference.
func TestActiveTaskRef_EmptySession_LegacyFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

	if err := SetActiveTaskRef(dir, "", "feat/legacy"); err != nil {
		t.Fatalf("SetActiveTaskRef legacy: %v", err)
	}
	legacyPath := filepath.Join(dir, ".forge", "active-task-ref")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("legacy file missing: %v", err)
	}
	if string(data) != "feat/legacy" {
		t.Errorf("legacy file = %q, want feat/legacy", string(data))
	}

	// A scoped session uses a separate file and must not touch the legacy file.
	if err := SetActiveTaskRef(dir, "sess-X", "feat/scoped"); err != nil {
		t.Fatalf("SetActiveTaskRef scoped: %v", err)
	}
	if got := ReadActiveTaskRef(dir, "sess-X"); got != "feat/scoped" {
		t.Errorf("scoped read = %q, want feat/scoped", got)
	}
	data2, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("legacy file should still exist: %v", err)
	}
	if string(data2) != "feat/legacy" {
		t.Errorf("legacy file clobbered by scoped write: %q", string(data2))
	}
}

// TestEnsureSession_Scoped_UsesRealSessionID verifies that a non-empty session
// id is stored scoped and identified by that id (not a forge-generated id), with
// no idle-rotation: repeated calls are stable.
func TestEnsureSession_Scoped_UsesRealSessionID(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	s, err := EnsureSession(dir, "uuid-aaa")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if s.SessionID != "uuid-aaa" {
		t.Errorf("SessionID = %q, want uuid-aaa", s.SessionID)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "sessions", "uuid-aaa.json")); err != nil {
		t.Errorf("scoped session file missing: %v", err)
	}
	// Legacy global session.json must NOT be created on the scoped path.
	if _, err := os.Stat(filepath.Join(dir, ".forge", "session.json")); !os.IsNotExist(err) {
		t.Errorf("legacy session.json should not exist on scoped path, got err=%v", err)
	}

	// Repeated call with the same id is stable (no rotation).
	s2, err := EnsureSession(dir, "uuid-aaa")
	if err != nil {
		t.Fatalf("second EnsureSession: %v", err)
	}
	if s2.SessionID != s.SessionID {
		t.Errorf("SessionID changed: %q -> %q", s.SessionID, s2.SessionID)
	}
	if !s2.StartedAt.Equal(s.StartedAt) {
		t.Errorf("StartedAt rotated: %v -> %v (should be stable)", s.StartedAt, s2.StartedAt)
	}
}

// TestEnsureSession_Scoped_DistinctSessionsIsolated verifies two distinct real
// session ids produce two separate scoped files and records — no clobber.
func TestEnsureSession_Scoped_DistinctSessionsIsolated(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	sA, err := EnsureSession(dir, "uuid-A")
	if err != nil {
		t.Fatalf("EnsureSession A: %v", err)
	}
	sB, err := EnsureSession(dir, "uuid-B")
	if err != nil {
		t.Fatalf("EnsureSession B: %v", err)
	}
	if sA.SessionID != "uuid-A" || sB.SessionID != "uuid-B" {
		t.Errorf("session ids not isolated: %q / %q", sA.SessionID, sB.SessionID)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "sessions", "uuid-A.json")); err != nil {
		t.Errorf("scoped file A missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "sessions", "uuid-B.json")); err != nil {
		t.Errorf("scoped file B missing: %v", err)
	}
}

// TestSanitizeSessionID_StripsUnsafeChars verifies the filename is safe even if
// the session id somehow contained path/path-separator characters.
func TestSanitizeSessionID_StripsUnsafeChars(t *testing.T) {
	cases := map[string]string{
		"uuid-aaa":              "uuid-aaa",
		"a/b\\c..d":             "a_b_c_d",
		"  spaces  ":            "spaces",
		"":                      "",
		"46bde758-0ee1-4bc9-b":  "46bde758-0ee1-4bc9-b",
	}
	for in, want := range cases {
		if got := util.SanitizeSessionID(in); got != want {
			t.Errorf("util.SanitizeSessionID(%q) = %q, want %q", in, got, want)
		}
	}
}

