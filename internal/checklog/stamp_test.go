package checklog

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hlc"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

// lastLines 返回测试隔离 checklog 的非空行。
func lastLines(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(filePath(root))
	if err != nil {
		t.Fatalf("read checklog: %v", err)
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestRecord_StampsMachineAttribution 在 checklog 收口点钉死事件打戳契约
// （node-identity.md §4）：每次 Record 落 node_id/seq/ts_hlc，seq 跨条目单调，
// 调用方预置戳（import/merge 路径）原样保留。
func TestRecord_StampsMachineAttribution(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	if err := Record(root, &Entry{Check: CheckTaskGuard, Passed: true, Detail: "d1"}); err != nil {
		t.Fatalf("Record 1: %v", err)
	}
	if err := Record(root, &Entry{Check: CheckTaskGuard, Passed: true, Detail: "d2"}); err != nil {
		t.Fatalf("Record 2: %v", err)
	}
	// caller-preset stamp (import/merge) must survive Record untouched.
	preset := nodestamp.Stamp{
		NodeID: "fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Seq:    777,
		TsHLC:  hlc.Timestamp{Wall: 1, Logical: 0}.String(),
	}
	if err := Record(root, &Entry{Check: CheckTaskGuard, Passed: true, Detail: "d3", Stamp: preset}); err != nil {
		t.Fatalf("Record 3: %v", err)
	}

	lines := lastLines(t, root)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	var e1, e2, e3 Entry
	for i, dst := range []*Entry{&e1, &e2, &e3} {
		if err := json.Unmarshal([]byte(lines[i]), dst); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
	}
	if !nodeid.ValidNodeID(e1.NodeID) {
		t.Fatalf("e1 node_id %q invalid", e1.NodeID)
	}
	if e1.Seq == 0 || e2.Seq <= e1.Seq {
		t.Fatalf("seq not monotonic: %d then %d", e1.Seq, e2.Seq)
	}
	if _, err := hlc.Parse(e1.TsHLC); err != nil {
		t.Fatalf("e1 ts_hlc unparsable: %v", err)
	}
	if e3.Stamp != preset {
		t.Fatalf("preset stamp overwritten: got %+v want %+v", e3.Stamp, preset)
	}
}
