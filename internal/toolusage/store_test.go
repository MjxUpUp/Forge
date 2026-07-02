package toolusage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestEstimateTokens：token 估算的 rune/3 启发式（loop 成本代理）。
// 核心契约：空=0、rune/3+1 单调、中英文统一按 rune 计数（中文 1 字也算 1 rune）。
func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 2},     // 4/3+1
		{"abcdefgh", 3}, // 8/3+1
		{"你好世界", 2},  // 4 runes /3+1
	}
	for _, c := range cases {
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

// TestSumEstTokens：累计 token 聚合（trace 显示 loop 成本用）。EstTokens=0 不影响累计。
func TestSumEstTokens(t *testing.T) {
	calls := []ToolCall{
		{EstTokens: 100},
		{EstTokens: 200},
		{EstTokens: 0}, // tool-track 无 input
	}
	if got := SumEstTokens(calls); got != 300 {
		t.Fatalf("SumEstTokens=%d want 300", got)
	}
}

// TestRecord_PersistsTokenFields：InputLen/EstTokens 必须随 ToolCall 持久化到 toollog
// 并能 LoadAll 读回——token 计量全链路（hook 填充 → JSON → trace 读取）的契约。
// 守护 JSON tag 正确（曾发生 session_id→task_id 误改事故，token 字段与 session_id 同级风险）。
func TestRecord_PersistsTokenFields(t *testing.T) {
	dir := t.TempDir()
	call := &ToolCall{
		ToolName:  "Edit",
		ToolInput: `{"file":"x.go"}`,
		InputLen:  14,
		EstTokens: 5,
	}
	if err := Record(dir, call); err != nil {
		t.Fatalf("Record: %v", err)
	}
	loaded, err := LoadAll(dir)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("LoadAll: err=%v len=%d", err, len(loaded))
	}
	if loaded[0].InputLen != 14 || loaded[0].EstTokens != 5 {
		t.Fatalf("token 字段未持久化: InputLen=%d EstTokens=%d（JSON tag/序列化断裂）", loaded[0].InputLen, loaded[0].EstTokens)
	}
	// session_id tag 回归保护：token 字段改动不应碰会话隔离字段（并发安全红线）。
	call2 := &ToolCall{ToolName: "Read", SessionID: "sess-abc"}
	if err := Record(dir, call2); err != nil {
		t.Fatalf("Record call2: %v", err)
	}
	loaded2, _ := LoadAll(dir)
	if len(loaded2) != 2 || loaded2[1].SessionID != "sess-abc" {
		t.Fatalf("session_id tag 应不变（并发隔离红线），got len=%d sess=%q", len(loaded2), loaded2[1].SessionID)
	}
}

func TestRecordAndLoad(t *testing.T) {
	dir := t.TempDir()

	call := &ToolCall{
		ToolName:  "Read",
		ToolInput: `{"file_path": "/tmp/test.go"}`,
		TaskRef:   "test-task",
	}

	if err := Record(dir, call); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	calls, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ToolName != "Read" {
		t.Errorf("expected ToolName=Read, got %s", calls[0].ToolName)
	}
	if calls[0].TaskRef != "test-task" {
		t.Errorf("expected TaskRef=test-task, got %s", calls[0].TaskRef)
	}
}

// TestTokenBreakerWarning：纯判断函数——超阈值返回警示，未超返回空。
// 不依赖文件，可直接覆盖阈值边界（避免造超 50 万 token 的测试数据）。
func TestTokenBreakerWarning(t *testing.T) {
	if w := tokenBreakerWarning(taskTokenWarnThreshold - 1); w != "" {
		t.Errorf("阈值-1 应无警示，got %q", w)
	}
	if w := tokenBreakerWarning(taskTokenWarnThreshold); w == "" {
		t.Error("阈值应触发警示，got 空")
	}
	if w := tokenBreakerWarning(taskTokenWarnThreshold + 1); w == "" {
		t.Error("阈值+1 应触发警示，got 空")
	}
}

// TestTaskTokenBreaker：文件加载 + 聚合接入——写超阈值的 call → warning 非空 + total 正确；
// 未超 → 空。守护 token 计量真正接入成本熔断（防回归到"只观测不 gating"）。
func TestTaskTokenBreaker(t *testing.T) {
	dir := t.TempDir()
	// 一条超大 EstTokens 的 call，累计超阈值。
	if err := Record(dir, &ToolCall{ToolName: "Read", TaskRef: "feat/big", EstTokens: taskTokenWarnThreshold + 100}); err != nil {
		t.Fatal(err)
	}
	w, total := TaskTokenBreaker(dir, "feat/big")
	if w == "" {
		t.Fatal("超阈值应返回警示，got 空")
	}
	if total != taskTokenWarnThreshold+100 {
		t.Errorf("total=%d want %d", total, taskTokenWarnThreshold+100)
	}

	// 未超阈值的 task → 无警示。
	if err := Record(dir, &ToolCall{ToolName: "Edit", TaskRef: "feat/small", EstTokens: 100}); err != nil {
		t.Fatal(err)
	}
	w2, total2 := TaskTokenBreaker(dir, "feat/small")
	if w2 != "" {
		t.Errorf("未超阈值应无警示，got %q", w2)
	}
	if total2 != 100 {
		t.Errorf("total=%d want 100", total2)
	}
}

