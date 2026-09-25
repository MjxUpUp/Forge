package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// TestNodeShowCmd_Roundtrip verifies `forge node show --json` emits the persisted identity WITHOUT the private key (display surface must never print secret material) and that repeated runs keep one stable node_id.
//
// TestNodeShowCmd_Roundtrip 验证 `forge node show --json` 输出持久化身份且不含私钥
// （展示面永不打印密钥材料），重复运行保持同一 node_id。
func TestNodeShowCmd_Roundtrip(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())

	var buf bytes.Buffer
	nodeShowCmd.SetOut(&buf)
	t.Cleanup(func() { nodeShowCmd.SetOut(nil) })
	nodeJSON = true
	t.Cleanup(func() { nodeJSON = false })
	if err := nodeShowCmd.RunE(nodeShowCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	out := buf.String()
	var parsed struct {
		NodeID        string `json:"node_id"`
		PublicKey     string `json:"public_key"`
		RotationChain []any  `json:"rotation_chain"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output not json: %v\n%s", err, out)
	}
	if !nodeid.ValidNodeID(parsed.NodeID) {
		t.Fatalf("node_id %q invalid", parsed.NodeID)
	}
	if parsed.PublicKey == "" {
		t.Fatal("public_key missing from output")
	}
	if strings.Contains(out, `private_key`) {
		t.Fatal("node show output contains private_key — display surface leaked secret material")
	}
	// Value-level check: the label check above passes even if the key VALUE leaks under
	// another field name.
	id, err := nodeid.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Contains(out, id.PrivateKey) {
		t.Fatal("node show output contains the private key VALUE under another field")
	}
	if parsed.RotationChain == nil {
		t.Fatal("rotation_chain must serialize as [] (reserved format)")
	}

	// persisted identity matches the printed one (id already loaded above).
	if id.NodeID != parsed.NodeID || id.PublicKey != parsed.PublicKey {
		t.Fatalf("printed (%q/%q) != persisted (%q/%q)", parsed.NodeID, parsed.PublicKey, id.NodeID, id.PublicKey)
	}
}
