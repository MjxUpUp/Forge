package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// saveIncompleteStateFor writes an incomplete TaskState to DataDir/tasks/.
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

// TestHasActiveTaskFromOtherSession verifies the concurrent-session detection
// used by the review-stop hook: when another session has an active task, the
// global git diff belongs to that session — the current session should not block.
//
// IMPORTANT: each test case uses FORGE_DATA_HOME=t.TempDir() to isolate from the
// real user's forge DataDir (~/.forge/projects/<key>/). Without this, DataDirFor
// resolves to the real DataDir because t.TempDir() is a subdirectory of the git
// repo, and the test reads real session data (false positives).
func TestHasActiveTaskFromOtherSession(t *testing.T) {
	isolatedRoot := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		t.Setenv("FORGE_DATA_HOME", dir)
		return dir
	}

	t.Run("other session active", func(t *testing.T) {
		dir := isolatedRoot(t)
		if err := SetActiveTaskRef(dir, "sess-A", "feat/x"); err != nil {
			t.Fatal(err)
		}
		if !HasActiveTaskFromOtherSession(dir, "sess-B") {
			t.Error("sess-B should detect sess-A's active task")
		}
	})

	t.Run("only own session", func(t *testing.T) {
		dir := isolatedRoot(t)
		if err := SetActiveTaskRef(dir, "sess-C", "feat/y"); err != nil {
			t.Fatal(err)
		}
		if HasActiveTaskFromOtherSession(dir, "sess-C") {
			t.Error("should not detect own session's file as other")
		}
	})

	t.Run("no sessions", func(t *testing.T) {
		dir := isolatedRoot(t)
		if HasActiveTaskFromOtherSession(dir, "sess-Z") {
			t.Error("should be false when no active-task-ref files exist")
		}
	})

	t.Run("empty sessionID", func(t *testing.T) {
		dir := isolatedRoot(t)
		if err := SetActiveTaskRef(dir, "sess-D", "feat/z"); err != nil {
			t.Fatal(err)
		}
		if HasActiveTaskFromOtherSession(dir, "") {
			t.Error("empty sessionID should return false (legacy mode)")
		}
	})

	t.Run("stale empty file not counted", func(t *testing.T) {
		dir := isolatedRoot(t)
		if err := SetActiveTaskRef(dir, "sess-E", "feat/e"); err != nil {
			t.Fatal(err)
		}
		// Overwrite with zero bytes via the correct DataDir path (FORGE_DATA_HOME/<key>/)
		refPath := activeTaskRefPath(dir, "sess-E")
		if err := os.WriteFile(refPath, []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		if HasActiveTaskFromOtherSession(dir, "sess-F") {
			t.Error("zero-size file should not be counted as active")
		}
	})

	// Crash-orphan path: a session that died mid-task leaves a non-empty
	// active-task-ref-<sid> that ClearActiveTaskRef never removes. Without the
	// TTL guard, orphans accumulate and disable the concurrent-session check.
	// Backdate mtime beyond otherSessionActiveTTL → must not be counted.
	t.Run("stale orphan from crashed session not counted", func(t *testing.T) {
		dir := isolatedRoot(t)
		if err := SetActiveTaskRef(dir, "sess-orphan", "feat/dead"); err != nil {
			t.Fatal(err)
		}
		refPath := activeTaskRefPath(dir, "sess-orphan")
		stale := time.Now().Add(-otherSessionActiveTTL - time.Hour)
		if err := os.Chtimes(refPath, stale, stale); err != nil {
			t.Fatal(err)
		}
		if HasActiveTaskFromOtherSession(dir, "sess-other") {
			t.Error("stale crash-orphan (mtime beyond TTL) should not be counted as active")
		}
	})

	t.Run("legacy file excluded", func(t *testing.T) {
		dir := isolatedRoot(t)
		// Write via activeTaskRefPath("") for the correct legacy path
		legacyPath := activeTaskRefPath(dir, "")
		os.MkdirAll(filepath.Dir(legacyPath), 0755)
		if err := os.WriteFile(legacyPath, []byte("feat/legacy"), 0644); err != nil {
			t.Fatal(err)
		}
		if HasActiveTaskFromOtherSession(dir, "sess-G") {
			t.Error("legacy global file should not be counted")
		}
	})

	t.Run("mixed sessions integrated", func(t *testing.T) {
		dir := isolatedRoot(t)
		if err := SetActiveTaskRef(dir, "sess-H", "feat/h"); err != nil {
			t.Fatal(err)
		}
		// sess-I has no task — should detect sess-H
		if !HasActiveTaskFromOtherSession(dir, "sess-I") {
			t.Error("sess-I (research, no task) should detect sess-H's active task")
		}
		// sess-H's own view
		if HasActiveTaskFromOtherSession(dir, "sess-H") {
			t.Error("sess-H should not see other session")
		}
		// After clearing sess-H, no other sessions remain
		if err := ClearActiveTaskRef(dir, "sess-H"); err != nil {
			t.Fatal(err)
		}
		if HasActiveTaskFromOtherSession(dir, "sess-I") {
			t.Error("after clearing, no active sessions remain")
		}
	})
}

// TestSanitizeSessionID_StripsUnsafeChars verifies the filename is safe even if
// the session id somehow contained path/path-separator characters. Empty input
// yields the documented "session" fallback (util.SanitizeSessionID: 若结果为空则
// 回退到 session) — in-package callers never pass "" (all call sites guard on
// sessionID != ""), so this only pins the cross-package contract.
func TestSanitizeSessionID_StripsUnsafeChars(t *testing.T) {
	cases := map[string]string{
		"uuid-aaa":             "uuid-aaa",
		"a/b\\c..d":            "a_b_c_d",
		"  spaces  ":           "spaces",
		"":                     "session",
		"46bde758-0ee1-4bc9-b": "46bde758-0ee1-4bc9-b",
	}
	for in, want := range cases {
		if got := util.SanitizeSessionID(in); got != want {
			t.Errorf("util.SanitizeSessionID(%q) = %q, want %q", in, got, want)
		}
	}
}
