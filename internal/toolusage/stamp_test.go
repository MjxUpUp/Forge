package toolusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

// TestRecord_StampExcludedFromID pins two invariants at the toollog choke point:
//  1. every Record lands node_id/seq/ts_hlc (event-stamping contract);
//  2. the stored ID equals computeID over the identity fields only (task + session +
//     timestamp + tool name) — stamp values must NEVER enter the hash, otherwise the
//     trace [#id] anchor drifts per stamp. (computeID hashes named fields, not the
//     struct, so this pins the field list against silent drift.)
//
// TestRecord_StampExcludedFromID 在 toollog 收口点钉死两条不变量：
//  1. 每次 Record 落 node_id/seq/ts_hlc（事件打戳契约）；
//  2. 落盘 ID 等于对身份字段（task + session + timestamp + tool 名）的 computeID——
//     戳值绝不进 hash，否则 trace 的 [#id] 锚随戳漂移。（computeID hash 的是具名字段
//     而非整个 struct，本测试把字段清单钉死防静默漂移。）
func TestRecord_StampExcludedFromID(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	mk := func() *ToolCall { return &ToolCall{ToolName: "Read", ToolInput: "x.go", TaskRef: "feat/t"} }
	if err := Record(root, mk()); err != nil {
		t.Fatalf("Record 1: %v", err)
	}
	if err := Record(root, mk()); err != nil {
		t.Fatalf("Record 2: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir(root), toollogFile))
	if err != nil {
		t.Fatalf("read toollog: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	var c1, c2 ToolCall
	if err := json.Unmarshal([]byte(lines[0]), &c1); err != nil {
		t.Fatalf("line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &c2); err != nil {
		t.Fatalf("line 2: %v", err)
	}
	if c1.NodeID == "" || c1.Seq == 0 || c1.TsHLC == "" {
		t.Fatalf("c1 missing stamp: %+v", c1.Stamp)
	}
	// ID derives from identity fields only — recomputing from the decoded entry (whose
	// Stamp is populated) must reproduce the stored ID exactly.
	//
	// ID 只由身份字段派生——对解码出的条目（Stamp 已填）重算必须精确复现落盘 ID。
	if want := computeID(c1); c1.ID != want {
		t.Fatalf("stored ID %q != computeID(entry) %q — stamp leaked into hash", c1.ID, want)
	}
	if c2.Seq <= c1.Seq {
		t.Fatalf("seq not monotonic: %d then %d", c1.Seq, c2.Seq)
	}

	// caller-preset stamp (import/merge) preserved.
	preset := nodestamp.Stamp{NodeID: "fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Seq: 42}
	c := mk()
	c.Stamp = preset
	if err := Record(root, c); err != nil {
		t.Fatalf("Record 3: %v", err)
	}
	if c.Stamp != preset {
		t.Fatalf("preset stamp overwritten: %+v", c.Stamp)
	}
}
