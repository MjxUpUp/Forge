package nodestamp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/hlc"
	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// withHome 隔离 FORGE_DATA_HOME 并重置进程级单例。
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dir)
	resetForTest()
	t.Cleanup(resetForTest)
	return dir
}

func TestNext_StampsAllFields(t *testing.T) {
	withHome(t)
	s := Next()
	if !nodeid.ValidNodeID(s.NodeID) {
		t.Fatalf("NodeID %q invalid", s.NodeID)
	}
	if s.Seq != 1 {
		t.Fatalf("first Seq = %d, want 1 (pre-increment)", s.Seq)
	}
	if _, err := hlc.Parse(s.TsHLC); err != nil {
		t.Fatalf("TsHLC %q unparsable: %v", s.TsHLC, err)
	}
	if s.Sig != "" {
		t.Fatalf("v1 Sig must be empty, got %q", s.Sig)
	}
}

func TestNext_SeqMonotonicAcrossCalls(t *testing.T) {
	withHome(t)
	a := Next()
	b := Next()
	c := Next()
	if !(a.Seq < b.Seq && b.Seq < c.Seq) {
		t.Fatalf("seq not monotonic: %d %d %d", a.Seq, b.Seq, c.Seq)
	}
}

func TestNext_SeqSurvivesProcessRestart(t *testing.T) {
	withHome(t)
	first := Next()
	resetForTest() // simulate process restart: in-memory state gone, file persists
	second := Next()
	if second.Seq <= first.Seq {
		t.Fatalf("seq %d after restart not ahead of %d (reuse poisons (node_id,seq) dedup)", second.Seq, first.Seq)
	}
	if second.NodeID != first.NodeID {
		t.Fatalf("node identity changed across restart: %q vs %q", second.NodeID, first.NodeID)
	}
}

func TestNext_ConcurrentUniqueSeq(t *testing.T) {
	withHome(t)
	const n = 32
	out := make(chan Stamp, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); out <- Next() }()
	}
	wg.Wait()
	close(out)
	seen := map[int64]bool{}
	for s := range out {
		if s.Seq == 0 {
			t.Fatal("zero seq under concurrency (fail-open triggered without cause)")
		}
		if seen[s.Seq] {
			t.Fatalf("duplicate seq %d", s.Seq)
		}
		seen[s.Seq] = true
	}
}

func TestNext_CorruptCounterFailsOpen(t *testing.T) {
	home := withHome(t)
	s1 := Next()
	if s1.Seq == 0 {
		t.Fatal("baseline stamp failed")
	}
	// Corrupt the counter: stamping must NOT silently restart at 1 (that would reuse
	// seqs already on disk and poison dedup) — it fails open to a zero stamp instead.
	if err := os.WriteFile(filepath.Join(home, "node-seq"), []byte(`garbage`), 0600); err != nil {
		t.Fatalf("write corrupt counter: %v", err)
	}
	resetForTest()
	s2 := Next()
	if s2 != (Stamp{}) {
		t.Fatalf("corrupt counter must yield zero Stamp (fail-open), got %+v", s2)
	}
}

func TestNext_HLCMonotonicAcrossCalls(t *testing.T) {
	withHome(t)
	a := Next()
	b := Next()
	ta, _ := hlc.Parse(a.TsHLC)
	tb, _ := hlc.Parse(b.TsHLC)
	if hlc.Compare(tb, ta) <= 0 {
		t.Fatalf("ts_hlc not monotonic: %q then %q", a.TsHLC, b.TsHLC)
	}
}

func TestNext_FreshLockContentionFailsOpen(t *testing.T) {
	home := withHome(t)
	// 他人持有的新锁：Next 必须最多等 seqLockMaxWait 然后 fail-open（零戳）——
	// 绝不报错，绝不无限阻塞事件路径。
	if err := os.WriteFile(filepath.Join(home, "node-seq.lock"), []byte("other\n"), 0644); err != nil {
		t.Fatalf("plant lock: %v", err)
	}
	start := time.Now()
	s := Next()
	if s != (Stamp{}) {
		t.Fatalf("contended counter must yield zero Stamp, got %+v", s)
	}
	if elapsed := time.Since(start); elapsed > seqLockMaxWait+2*time.Second {
		t.Fatalf("contention wait %v exceeded budget", elapsed)
	}
}

func TestNext_StaleLockBroken(t *testing.T) {
	home := withHome(t)
	// stale 锁（持锁者崩溃）被打破，打戳继续。
	lock := filepath.Join(home, "node-seq.lock")
	if err := os.WriteFile(lock, []byte("dead\n"), 0644); err != nil {
		t.Fatalf("plant lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	s := Next()
	if s.Seq != 1 {
		t.Fatalf("stale lock not broken: %+v", s)
	}
}

func TestNext_CorruptIdentityFailsOpen(t *testing.T) {
	home := withHome(t)
	if err := os.WriteFile(filepath.Join(home, "node.json"), []byte(`{corrupt`), 0600); err != nil {
		t.Fatalf("write corrupt identity: %v", err)
	}
	if s := Next(); s != (Stamp{}) {
		t.Fatalf("corrupt identity must yield zero Stamp, got %+v", s)
	}
	// 失败按进程缓存（idTried）：hook 热路径不会对损坏的 node.json 每条事件付一次
	// 磁盘读——第二次调用仍快速 fail-open。
	if s := Next(); s != (Stamp{}) {
		t.Fatalf("cached failure must stay zero, got %+v", s)
	}
}

// TestHelperProcess is the child side of TestBumpSeq_CrossProcess: bumps the counter twice in a SEPARATE process and prints the values.
//
// TestHelperProcess 是 TestBumpSeq_CrossProcess 的子进程侧：在独立进程里把计数器
// 增两次并打印值。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("NODESTAMP_HELPER") != "1" {
		return
	}
	a, err := bumpSeq()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bump1: %v", err)
		os.Exit(1)
	}
	b, err := bumpSeq()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bump2: %v", err)
		os.Exit(1)
	}
	fmt.Printf("%d,%d", a, b)
	os.Exit(0)
}

func TestBumpSeq_CrossProcess(t *testing.T) {
	withHome(t)
	// 子进程经同一条带锁路径把计数器从 0 增到 2。（FORGE_DATA_HOME 已随 t.Setenv
	// 进 os.Environ——不重复传键。）
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	cmd.Env = append(os.Environ(), "NODESTAMP_HELPER=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper: %v", err)
	}
	if string(out) != "1,2" {
		t.Fatalf("helper bumped %q, want 1,2", out)
	}
	// Parent continues at 3 — the file counter is the cross-process source of truth.
	resetForTest()
	next, err := bumpSeq()
	if err != nil {
		t.Fatalf("parent bump: %v", err)
	}
	if next != 3 {
		t.Fatalf("parent seq = %d, want 3 (child persisted 2)", next)
	}
}

func TestStamp_ZeroValueOmitsAllFields(t *testing.T) {
	// 零值戳必须序列化为无字段（omitempty），未打戳事件与打戳前格式字节一致——
	// 老读者无感，无 schema 升级。
	if got := (Stamp{}).StringJSON(); strings.Contains(got, "node_id") || strings.Contains(got, "seq") || strings.Contains(got, "ts_hlc") || strings.Contains(got, "sig") {
		t.Fatalf("zero stamp leaked fields into JSON: %s", got)
	}
}
