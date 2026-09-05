package hazard

// halt_test.go — safe-halt 判定的表驱动测试：连续计数 / confirm 与 release 重置 /
// 阈值边界 / 空事件流。事件经 AppendEvent 真实落盘（不 mock 文件层）。

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

func newHaltProject(t *testing.T) *forgedata.Project {
	t.Helper()
	dir := t.TempDir()
	p, err := forgedata.ProjectFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheckHalt_Threshold(t *testing.T) {
	p := newHaltProject(t)
	// 0-2 次：未停机
	for i := 0; i < HaltThreshold-1; i++ {
		if err := AppendEvent(p, Event{Type: EventBlock, Command: "rm -rf x"}); err != nil {
			t.Fatal(err)
		}
	}
	if st := CheckHalt(p); st.Halted || st.Blocks != HaltThreshold-1 {
		t.Fatalf("阈值下不应停机: %+v", st)
	}
	// 第 3 次：停机
	if err := AppendEvent(p, Event{Type: EventBlock, Command: "rm -rf y"}); err != nil {
		t.Fatal(err)
	}
	if st := CheckHalt(p); !st.Halted || st.Blocks != HaltThreshold {
		t.Fatalf("达阈值应停机: %+v", st)
	}
}

func TestCheckHalt_ConfirmResets(t *testing.T) {
	p := newHaltProject(t)
	for i := 0; i < HaltThreshold; i++ {
		AppendEvent(p, Event{Type: EventBlock, Command: "dangerous"})
	}
	// confirm 是重置点：确认过的高危命令不是盲试
	if err := AppendEvent(p, Event{Type: EventConfirm, Command: "dangerous"}); err != nil {
		t.Fatal(err)
	}
	if st := CheckHalt(p); st.Halted || st.Blocks != 0 {
		t.Fatalf("confirm 后应重置: %+v", st)
	}
}

func TestReleaseHalt(t *testing.T) {
	p := newHaltProject(t)
	for i := 0; i < HaltThreshold+2; i++ {
		AppendEvent(p, Event{Type: EventBlock, Command: "dangerous"})
	}
	if st := CheckHalt(p); !st.Halted {
		t.Fatal("前置：应停机")
	}
	if err := ReleaseHalt(p); err != nil {
		t.Fatal(err)
	}
	st := CheckHalt(p)
	if st.Halted || st.Blocks != 0 {
		t.Fatalf("release 后应停机解除且计数归零: %+v", st)
	}
	// release 后再拦一次：未达阈值（计 1，不是历史累计 6）
	AppendEvent(p, Event{Type: EventBlock, Command: "again"})
	if st := CheckHalt(p); st.Halted || st.Blocks != 1 {
		t.Fatalf("release 后计数应从零起算: %+v", st)
	}
}

func TestCheckHalt_EmptyEvents(t *testing.T) {
	p := newHaltProject(t)
	if st := CheckHalt(p); st.Halted || st.Blocks != 0 {
		t.Fatalf("空事件流应未停机: %+v", st)
	}
}
