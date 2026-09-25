package nodeid

// nodeid_race_test.go —— 并发首跑契约：N 个进程同时在新 home 上 LoadOrCreate
//（agent 并行发工具调用时 hook 本就并发）必须收敛到同一身份。裸的
// load-失败→generate→save 竞态败者会继续用磁盘上已不存在的 node_id 打戳——
// 这正是 LoadOrCreate 要防的归因分叉。

import (
	"sync"
	"testing"
)

func TestLoadOrCreate_ConcurrentFirstRun_ConvergesToOneIdentity(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())

	const n = 64
	ids := make([]string, n)
	errs := make([]error, n)
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-barrier // release all goroutines onto the empty home at once
			id, err := LoadOrCreate()
			if err == nil {
				ids[i] = id.NodeID
			}
			errs[i] = err
		}(i)
	}
	close(barrier)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: LoadOrCreate: %v", i, err)
		}
	}
	first := ids[0]
	for i, id := range ids {
		if id != first {
			t.Fatalf("concurrent first-run forked: goroutine %d got %s, goroutine 0 got %s — exactly one identity must win and every caller must adopt it", i, id, first)
		}
	}
	disk, err := Load()
	if err != nil {
		t.Fatalf("load on-disk identity: %v", err)
	}
	if disk.NodeID != first {
		t.Fatalf("on-disk identity %s disagrees with the adopted identity %s", disk.NodeID, first)
	}
}
