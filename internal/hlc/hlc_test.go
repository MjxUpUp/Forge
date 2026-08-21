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
	for _, bad := range []string{"", "1", "1.", ".1", "1.2.3", "a.b", "1.x", "-1.0", "1.-1",
		"+1.2",                   // ParseInt accepts leading +; canonical form never has it (dedup-key hygiene)
		"1.2147483648",           // logical int32 overflow
		"99999999999999999999.1", // wall int64 overflow
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted malformed input", bad)
		}
	}
}

// TestString_OrderMatchesCompare pins the property consumers rely on when ts_hlc
// strings are used as sort keys (sync-convergence §2 merge path): for ALL
// non-negative values, string order == Compare order — including the wall-width
// boundary (13→14 digits) and logical values past 6 digits.
//
// TestString_OrderMatchesCompare 钉死消费方把 ts_hlc 字符串当排序键时依赖的性质
// （sync-convergence §2 合并路径）：所有非负值上字符串序 == Compare 序——含 wall
// 位宽跨界（13→14 位）与超 6 位的 logical。
func TestString_OrderMatchesCompare(t *testing.T) {
	vals := []Timestamp{
		{Wall: 0, Logical: 0},
		{Wall: 100, Logical: 7},
		{Wall: 100, Logical: 123},
		{Wall: 100, Logical: 1000000},    // past 6 digits
		{Wall: 100, Logical: 2147483647}, // int32 max
		{Wall: 999999999999, Logical: 999999},
		{Wall: 1000000000000, Logical: 0}, // 13→14 digit boundary
		{Wall: 1750000000000, Logical: 42},
	}
	for i, a := range vals {
		for j, b := range vals {
			cmp := Compare(a, b)
			str := strings.Compare(a.String(), b.String())
			if (cmp < 0) != (str < 0) || (cmp == 0) != (str == 0) {
				t.Fatalf("order mismatch at (%d,%d): Compare=%d strings %q vs %q", i, j, cmp, a.String(), b.String())
			}
		}
	}
}

// TestNow_LogicalSaturationAdvancesWall covers the int32 overflow path: on a frozen
// wall clock, Logical must NEVER wrap negative (that would silently break
// monotonicity AND produce unparsable strings) — saturation advances Wall instead.
//
// TestNow_LogicalSaturationAdvancesWall 覆盖 int32 溢出路径：墙钟冻结时 Logical
// 绝不回绕为负（那会静默破坏单调性且产出不可解析字符串）——饱和改为推进 Wall。
func TestNow_LogicalSaturationAdvancesWall(t *testing.T) {
	wall := int64(1000)
	c := fixedClock(&wall)
	c.last = Timestamp{Wall: 1000, Logical: 2147483647} // math.MaxInt32
	got := c.Now()
	if got.Wall != 1001 || got.Logical != 0 {
		t.Fatalf("saturated tick = %+v, want {1001 0}", got)
	}
	if _, err := Parse(got.String()); err != nil {
		t.Fatalf("post-saturation string unparsable: %v", err)
	}
	next := c.Now()
	if Compare(next, got) <= 0 {
		t.Fatalf("post-saturation tick %+v not ahead of %+v", next, got)
	}
}

// TestObserve_EqualWallTakesMaxPlusOne directly covers the trickiest recv branch:
// all walls equal (incl. local wall BEHIND last) → logical = max(local, remote)+1.
//
// TestObserve_EqualWallTakesMaxPlusOne 直接覆盖最 tricky 的 recv 分支：三方墙钟
// 相等（含本地墙钟落后于 last）→ logical = max(本地, 远端)+1。
func TestObserve_EqualWallTakesMaxPlusOne(t *testing.T) {
	wall := int64(1000)
	c := fixedClock(&wall)
	c.last = Timestamp{Wall: 2000, Logical: 10}
	got := c.Observe(Timestamp{Wall: 2000, Logical: 5}) // remote behind local logical
	if got.Wall != 2000 || got.Logical != 11 {
		t.Fatalf("Observe equal-wall = %+v, want {2000 11}", got)
	}
	got = c.Observe(Timestamp{Wall: 2000, Logical: 99}) // remote ahead
	if got.Wall != 2000 || got.Logical != 100 {
		t.Fatalf("Observe equal-wall remote-ahead = %+v, want {2000 100}", got)
	}
}

func TestTimestamp_ZeroIsUsable(t *testing.T) {
	var z Timestamp
	if !strings.HasPrefix(z.String(), "0") {
		t.Fatalf("zero timestamp String = %q", z.String())
	}
	if _, err := Parse(z.String()); err != nil {
		t.Fatalf("zero timestamp unparsable: %v", err)
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
