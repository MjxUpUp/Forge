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

// Host double-delivery dedupe (docs/design/harness-fixes-a-g-2026-09.md F.3): when a host fires
// PreToolUse twice for one Bash call, the hook script calls `forge hazard log block` twice —
// on machine 乙, 24 of 52 block rows were same-fingerprint short-window repeats, doubling the
// safe-halt counter (3 logical blocks recorded as 6, tripping the threshold of 3). Key is
// (Session, Type, Fingerprint); the window matches hookdispatch's blockRecordDedupWindow.
//
// 宿主双投递去重（docs/design/harness-fixes-a-g-2026-09.md F.3）：同一 Bash 调用被宿主
// 双发 PreToolUse 时，hook 脚本会两次调 `forge hazard log block`——乙机实录 52 条 block
// 里 24 条是同指纹短窗重复，把 safe-halt 计数直接翻倍（3 次逻辑拦截被记成 6 次越过阈值）。
// 去重键 (Session, Type, Fingerprint)，窗口与 hookdispatch 的 blockRecordDedupWindow 同量级。

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

// TestAppendEvent_DifferentSessionsNotDeduped pins the session dimension of the dedupe key
// (aligned with hookdispatch blockRecordMarker): two parallel sessions blocked on the identical
// command within the window are two incidents; a legacy row without a session still dedupes
// against a same-fingerprint follow-up.
//
// TestAppendEvent_DifferentSessionsNotDeduped 钉住去重键的会话维度（与 hookdispatch
// blockRecordMarker 对齐）：两个并行会话 3s 内各拦一次同命令是两起事件；无会话的旧行
// 与随后同指纹行仍按双投递合并。
func TestAppendEvent_DifferentSessionsNotDeduped(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	fp := Fingerprint("git reset --hard origin/main")
	for _, sid := range []string{"sess-a", "sess-b"} {
		if err := AppendEvent(root, Event{Type: EventBlock, Fingerprint: fp, Command: "c", SessionID: sid}); err != nil {
			t.Fatal(err)
		}
	}
	events, _ := LoadEvents(root)
	if len(events) != 2 {
		t.Fatalf("two sessions blocked on the same command must yield 2 rows, got %d", len(events))
	}
	// 会话任一侧为空：退化为只比 type+指纹
	if err := AppendEvent(root, Event{Type: EventBlock, Fingerprint: fp, Command: "c"}); err != nil {
		t.Fatal(err)
	}
	events, _ = LoadEvents(root)
	if len(events) != 2 {
		t.Fatalf("session-less follow-up of sess-b's block must dedupe, got %d rows", len(events))
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

// TestIsDoubleDelivery_Table pins the exported double-delivery predicate (shared with
// harness-audit's F2 metric): type+fingerprint must match, session only when both carry one,
// window half-open [0,3s), clock skew never dedupes.
//
// TestIsDoubleDelivery_Table 钉住导出的双投递谓词（与 harness-audit 的 F2 度量共用）：
// type+指纹须同、会话仅双方都带才比、窗口半开 [0,3s)、时钟回拨不去重。
func TestIsDoubleDelivery_Table(t *testing.T) {
	t0 := time.Date(2026, 9, 7, 22, 50, 0, 0, time.UTC)
	fp := Fingerprint("git reset --hard origin/main")
	base := Event{Ts: t0, Type: EventBlock, Fingerprint: fp, SessionID: "s1"}
	cases := []struct {
		name string
		next Event
		want bool
	}{
		{"same within window", Event{Ts: t0.Add(2 * time.Second), Type: EventBlock, Fingerprint: fp, SessionID: "s1"}, true},
		{"boundary 3s excluded", Event{Ts: t0.Add(3 * time.Second), Type: EventBlock, Fingerprint: fp, SessionID: "s1"}, false},
		{"different type", Event{Ts: t0.Add(time.Second), Type: EventData, Fingerprint: fp, SessionID: "s1"}, false},
		{"different fingerprint", Event{Ts: t0.Add(time.Second), Type: EventBlock, Fingerprint: Fingerprint("other"), SessionID: "s1"}, false},
		{"different session", Event{Ts: t0.Add(time.Second), Type: EventBlock, Fingerprint: fp, SessionID: "s2"}, false},
		{"one side sessionless dedupes", Event{Ts: t0.Add(time.Second), Type: EventBlock, Fingerprint: fp}, true},
		{"clock skew never dedupes", Event{Ts: t0.Add(-time.Second), Type: EventBlock, Fingerprint: fp, SessionID: "s1"}, false},
	}
	for _, c := range cases {
		if got := IsDoubleDelivery(base, c.next); got != c.want {
			t.Errorf("%s: IsDoubleDelivery = %v, want %v", c.name, got, c.want)
		}
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
