package hazard

// halt_test.go — safe-halt 判定的表驱动测试：连续计数 / confirm 与 release 重置 /
// 阈值边界 / 空事件流。事件经 AppendEvent 真实落盘（不 mock 文件层）。

import (
	"os"
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

// TestEventsDestroyed pins the "events were destroyed" state detector shared by the
// delivery gate and halt release (single source — two drifting stat checks were the
// alternative): dir exists + events.jsonl missing → true (events were written then
// removed); file present → false; nothing ever written → false.
//
// TestEventsDestroyed 钉"事件流被毁"判定——清账门与 halt release 单源共用（否则
// 两处 stat 判定漂移）：目录在而 events.jsonl 缺失 → true（事件曾落盘后被清除）；
// 文件在 → false；从未写过 → false。
func TestEventsDestroyed(t *testing.T) {
	t.Run("从未写事件", func(t *testing.T) {
		if EventsDestroyed(newHaltProject(t)) {
			t.Fatal("全新项目不应判被毁")
		}
	})
	t.Run("事件在盘", func(t *testing.T) {
		p := newHaltProject(t)
		if err := AppendEvent(p, Event{Type: EventBlock, Command: "x"}); err != nil {
			t.Fatal(err)
		}
		if EventsDestroyed(p) {
			t.Fatal("事件流存在不应判被毁")
		}
	})
	t.Run("目录在文件无", func(t *testing.T) {
		p := newHaltProject(t)
		if err := AppendEvent(p, Event{Type: EventBlock, Command: "x"}); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(p.HazardsEventsPath()); err != nil {
			t.Fatal(err)
		}
		if !EventsDestroyed(p) {
			t.Fatal("目录在而事件文件无应判被毁——清账证据被毁不是清白证明")
		}
	})
}
