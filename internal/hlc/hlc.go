// Package hlc implements a Hybrid Logical Clock (HLC): the LWW tie-break timestamp
// for multi-machine convergence (docs/design/sync-convergence.md §3). A Timestamp is
// (Wall unix-millis, Logical counter): physical time orders events in the common
// case, the logical counter keeps order monotonic under clock jump-back and breaks
// same-millisecond ties deterministically.
//
// Why not bare updated_at: the Redlock debate's core lesson is that clocks are
// untrustworthy (GC pauses, NTP steps). Bare-timestamp LWW picks wrong, irreproducible
// winners under skew; HLC stays monotonic without any clock-sync quality assumption.
// Final tie-break (identical HLC) is node_id lexicographic — determinism over
// correctness: two machines must converge to the SAME result, even if "wrong".
//
// Package hlc 实现混合逻辑时钟（HLC）：多机收敛的 LWW 决胜时间戳（见
// docs/design/sync-convergence.md §3）。Timestamp = (Wall unix 毫秒, Logical 计数器)：
// 常态物理时间排序，逻辑计数器在时钟回拨下保持单调，并对同毫秒事件确定性决胜。
package hlc

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Timestamp is one HLC reading: Wall = unix milliseconds, Logical = tie-break counter.
//
// Timestamp 是一次 HLC 读数：Wall = unix 毫秒，Logical = 决胜计数器。
type Timestamp struct {
	Wall    int64 `json:"wall"`
	Logical int32 `json:"logical"`
}

// Compare returns -1/0/+1 for a </=/> b. Wall dominates; Logical breaks wall ties.
//
// Compare 返回 -1/0/+1（a </=/> b）。Wall 主导；Logical 破 Wall 平手。
func Compare(a, b Timestamp) int {
	switch {
	case a.Wall != b.Wall:
		if a.Wall < b.Wall {
			return -1
		}
		return 1
	case a.Logical != b.Logical:
		if a.Logical < b.Logical {
			return -1
		}
		return 1
	default:
		return 0
	}
}

// String renders "<wall>.<logical 6-digit zero-padded>" — fixed-width logical keeps
// string order consistent with Compare for same-width wall values.
//
// String 渲染为 "<wall>.<6 位零填充 logical>"——定宽 logical 让同位宽 wall 的
// 字符串序与 Compare 序一致。
func (t Timestamp) String() string {
	return strconv.FormatInt(t.Wall, 10) + `.` + fmt.Sprintf(`%06d`, t.Logical)
}

// Parse parses a String-produced timestamp. Malformed input is an error — HLC strings
// arrive over sync boundaries and must fail loud, not silently become zero.
//
// Parse 解析 String 产出的时间戳。畸形输入是错误——HLC 字符串跨越同步边界到来，
// 必须响亮失败，不能静默变零值。
func Parse(s string) (Timestamp, error) {
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot == len(s)-1 || strings.IndexByte(s[dot+1:], '.') >= 0 {
		return Timestamp{}, fmt.Errorf(`malformed hlc %q`, s)
	}
	wall, err := strconv.ParseInt(s[:dot], 10, 64)
	if err != nil {
		return Timestamp{}, fmt.Errorf(`malformed hlc wall %q: %w`, s, err)
	}
	logical, err := strconv.ParseInt(s[dot+1:], 10, 32)
	if err != nil {
		return Timestamp{}, fmt.Errorf(`malformed hlc logical %q: %w`, s, err)
	}
	if wall < 0 || logical < 0 {
		return Timestamp{}, fmt.Errorf(`malformed hlc %q: negative component`, s)
	}
	return Timestamp{Wall: wall, Logical: int32(logical)}, nil
}

// Clock is a process-local HLC source, safe for concurrent use. The wall source is
// injectable for tests (clock skew injection — convergence tests MUST simulate
// jump-back, not assume the host clock behaves).
//
// Clock 是进程内 HLC 源，并发安全。墙钟源可注入供测试（时钟偏斜注入——收敛测试
// 必须模拟回拨，不能假设宿主时钟乖）。
type Clock struct {
	mu   sync.Mutex
	last Timestamp
	wall func() time.Time
}

// NewClock builds a Clock on the given wall source (nil = time.Now).
//
// NewClock 以给定墙钟源建 Clock（nil = time.Now）。
func NewClock(wall func() time.Time) *Clock {
	if wall == nil {
		wall = time.Now
	}
	return &Clock{wall: wall}
}

// Now returns a fresh timestamp, strictly greater than every previous reading from
// this Clock — even if the wall clock jumped back (logical ticks on frozen wall).
//
// Now 返回严格大于本 Clock 既往一切读数的新时间戳——墙钟回拨也不例外（墙钟冻结
// 时逻辑位递增）。
func (c *Clock) Now() Timestamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tickLocked(c.wall().UnixMilli())
}

// Observe merges a remote timestamp (received over sync) and returns a fresh local
// one strictly greater than it: the classic HLC recv rule. This is what makes
// "received a future event" never poison local monotonicity.
//
// Observe 合并远端时间戳（经同步收到）并返回严格大于它的本地新时间戳——经典 HLC
// recv 规则。「收到未来事件」因此永不毒害本地单调性。
func (c *Clock) Observe(remote Timestamp) Timestamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := c.wall().UnixMilli()
	switch {
	case wall > c.last.Wall && wall > remote.Wall:
		c.last = Timestamp{Wall: wall, Logical: 0}
	case c.last.Wall > remote.Wall:
		c.last = Timestamp{Wall: c.last.Wall, Logical: c.last.Logical + 1}
	case remote.Wall > c.last.Wall:
		c.last = Timestamp{Wall: remote.Wall, Logical: remote.Logical + 1}
	default: // all three walls equal
		c.last = Timestamp{Wall: c.last.Wall, Logical: max(c.last.Logical, remote.Logical) + 1}
	}
	return c.last
}

// tickLocked advances last strictly monotonically against wallMillis.
//
// tickLocked 对 wallMillis 严格单调推进 last。
func (c *Clock) tickLocked(wallMillis int64) Timestamp {
	if wallMillis > c.last.Wall {
		c.last = Timestamp{Wall: wallMillis, Logical: 0}
	} else {
		c.last = Timestamp{Wall: c.last.Wall, Logical: c.last.Logical + 1}
	}
	return c.last
}