func TestLoadForTaskAll(t *testing.T) {
	dir := t.TempDir()

	// Active toollog: two task refs.
	if err := Record(dir, &ToolCall{ToolName: "Read", TaskRef: "feat/x"}); err != nil {
		t.Fatal(err)
	}
	if err := Record(dir, &ToolCall{ToolName: "Edit", TaskRef: "feat/y"}); err != nil {
		t.Fatal(err)
	}

	// Archived toollog from a previous task start — feat/x history that the
	// active-only LoadForTask would miss. LoadForTaskAll must include it.
	archivePath := filepath.Join(dir, ".forge", "toollog-20260101000000.jsonl")
	archived := []byte(`{"tool_name":"Bash","task_ref":"feat/x","timestamp":"2026-01-01T00:00:00Z"}` + "\n")
	if err := os.WriteFile(archivePath, archived, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadForTaskAll(dir, "feat/x")
	if err != nil {
		t.Fatalf("LoadForTaskAll: %v", err)
	}
	// 1 active (Read) + 1 archived (Bash).
	if len(got) != 2 {
		t.Fatalf("expected 2 calls for feat/x, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.TaskRef != "feat/x" {
			t.Errorf("call TaskRef = %q, want feat/x", c.TaskRef)
		}
	}
}

func TestLoadForTask(t *testing.T) {
	dir := t.TempDir()

	records := []ToolCall{
		{ToolName: "Read", TaskRef: "task-a"},
		{ToolName: "Edit", TaskRef: "task-b"},
		{ToolName: "Bash", TaskRef: "task-a"},
	}

	for _, r := range records {
		if err := Record(dir, &r); err != nil {
			t.Fatalf("Record failed: %v", err)
		}
	}

	calls, err := LoadForTask(dir, "task-a")
	if err != nil {
		t.Fatalf("LoadForTask failed: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls for task-a, got %d", len(calls))
	}
	for _, c := range calls {
		if c.TaskRef != "task-a" {
			t.Errorf("expected TaskRef=task-a, got %s", c.TaskRef)
		}
	}
}

func TestTruncateInput(t *testing.T) {
	// ASCII
	short := "hello"
	if TruncateInput(short) != short {
		t.Error("short string should not be truncated")
	}

	// Long ASCII
	long := ""
	for range 600 {
		long += "x"
	}
	truncated := TruncateInput(long)
	if len([]rune(truncated)) != maxToolInputLen {
		t.Errorf("expected %d runes, got %d", maxToolInputLen, len([]rune(truncated)))
	}

	// Chinese (rune-safe)
	chinese := ""
	for range 300 {
		chinese += "中文"
	}
	truncatedChinese := TruncateInput(chinese)
	if len([]rune(truncatedChinese)) != maxToolInputLen {
		t.Errorf("expected %d runes for Chinese, got %d", maxToolInputLen, len([]rune(truncatedChinese)))
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()

	Record(dir, &ToolCall{ToolName: "Read"})
	Record(dir, &ToolCall{ToolName: "Edit"})

	calls, _ := LoadAll(dir)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls before clear, got %d", len(calls))
	}

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	calls, _ = LoadAll(dir)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls after clear, got %d", len(calls))
	}

	// Archived file should exist
	files, _ := filepath.Glob(filepath.Join(dir, ".forge", "toollog-*.jsonl"))
	if len(files) == 0 {
		// Archive might be in the temp dir that got cleaned, check .forge dir
		forgeDir := filepath.Join(dir, ".forge")
		entries, err := os.ReadDir(forgeDir)
		if err == nil {
			found := false
			for _, e := range entries {
				if len(e.Name()) > 12 && e.Name()[:12] == "toollog-20" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected archived toollog file after Clear")
			}
		}
	}
}

func TestLoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	calls, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll on nonexistent dir should not error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

// TestRecordAndClear_ConcurrentNoDeadlock guards the C2 fix: Clear and Archive
// now hold the same mutex as Record. The lock split previously let a rotation
// race a concurrent append (entries lost). The fix introduces archiveLocked so
// Clear can archive-then-remove under one lock WITHOUT re-entering the mutex
// (which would deadlock, since sync.Mutex is non-reentrant). This test fails
// fast on that re-entry deadlock via the timeout, and under -race catches any
// remaining unsynchronized file access.
func TestRecordAndClear_ConcurrentNoDeadlock(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 5 {
				_ = Record(dir, &ToolCall{ToolName: "Read", TaskRef: "t"})
			}
		}()
		go func() {
			defer wg.Done()
			_ = Clear(dir)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record/Clear deadlocked (Clear→Archive mutex re-entry?)")
	}
	if _, err := LoadAll(dir); err != nil {
		t.Fatalf("LoadAll after concurrent Record/Clear: %v", err)
	}
}
