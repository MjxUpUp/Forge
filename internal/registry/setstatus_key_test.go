package registry

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestSetStatus_KeyMatchDoesNotTouchOtherEntries 钉住 SetStatus 的 key 精确匹配：
// 只按 key 命中目标条目，不得顺手把 key 不同的兄弟条目一起翻转。
//
// 变异测试用：把 `keyOf(e) == key` 翻成 `!=` 后，另一个条目会被误改，本用例失败。
func TestSetStatus_KeyMatchDoesNotTouchOtherEntries(t *testing.T) {
	useTempHome(t)
	a := t.TempDir()
	b := t.TempDir()
	gitInit(t, a)
	gitInit(t, b)

	keyA, err := forgedata.Key(a)
	if err != nil {
		t.Fatalf("forgedata.Key(a): %v", err)
	}
	keyB, err := forgedata.Key(b)
	if err != nil {
		t.Fatalf("forgedata.Key(b): %v", err)
	}
	if keyA == keyB {
		t.Fatalf("前置条件不成立：两个目录算出同一个 key（%q）", keyA)
	}
	if err := writeEntries([]Entry{
		{Path: a, Key: keyA},
		{Path: b, Key: keyB},
	}); err != nil {
		t.Fatal(err)
	}

	if err := SetStatus(b, StatusDeclined, `test`); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	_, stateA := State(a)
	if stateA != StatusManaged {
		t.Fatalf("只退出了 b，a 却变成 %q（key 匹配误伤了别的条目）", stateA)
	}
	_, stateB := State(b)
	if stateB != StatusDeclined {
		t.Fatalf("b 应为 declined，得 %q", stateB)
	}
}
