package toolusage

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

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

// TestTokenBreakerWarning：纯判断函数——超阈值返回警示，未超返回空。
// 不依赖文件，可直接覆盖阈值边界（避免造超 50 万 token 的测试数据）。
func TestTokenBreakerWarning(t *testing.T) {
	if w := tokenBreakerWarning(DefaultTaskTokenBudget-1, 0); w != "" {
		t.Errorf("阈值-1 应无警示，got %q", w)
	}
	if w := tokenBreakerWarning(DefaultTaskTokenBudget, 0); w == "" {
		t.Error("阈值应触发警示，got 空")
	}
	if w := tokenBreakerWarning(DefaultTaskTokenBudget+1, 0); w == "" {
		t.Error("阈值+1 应触发警示，got 空")
	}
	// 项目级预算（loop 契约 B1-3）：显式 budget 取代默认阈值；≤0 回落默认。
	if w := tokenBreakerWarning(1000, 1000); w == "" {
		t.Error("达到显式预算 1000 应触发警示，got 空")
	}
	if w := tokenBreakerWarning(999, 1000); w != "" {
		t.Errorf("未达显式预算不应警示，got %q", w)
	}
	if w := tokenBreakerWarning(1000, -1); w != "" {
		t.Errorf("budget<0 应回落默认阈值（1000 远未达），got %q", w)
	}
}

// TestPrune_ReplacesDeletedClear：Clear（归档 + 删 active）已作为死代码删除——
// task start 不再截断 toollog（multi-task-concurrency §5）。本测试钉住原先经
// TestClear 覆盖的存活关切：Prune 不动 active toollog（历史跨任务边界存活）。
func TestPrune_ReplacesDeletedClear(t *testing.T) {
	dir := t.TempDir()

	Record(dir, &ToolCall{ToolName: "Read"})
	Record(dir, &ToolCall{ToolName: "Edit"})

	Prune(dir)

	// active toollog 不动：读取按 TaskRef 过滤，历史必须存活。
	calls, _ := LoadAll(dir)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls after prune (active toollog untouched), got %d", len(calls))
	}
}

// TestPrune_PrunesOldArchives：Prune 按 FORGE_LOG_RETENTION_DAYS 清超期 toollog
// 归档，保留近期归档（原 TestClear_PrunesOldArchives；Clear 删除后由 Prune 承接
// 该 retention 钉子）。
func TestPrune_PrunesOldArchives(t *testing.T) {
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "30")
	dir := t.TempDir()
	forgeDir := forgedata.DataDirFor(dir)
	os.MkdirAll(forgeDir, 0755)
	os.WriteFile(filepath.Join(forgeDir, "toollog.jsonl"), []byte("{}\n"), 0644)
	os.WriteFile(filepath.Join(forgeDir, "toollog-20000101000000.jsonl"), []byte("old"), 0644)
	today := time.Now().Format("20060102150405.000000000")
	os.WriteFile(filepath.Join(forgeDir, "toollog-"+today+".jsonl"), []byte("new"), 0644)

	Prune(dir)
	if _, err := os.Stat(filepath.Join(forgeDir, "toollog-20000101000000.jsonl")); !os.IsNotExist(err) {
		t.Error("old toollog archive should be pruned")
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "toollog-"+today+".jsonl")); err != nil {
		t.Error("recent toollog archive should be kept")
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

// TestRecordAndPrune_ConcurrentNoDeadlock 守卫 C2 修复谱系：改动 toollog 的路径
// 与 Record 持同一把 mutex（旧 Clear 及其 archiveLocked 拆分已于 2026-08-29 作
// 死代码删除；幸存的持锁写方除 Record 外即 Prune）。并发 Record + Prune 既不能
// 死锁、也不能把 active toollog 留成不可读。
//
// 判据是「无进展」而非「总耗时」：旧版 30s 总时长上限在 Windows FS + -race 与
// 本地重负载（多包并行）下误报过死锁——250 次 Record 的 IO 风暴合法地慢，但每次
// 完成都推进计数。死锁 = 计数在整整一个 stallWindow 内纹丝不动，必然被拦；
// 慢而在走 = 计数持续增长，不再误报。
//
// The verdict is "no progress", not "total time": the former 30s overall cap
// misfired as a deadlock under Windows FS + -race and under local heavy load —
// the 250-Record IO storm is legitimately slow but every completion advances the
// counter. A deadlock freezes the counter for a whole stallWindow and is still
// caught; slow-but-moving no longer is.
func TestRecordAndPrune_ConcurrentNoDeadlock(t *testing.T) {
	const stallWindow = 30 * time.Second
	dir := t.TempDir()
	var wg sync.WaitGroup
	var progress atomic.Int64
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 5 {
				_ = Record(dir, &ToolCall{ToolName: "Read", TaskRef: "t"})
				progress.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			Prune(dir)
			progress.Add(1)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	ticker := time.NewTicker(stallWindow)
	defer ticker.Stop()
	last := int64(-1)
wait:
	for {
		select {
		case <-done:
			break wait
		case <-ticker.C:
			now := progress.Load()
			if now == last {
				t.Fatalf("Record/Prune deadlocked (mutex re-entry?): no operation completed in %v (%d/300 done)", stallWindow, now)
			}
			last = now
		}
	}
	if got := progress.Load(); got != 300 {
		t.Fatalf("all 250 Record + 50 Prune must complete, got %d", got)
	}
	if _, err := LoadAll(dir); err != nil {
		t.Fatalf("LoadAll after concurrent Record/Prune: %v", err)
	}
}

// TestToollogHasData 钉住基于 stat 的遥测探测：toollog.jsonl 缺失时 false
// （loadFromPath 在此返回 nil,nil，与空文件不可区分）、文件存在但零字节时 false、
// 记入任一条目后 true。work-activity 门禁靠它区分「hook 分发未接」与「分发正常但零调用」。
func TestToollogHasData(t *testing.T) {
	dir := t.TempDir()
	if ToollogHasData(dir) {
		t.Fatal("toollog.jsonl 不存在时应为 false")
	}

	// 零字节文件——存在但无遥测内容。
	empty := t.TempDir()
	if err := os.MkdirAll(dataDir(empty), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir(empty), toollogFile), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if ToollogHasData(empty) {
		t.Fatal("toollog.jsonl 零字节时应为 false")
	}

	if err := Record(dir, &ToolCall{ToolName: "Read", TaskRef: "t"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !ToollogHasData(dir) {
		t.Fatal("记入条目后应为 true")
	}
}

// TestToollogAnyData 探测面含归档（对抗审查 should-fix 的回归钉）：active 空但归档
// 非空 → true；两者全空/不存在 → false。
func TestToollogAnyData(t *testing.T) {
	dir := t.TempDir()
	if ToollogAnyData(dir) {
		t.Fatal("无任何 toollog 应 false")
	}
	// 仅归档非空（active 被另一 task start 归档后的形态）。
	arch := filepath.Join(dataDir(dir), "toollog-20260905-000000.000.jsonl")
	if err := os.MkdirAll(filepath.Dir(arch), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arch, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ToollogAnyData(dir) {
		t.Fatal("归档非空应 true——只看 active 会把该形态误判遥测未接")
	}
}
