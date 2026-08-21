package hlc

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b Timestamp
		want int
	}{
		{Timestamp{Wall: 1, Logical: 0}, Timestamp{Wall: 1, Logical: 0}, 0},
		{Timestamp{Wall: 2, Logical: 0}, Timestamp{Wall: 1, Logical: 99}, 1}, // wall dominates
		{Timestamp{Wall: 1, Logical: 1}, Timestamp{Wall: 1, Logical: 0}, 1},  // logical breaks wall tie
		{Timestamp{Wall: 1, Logical: 0}, Timestamp{Wall: 1, Logical: 1}, -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%+v,%+v) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%+v,%+v) = %d, want %d (antisymmetry)", c.b, c.a, got, -c.want)
		}
	}
}

func TestStringParse_Roundtrip(t *testing.T) {
	ts := Timestamp{Wall: 1750000000000, Logical: 42}
	s := ts.String()
	got, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	if got != ts {
		t.Fatalf("roundtrip = %+v, want %+v", got, ts)
	}
	// zero-padded logical keeps string order == Compare order for same wall width.
	a := Timestamp{Wall: 100, Logical: 7}
	b := Timestamp{Wall: 100, Logical: 123}
	if !(a.String() < b.String()) {
		t.Fatalf("string order broken: %q !< %q", a.String(), b.String())
	}
}

func TestParse_RejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "1", "1.", ".1", "1.2.3", "a.b", "1.x", "-1.0", "1.-1"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted malformed input", bad)
		}
	}
}

// fixedClock returns a Clock with a controllable wall source (millis).
//
// fixedClock 返回墙钟源可控的 Clock（毫秒）。
func fixedClock(wall *int64) *Clock {
	return NewClock(func() time.Time { return time.UnixMilli(*wall) })
}

func TestNow_MonotonicUnderWallClockJumpBack(t *testing.T) {
	wall := int64(1000)
	c := fixedClock(&wall)
	first := c.Now()
	wall = 500 // NTP jump-back / manual clock set
	second := c.Now()
	if Compare(second, first) <= 0 {
		t.Fatalf("clock jump-back broke monotonicity: first %+v second %+v", first, second)
	}
	if second.Wall != first.Wall || second.Logical != first.Logical+1 {
		t.Fatalf("jump-back should tick logical on frozen wall, got %+v after %+v", second, first)
	}
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

func TestObserve_RemoteAheadPullsLocalUp(t *testing.T) {
	wall := int64(1000)
	c := fixedClock(&wall)
	_ = c.Now()
	remote := Timestamp{Wall: 5000, Logical: 3}
	got := c.Observe(remote)
	if Compare(got, remote) <= 0 {
		t.Fatalf("Observe(%+v) = %+v, must exceed remote", remote, got)
	}
	// subsequent Now stays ahead of the observed remote even with wall still behind.
	next := c.Now()
	if Compare(next, remote) <= 0 {
		t.Fatalf("post-Observe Now %+v not ahead of remote %+v", next, remote)
	}
}

func TestObserve_RemoteBehindIgnored(t *testing.T) {
	wall := int64(9000)
	c := fixedClock(&wall)
	before := c.Now()
	got := c.Observe(Timestamp{Wall: 1, Logical: 0})
	if got.Wall != 9000 || Compare(got, before) <= 0 {
		t.Fatalf("Observe(stale) = %+v, want fresh local tick ahead of %+v", got, before)
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

func TestTimestamp_ZeroIsUsable(t *testing.T) {
	var z Timestamp
	if !strings.HasPrefix(z.String(), "0.") {
		t.Fatalf("zero timestamp String = %q", z.String())
	}
}
