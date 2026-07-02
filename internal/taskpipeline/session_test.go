package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

func TestEnsureSession_CreatesNewSession(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	session, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if session == nil {
		t.Fatal("session is nil")
	}
	if session.SessionID == "" {
		t.Error("SessionID is empty")
	}
	if session.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
	if session.AgentType != "claude-code" {
		t.Errorf("AgentType = %q, want claude-code", session.AgentType)
	}

	// Verify session file was created
	if _, err := os.Stat(filepath.Join(dir, ".forge", "session.json")); os.IsNotExist(err) {
		t.Error("session.json was not created")
	}

	// Verify sessions log was created
	if _, err := os.Stat(filepath.Join(dir, ".forge", "sessions.jsonl")); os.IsNotExist(err) {
		t.Error("sessions.jsonl was not created")
	}
}

// TestEnsureSession_WritesStartedEpoch guards the integer started_epoch field.
// The session-health bash hook reads this field directly (via extract_num) to
// avoid parsing the RFC3339Nano started_at string with the cross-platform-fragile
// date command (GNU vs BSD). Both construction paths — the legacy global
// session.json and the session-scoped per-id file — must populate it, and it must
// be the exact Unix-seconds form of StartedAt so the Go→bash contract is an
// integer, not a format bash has to reverse.
func TestEnsureSession_WritesStartedEpoch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	// Legacy path (empty sessionID → global session.json).
	legacy, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession legacy: %v", err)
	}
	if legacy.StartedEpoch != legacy.StartedAt.Unix() {
		t.Errorf("legacy StartedEpoch=%d, want StartedAt.Unix()=%d (hook reads this int directly)",
			legacy.StartedEpoch, legacy.StartedAt.Unix())
	}
	if legacy.StartedEpoch == 0 {
		t.Error("legacy StartedEpoch is zero — hook would fall back to the fragile date parse")
	}

	// Scoped path (non-empty sessionID → per-id file under .forge/sessions/).
	const scopedID = "scoped-abc-123"
	scoped, err := EnsureSession(dir, scopedID)
	if err != nil {
		t.Fatalf("EnsureSession scoped: %v", err)
	}
	if scoped.StartedEpoch != scoped.StartedAt.Unix() {
		t.Errorf("scoped StartedEpoch=%d, want StartedAt.Unix()=%d",
			scoped.StartedEpoch, scoped.StartedAt.Unix())
	}

	// The on-disk global session.json must marshal the field by its json tag so
	// the bash hook's extract_num started_epoch finds it.
	raw, err := os.ReadFile(filepath.Join(dir, ".forge", "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	if !strings.Contains(string(raw), `"started_epoch":`) {
		t.Errorf("session.json missing started_epoch field; bash hook would not find it.\ngot: %s", raw)
	}
}

func TestEnsureSession_ReusesExistingWithinMaxIdle(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	first, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("first EnsureSession: %v", err)
	}

	// Second call within maxIdle should return the same session
	second, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("second EnsureSession: %v", err)
	}

	if second.SessionID != first.SessionID {
		t.Errorf("SessionID changed: %q -> %q (should stay the same)", first.SessionID, second.SessionID)
	}
}

func TestEnsureSession_RotatesAfterMaxIdle(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	first, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("first EnsureSession: %v", err)
	}

	// Manually set the session start time far in the past to simulate expiration
	oldSession := &SessionRecord{
		SessionID: first.SessionID,
		StartedAt: time.Now().Add(-3 * time.Hour),
		AgentType: first.AgentType,
	}
	if err := saveSession(dir, oldSession); err != nil {
		t.Fatalf("saveSession: %v", err)
	}

	// Next call should create a new session
	second, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("second EnsureSession: %v", err)
	}

	if second.SessionID == first.SessionID {
		t.Errorf("SessionID should have rotated, but got same: %q", second.SessionID)
	}
}

func TestLoadSessions_IncludesCurrent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	// Create a session
	session, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	sessions, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("LoadSessions count = %d, want 1", len(sessions))
	}
	if sessions[0].SessionID != session.SessionID {
		t.Errorf("SessionID = %q, want %q", sessions[0].SessionID, session.SessionID)
	}
}

func TestLoadSessions_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	sessions, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if sessions != nil {
		t.Fatalf("LoadSessions should return nil for empty dir, got %d entries", len(sessions))
	}
}

func TestSessionAgentTypeDetection(t *testing.T) {
	dir := t.TempDir()
	if got := detectAgentType(dir); got != "" {
		t.Errorf("detectAgentType on empty dir = %q, want empty", got)
	}

	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	if got := detectAgentType(dir); got != "claude-code" {
		t.Errorf("detectAgentType on .claude dir = %q, want claude-code", got)
	}
}

func TestNewTaskState_HasSessionID(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	session, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	ctx := &taskcontext.Context{
		Source:     "explicit",
		TaskRef:    "PROJ-456",
		Branch:     "feature/PROJ-456",
		DetectedAt: time.Now(),
	}
	state := NewTaskState(ctx)
	state.SessionID = session.SessionID

	// Save and reload to verify persistence
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	loaded, err := LoadTaskState(dir, "PROJ-456")
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}
	if loaded.SessionID != session.SessionID {
		t.Errorf("TaskState.SessionID = %q, want %q", loaded.SessionID, session.SessionID)
	}
}
