package hazard

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// rewindLastEvent 把 events.jsonl 末行的 Ts 平移 delta（负值=推向过去），用于把已落盘
// 事件挪出去重窗口而不 sleep。
func rewindLastEvent(t *testing.T, p *forgedata.Project, delta time.Duration) {
	t.Helper()
	events, err := LoadEvents(p)
	if err != nil || len(events) == 0 {
		t.Fatalf("rewindLastEvent: load events: %v (n=%d)", err, len(events))
	}
	events[len(events)-1].Ts = events[len(events)-1].Ts.Add(delta)
	f, err := os.Create(p.HazardsEventsPath())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, e := range events {
		data, _ := json.Marshal(e)
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// 宿主双投递去重（docs/design/harness-fixes-a-g-2026-09.md F.3）：同一 Bash 调用被宿主
// 双发 PreToolUse 时，hook 脚本会两次调 `forge hazard log block`——乙机实录 52 条 block
// 里 24 条是同指纹短窗重复，把 safe-halt 计数直接翻倍（3 次逻辑拦截被记成 6 次越过阈值）。
// 去重键 (Type, Fingerprint)，窗口与 hookdispatch 的 blockRecordDedupWindow 同量级。

func TestAppendEvent_DedupesSameFingerprintWithinWindow(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	e := Event{Type: EventBlock, Fingerprint: Fingerprint("git push origin --force"), Command: "git push origin --force"}
	if err := AppendEvent(root, e); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvent(root, e); err != nil {
		t.Fatal(err)
	}
	events, err := LoadEvents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("duplicate block within window must collapse to 1 row, got %d", len(events))
	}
	// 不同 type 同指纹不去重（block → data 是语义变化）
	if err := AppendEvent(root, Event{Type: EventData, Fingerprint: e.Fingerprint, Command: e.Command}); err != nil {
		t.Fatal(err)
	}
	events, _ = LoadEvents(root)
	if len(events) != 2 {
		t.Fatalf("different type must not dedupe, got %d rows", len(events))
	}
}

func TestAppendEvent_EmptyFingerprintNeverDedupes(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	for i := 0; i < 3; i++ {
		if err := AppendEvent(root, Event{Type: EventBlock, Command: "opaque"}); err != nil {
			t.Fatal(err)
		}
	}
	events, _ := LoadEvents(root)
	if len(events) != 3 {
		t.Fatalf("events without fingerprint must all be kept, got %d", len(events))
	}
}

func TestAppendEvent_DedupeWindowExpires(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	fp := Fingerprint("rm -rf ./build")
	old := Event{Type: EventBlock, Fingerprint: fp, Command: "rm -rf ./build"}
	if err := AppendEvent(root, old); err != nil {
		t.Fatal(err)
	}
	// 把已落盘行的时间戳推回窗口之外，再追加同指纹：必须视为新事件。
	rewindLastEvent(t, root, -(EventDedupWindow + time.Second))
	if err := AppendEvent(root, old); err != nil {
		t.Fatal(err)
	}
	events, _ := LoadEvents(root)
	if len(events) != 2 {
		t.Fatalf("same fingerprint outside window is a genuine retry, want 2 rows got %d", len(events))
	}
}

func TestCheckHalt_CountsDedupedBlocksOnce(t *testing.T) {
	p := forgedatatest.ForDataDir(t.TempDir())
	// 3 条逻辑拦截各被双投递：未去重会记 6 次；去重后 3 次恰到阈值——语义与 halt_test
	// 的单投递路径一致。
	for i, cmd := range []string{"rm -rf a", "rm -rf b", "rm -rf c"} {
		e := Event{Type: EventBlock, Fingerprint: Fingerprint(cmd), Command: cmd}
		if err := AppendEvent(p, e); err != nil {
			t.Fatal(err)
		}
		if err := AppendEvent(p, e); err != nil {
			t.Fatal(err)
		}
		st := CheckHalt(p)
		if st.Blocks != i+1 {
			t.Fatalf("after %d logical blocks (double-delivered) Blocks = %d, want %d", i+1, st.Blocks, i+1)
		}
	}
	if st := CheckHalt(p); !st.Halted {
		t.Fatal("3 logical blocks must still trip safe-halt")
	}
}
