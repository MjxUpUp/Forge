package hazard

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAppendEvent_LoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	e := Event{
		Type:        EventBlock,
		Fingerprint: Fingerprint("rm -rf ./vault"),
		Command:     "rm -rf ./vault",
	}
	if err := AppendEvent(root, e); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	events, err := LoadEvents(root)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.Type != EventBlock {
		t.Errorf("Type: got %q want %q", got.Type, EventBlock)
	}
	if got.Fingerprint != e.Fingerprint {
		t.Errorf("Fingerprint mismatch: got %s want %s", got.Fingerprint, e.Fingerprint)
	}
	if got.Command != e.Command {
		t.Errorf("Command: got %q want %q", got.Command, e.Command)
	}
	// Ts 由 AppendEvent 盖戳，必须在近过去（非零）
	if got.Ts.IsZero() {
		t.Fatal("Ts must be stamped by AppendEvent")
	}
	if time.Since(got.Ts) > 5*time.Second {
		t.Fatalf("Ts should be ~now, got %v old", time.Since(got.Ts))
	}
}

func TestAppendEvent_TruncatesLongCommand(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("a", maxCommandStore*3) // 远超截断阈值
	if err := AppendEvent(root, Event{Type: EventBlock, Command: long}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	events, _ := LoadEvents(root)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	// 截断后 = s[:maxCommandStore] + "…"（… 是 3 字节 UTF-8），故 len = maxCommandStore+3
	if !strings.HasSuffix(events[0].Command, "…") {
		t.Errorf("truncated command must end with ellipsis, got len=%d: %q", len(events[0].Command), events[0].Command)
	}
	if want := maxCommandStore + 3; len(events[0].Command) != want {
		t.Errorf("truncated command length: got %d, want %d (maxCommandStore + 3-byte ellipsis)", len(events[0].Command), want)
	}
}

// TestAppendEvent_TruncatesMultiByte 验证 truncate 按 rune 而非字节切片——中文命令
// （每字 3 字节）截断后必须仍是有效 UTF-8，不能在字符中间切断（字节切片会产生无效
// UTF-8，json.Marshal 替换为 U+FFFD，审计日志乱码）。
func TestAppendEvent_TruncatesMultiByte(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("删", maxCommandStore*2) // 远超 maxCommandStore 字符
	if err := AppendEvent(root, Event{Type: EventBlock, Command: long}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	events, _ := LoadEvents(root)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	cmd := events[0].Command
	if !strings.HasSuffix(cmd, "…") {
		t.Fatalf("truncated command must end with ellipsis, got %q", cmd)
	}
	if !utf8.ValidString(cmd) {
		t.Fatalf("truncated command must be valid UTF-8 (rune-based truncation), got invalid bytes")
	}
}

func TestAppendEvent_AppendsInOrder(t *testing.T) {
	root := t.TempDir()
	for _, typ := range []string{EventBlock, EventRelease, EventData} {
		if err := AppendEvent(root, Event{Type: typ, Command: "cmd-" + typ}); err != nil {
			t.Fatalf("AppendEvent %s: %v", typ, err)
		}
	}
	events, _ := LoadEvents(root)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// 追加序 = 文件内序（O_APPEND 保证）
	if events[0].Type != EventBlock || events[1].Type != EventRelease || events[2].Type != EventData {
		t.Fatalf("order mismatch: %+v", events)
	}
}

func TestCountSince(t *testing.T) {
	root := t.TempDir()
	AppendEvent(root, Event{Type: EventBlock, Command: "a"})
	AppendEvent(root, Event{Type: EventBlock, Command: "b"})
	AppendEvent(root, Event{Type: EventRelease, Command: "c"})

	// since=0 → 全部 block 计入
	n, err := CountSince(root, EventBlock, time.Time{})
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 block events since zero, got %d", n)
	}
	// since=未来 → 0
	n, _ = CountSince(root, EventBlock, time.Now().Add(time.Hour))
	if n != 0 {
		t.Fatalf("expected 0 events since future, got %d", n)
	}
}

func TestLoadEvents_MissingFile(t *testing.T) {
	root := t.TempDir()
	events, err := LoadEvents(root)
	if err != nil {
		t.Fatalf("missing events file should not error: %v", err)
	}
	if events != nil {
		t.Fatalf("expected nil for missing file, got %v", events)
	}
}

func TestLoadEvents_SkipsCorruptLine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".forge", "hazards")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 第 1、3 行合法，第 2 行损坏 → LoadEvents 返回 2 条，不报错
	content := `{"type":"block","command":"ok1"}
{not json}
{"type":"release","command":"ok2"}
`
	if err := os.WriteFile(eventsPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := LoadEvents(root)
	if err != nil {
		t.Fatalf("corrupt line should not error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 valid events (corrupt skipped), got %d", len(events))
	}
}

// TestAppendEvent_ConcurrentSafe 并发追加不应交错/丢失（eventMu + O_APPEND）。
// 这是 hook 在多 session 并发时的正确性保障。
func TestAppendEvent_ConcurrentSafe(t *testing.T) {
	root := t.TempDir()
	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			AppendEvent(root, Event{Type: EventBlock, Command: "concurrent"})
		}()
	}
	wg.Wait()
	events, err := LoadEvents(root)
	if err != nil {
		t.Fatalf("LoadEvents after concurrent writes: %v", err)
	}
	if len(events) != N {
		t.Fatalf("concurrent append lost events: expected %d, got %d", N, len(events))
	}
}
