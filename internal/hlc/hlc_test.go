package hlc

import (
	"sync"
	"testing"
	"time"
)

// fixedClock 返回墙钟源可控的 Clock（毫秒）。
func fixedClock(wall *int64) *Clock {
	return NewClock(func() time.Time { return time.UnixMilli(*wall) })
}

func TestNow_AdvancesWithWallClock(t *testing.T) {
	wall := int64(1000)
	c := fixedClock(&wall)
	_ = c.Now() // establish a reading at wall=1000
	wall = 1001
	b := c.Now()
	if b.Wall != 1001 || b.Logical != 0 {
		t.Fatalf("wall advance should reset logical: got %+v", b)
	}
}

func TestNow_ConcurrentUnique(t *testing.T) {
	wall := int64(1000)
	c := fixedClock(&wall)
	const n = 64
	out := make(chan Timestamp, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); out <- c.Now() }()
	}
	wg.Wait()
	close(out)
	seen := map[string]bool{}
	for ts := range out {
		s := ts.String()
		if seen[s] {
			t.Fatalf("duplicate timestamp %s under concurrency", s)
		}
		seen[s] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d unique timestamps, want %d", len(seen), n)
	}
}
