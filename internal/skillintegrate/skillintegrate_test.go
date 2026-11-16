package skillintegrate

import (
	"strings"
	"testing"
)

// TestLookupKnownAndUnknown.
//
// TestLookupKnownAndUnknown — Lookup 按文件名解析笔记；未知/空名返 false。
// code-review-gate 是迁移首批笔记之一（真实内嵌资产，非合成 fixture）——钉住
// embed 接线本身，防 notes/ 目录改名后 Lookup 静默全空。
func TestLookupKnownAndUnknown(t *testing.T) {
	note, ok := Lookup("code-review-gate")
	if !ok {
		t.Fatal("code-review-gate 应有集成笔记（迁移首批）")
	}
	if !strings.Contains(note, "code-review-gate") {
		t.Errorf("笔记内容应含 skill 名, got:\n%s", note[:80])
	}
	if _, ok := Lookup("no-such-skill"); ok {
		t.Error("未知 skill 不应有笔记")
	}
	if _, ok := Lookup(""); ok {
		t.Error("空名不应有笔记")
	}
}

// TestListNonEmpty — List returns a sorted non-empty roster (the 10 migrated notes).
//
// TestListNonEmpty — List 返回字典序非空清单（10 份迁移笔记）。
func TestListNonEmpty(t *testing.T) {
	names := List()
	if len(names) < 10 {
		t.Fatalf("迁移笔记应 ≥10 份, got %v", names)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("List 应字典序: %v", names)
		}
	}
}
