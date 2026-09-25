package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hlc"
	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// TestEnsureSession_StampsMachineAttribution 在 sessions 收口点钉死事件打戳契约
// （node-identity.md §4）：追加进 sessions.jsonl 的记录携带 node_id/seq/ts_hlc。
func TestEnsureSession_StampsMachineAttribution(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := EnsureSession(dir, ""); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	sessions, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("no sessions recorded")
	}
	var last SessionRecord
	for _, s := range sessions {
		if s.StartedAt.After(last.StartedAt) || last.SessionID == "" {
			last = s
		}
	}
	if !nodeid.ValidNodeID(last.NodeID) {
		t.Fatalf("node_id %q invalid", last.NodeID)
	}
	if last.Seq == 0 {
		t.Fatal("seq zero after appendSessionLog")
	}
	if _, err := hlc.Parse(last.TsHLC); err != nil {
		t.Fatalf("ts_hlc unparsable: %v", err)
	}
}
