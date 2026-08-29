package taskpipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// withTestIdentity pins FORGE_DATA_HOME to a temp home carrying a node identity, so
// sign/verify round-trips exercise the real key derivation.
//
// withTestIdentity 把 FORGE_DATA_HOME 钉到带 node 身份的临时 home，让签名/验签
// 往返走真实密钥派生。
func withTestIdentity(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	id, err := nodeid.Generate()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	id.NodeID = id.NodeID // keep field access explicit
	if err := id.Save(); err != nil {
		t.Fatalf("save identity: %v", err)
	}
	return home
}

// TestTaskStateIntegrity_SignVerifyRoundTrip pins the happy path: SaveTaskState
// signs, LoadTaskState verifies silently, IntegrityBroken()=false.
//
// TestTaskStateIntegrity_SignVerifyRoundTrip 钉住快乐路径：SaveTaskState 签名、
// LoadTaskState 静默验签通过、IntegrityBroken()=false。
func TestTaskStateIntegrity_SignVerifyRoundTrip(t *testing.T) {
	withTestIdentity(t)
	root := t.TempDir()
	state := NewTaskState(&taskcontext.Context{Source: "explicit", TaskRef: "integ-ok", Branch: "main"})
	if err := SaveTaskState(root, state); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadTaskState(root, "integ-ok")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.IntegrityBroken() {
		t.Fatal("honestly-signed state must not be flagged broken")
	}
	if loaded.Integrity == nil || loaded.Integrity.Sig == "" {
		t.Fatal("save must attach a signature when identity exists")
	}
}

// TestTaskStateIntegrity_TamperedStateFlagged pins the attack path: hand-editing the
// persisted JSON (the 2026-08-29 functional probe — forged ReviewPassed was fully
// trusted) must leave the loaded state flagged IntegrityBroken.
//
// TestTaskStateIntegrity_TamperedStateFlagged 钉住攻击路径：手改落盘 JSON
//（2026-08-29 功能探针——伪造的 ReviewPassed 曾被全量采信）必须让加载后的状态
// 带上 IntegrityBroken 标记。
func TestTaskStateIntegrity_TamperedStateFlagged(t *testing.T) {
	withTestIdentity(t)
	root := t.TempDir()
	state := NewTaskState(&taskcontext.Context{Source: "explicit", TaskRef: "integ-tamper", Branch: "main"})
	if err := SaveTaskState(root, state); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := filepath.Join(forgedata.DataDirFor(root), "tasks", "integ-tamper.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The probe's exact forgery: flip review_passed to true outside forge.
	tampered := strings.Replace(string(raw), `"review_passed":`, `"review_passed": true, "_x":`, 1)
	if tampered == string(raw) {
		// omitempty may omit the field entirely — inject it instead.
		tampered = strings.Replace(string(raw), `"task_ref": "integ-tamper"`, `"task_ref": "integ-tamper", "review_passed": true`, 1)
	}
	if tampered == string(raw) {
		t.Skip("could not construct tampered variant")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTaskState(root, "integ-tamper")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded.IntegrityBroken() {
		t.Fatal("tampered state must be flagged IntegrityBroken")
	}
}

// TestTaskStateIntegrity_LegacyUnsignedAllowed pins compatibility: pre-signing data
// (no integrity block) loads without the broken flag.
//
// TestTaskStateIntegrity_LegacyUnsignedAllowed 钉住兼容性：签名前的存量数据
//（无 integrity 块）加载不置 broken 标记。
func TestTaskStateIntegrity_LegacyUnsignedAllowed(t *testing.T) {
	root := t.TempDir()
	state := NewTaskState(&taskcontext.Context{Source: "explicit", TaskRef: "integ-legacy", Branch: "main"})
	tasksDir := filepath.Join(forgedata.DataDirFor(root), "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Hand-write WITHOUT going through SaveTaskState → no signature block.
	state.Integrity = nil
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, "integ-legacy.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTaskState(root, "integ-legacy")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.IntegrityBroken() {
		t.Fatal("legacy unsigned state must not be flagged broken")
	}
}
