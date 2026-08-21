package act

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/hlc"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

// TestAppend_StampsMachineAttribution pins the event-stamping contract at the
// conclusions choke point (node-identity.md §4): Append lands node_id/seq/ts_hlc,
// and a caller-preset stamp (import/merge path) survives untouched.
//
// TestAppend_StampsMachineAttribution 在 conclusions 收口点钉死事件打戳契约
// （node-identity.md §4）：Append 落 node_id/seq/ts_hlc，调用方预置戳
// （import/merge 路径）原样保留。
func TestAppend_StampsMachineAttribution(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := forgedatatest.ForDataDir(t.TempDir())

	c := Conclusion{TaskRef: "feat/a", CompletedAt: time.Now()}
	if err := Append(root, &c); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d conclusions, want 1", len(got))
	}
	g := got[0]
	if !nodeid.ValidNodeID(g.NodeID) {
		t.Fatalf("node_id %q invalid", g.NodeID)
	}
	if g.Seq == 0 {
		t.Fatal("seq zero after Append")
	}
	if _, err := hlc.Parse(g.TsHLC); err != nil {
		t.Fatalf("ts_hlc unparsable: %v", err)
	}

	preset := nodestamp.Stamp{NodeID: "fnode_cccccccccccccccccccccccccccccccc", Seq: 900, TsHLC: hlc.Timestamp{Wall: 5}.String()}
	c2 := Conclusion{TaskRef: "feat/b", CompletedAt: time.Now(), Stamp: preset}
	if err := Append(root, &c2); err != nil {
		t.Fatalf("Append preset: %v", err)
	}
	got2, err := LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got2) != 2 || got2[1].Stamp != preset {
		t.Fatalf("preset stamp not preserved: %+v", got2)
	}
}
