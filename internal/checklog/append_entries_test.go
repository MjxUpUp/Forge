package checklog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestAppendEntries_PreservesTimingAndOrder is the cross-machine-import replay contract: entries
// carried in by a task import must land in the local checklog with their ORIGINAL RecordedAt
// (source-machine timing) intact and in given order, so `forge trace <ref>` reconstructs the real
// timeline. AppendEntries deliberately does NOT rewrite RecordedAt (unlike Record, which stamps now).
//
// TestAppendEntries_PreservesTimingAndOrder 是跨机器 import 回放契约：任务 import 带入的条目必须以
// 原始 RecordedAt（源机器时序）落地进本地 checklog 且保持给定顺序，使 forge trace <ref> 重建真实时间线。
// AppendEntries 刻意不重写 RecordedAt（区别于盖当前时间的 Record）。
func TestAppendEntries_PreservesTimingAndOrder(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	want := []Entry{
		{Check: `compile`, TaskRef: `feat/x`, RecordedAt: time.Unix(1000, 0).UTC(), Detail: `first`},
		{Check: `test`, TaskRef: `feat/x`, RecordedAt: time.Unix(2000, 0).UTC(), Detail: `second`},
	}
	if err := AppendEntries(dir, want); err != nil {
		t.Fatalf(`AppendEntries: %v`, err)
	}
	got, err := LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	if len(got) != 2 {
		t.Fatalf(`got %d entries, want 2`, len(got))
	}
	for i, e := range got {
		if e.Check != want[i].Check {
			t.Errorf(`entry %d Check = %q, want %q`, i, e.Check, want[i].Check)
		}
		if !e.RecordedAt.Equal(want[i].RecordedAt) {
			t.Errorf(`entry %d RecordedAt = %v, want %v（import 须保留源时序）`, i, e.RecordedAt, want[i].RecordedAt)
		}
	}
}

// TestAppendEntries_EmptyIsNoop: a nil/empty slice must not create the checklog file (the import
// path calls AppendEntries unconditionally when bundle.Checklog is non-empty, but a defensively-empty
// slice should be a clean no-op, not an empty-file side effect).
//
// TestAppendEntries_EmptyIsNoop：nil/空切片不得创建 checklog 文件（import 在 bundle.Checklog 非空时
// 无条件调 AppendEntries，但防御性的空切片应是干净 no-op，而非留个空文件的副作用）。
func TestAppendEntries_EmptyIsNoop(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	if err := AppendEntries(dir, nil); err != nil {
		t.Fatalf(`nil AppendEntries: %v`, err)
	}
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), `checklog.jsonl`)); !os.IsNotExist(err) {
		t.Errorf(`空 AppendEntries 不应创建文件: %v`, err)
	}
}
