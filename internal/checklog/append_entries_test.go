package checklog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

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

// TestAppendEntries_FillsEmptySourceLevel：legacy/手工构造的 import 条目从未设过 Source/Level 时，必须由
// 与 Record 同款的兜底推断填上——否则与本地 Record 的 compile-pass 分桶不一致（如空 Source 使其被按来源
// 过滤的证据链查询漏掉）。调用方设过的值恒优先（此处一并验证）。
func TestAppendEntries_FillsEmptySourceLevel(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	entries := []Entry{
		{Check: `compile`, TaskRef: `feat/x`, Passed: true, RecordedAt: time.Unix(1000, 0).UTC()},                                               // Source/Level 空 → 兜底填
		{Check: `compile`, TaskRef: `feat/y`, Passed: true, RecordedAt: time.Unix(2000, 0).UTC(), Source: EvidenceAgentClaim, Level: LevelPass}, // 已设 → 保留
	}
	if err := AppendEntries(dir, entries); err != nil {
		t.Fatalf(`AppendEntries: %v`, err)
	}
	got, err := LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	if len(got) != 2 {
		t.Fatalf(`got %d entries, want 2`, len(got))
	}
	if got[0].Source != EvidenceDeterministic {
		t.Errorf(`空 Source 应兜底为 deterministic, got %q`, got[0].Source)
	}
	if got[0].Level != LevelPass {
		t.Errorf(`空 Level（Passed=true）应兜底为 pass, got %q`, got[0].Level)
	}
	if got[1].Source != EvidenceAgentClaim || got[1].Level != LevelPass {
		t.Errorf(`调用方已设值应保留, got Source=%q Level=%q`, got[1].Source, got[1].Level)
	}
}
