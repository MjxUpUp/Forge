// Package hlc implements a Hybrid Logical Clock (HLC): the LWW tie-break timestamp for multi-machine convergence (docs/design/sync-convergence.md §3).
//
// Package hlc 实现混合逻辑时钟（HLC）：多机收敛的 LWW 决胜时间戳（见
// docs/design/sync-convergence.md §3）。Timestamp = (Wall unix 毫秒, Logical 计数器)：
// 常态物理时间排序，逻辑计数器在时钟回拨下保持单调，并对同毫秒事件确定性决胜。
// 为何不用裸 updated_at：Redlock 论战的核心教训是时钟不可信（GC 停顿、NTP 跳变），
// 裸时间戳 LWW 在偏斜下选出错误且不可复现的胜者；HLC 不依赖任何时钟同步质量假设
// 保持单调。完全相同 HLC 的最终决胜按 node_id 字典序——确定性优先于正确性：
// 两台机器必须收敛到同一结果，即便「错」。
//
// 状态警示（2026-09 代码普查）：Compare/Parse/Clock.Observe 当前无生产接线——
// 与 docs/design/sync-convergence.md §3 的实现校正一致（ts_hlc 未落盘，不作为
// 正确性论证依据）。保留是为既定设计的完整性（测试钉住语义）；接线前不得依赖
// 本包参与任何收敛决策。
package hlc

import (
	"fmt"
	"math"
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

// Compare returns -1/0/+1 for a </=/> b.
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

// String renders the canonical fixed-width form "<wall 19-digit>.<logical 10-digit>".
//
// String 渲染为规范定宽形 "<wall 19 位>.<logical 10 位>"。两分量按类型位宽零填充，
// 故所有非负值上字符串序 == Compare 序——ts_hlc 字符串因此可直接当合并路径的
// 排序键（sync-convergence §2），无需自定义比较器。
func (t Timestamp) String() string {
	return fmt.Sprintf(`%019d.%010d`, t.Wall, t.Logical)
}

// Parse parses a timestamp string.
//
// Parse 解析时间戳字符串。每分量仅数字（无 '+'、无符号），同值的非规范编码无法
// 在同步边界上拆裂去重键；溢出与畸形输入响亮失败，绝不静默变零值。
func Parse(s string) (Timestamp, error) {
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot == len(s)-1 || strings.IndexByte(s[dot+1:], '.') >= 0 {
		return Timestamp{}, fmt.Errorf(`malformed hlc %q`, s)
	}
	wallPart, logicalPart := s[:dot], s[dot+1:]
	for _, part := range []string{wallPart, logicalPart} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return Timestamp{}, fmt.Errorf(`malformed hlc %q: non-digit component`, s)
			}
		}
	}
	wall, err := strconv.ParseInt(wallPart, 10, 64)
	if err != nil {
		return Timestamp{}, fmt.Errorf(`malformed hlc wall %q: %w`, s, err)
	}
	logical, err := strconv.ParseInt(logicalPart, 10, 32)
	if err != nil {
		return Timestamp{}, fmt.Errorf(`malformed hlc logical %q: %w`, s, err)
	}
	return Timestamp{Wall: wall, Logical: int32(logical)}, nil
}

// Clock is a process-local HLC source, safe for concurrent use.
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

// Now returns a fresh timestamp, strictly greater than every previous reading from this Clock — even if the wall clock jumped back (logical ticks on frozen wall).
//
// Now 返回严格大于本 Clock 既往一切读数的新时间戳——墙钟回拨也不例外（墙钟冻结
// 时逻辑位递增）。
func (c *Clock) Now() Timestamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tickLocked(c.wall().UnixMilli())
}

// Observe merges a remote timestamp (received over sync) and returns a fresh local one strictly greater than it: the classic HLC recv rule.
//
// STATUS (v1, fix/dsh-review-followup): UNWIRED — no production caller yet. Today's
// decisive keys are fencing + canonical bytes (see the sync-convergence §3 实现校正);
// stamps/leases carry ts_hlc as a display + reserved field. Wire this when the first
// cross-machine ts_hlc consumer lands (A-class G-Set / LWW), together with clock
// persistence — a per-process clock alone cannot deliver cross-process monotonicity.
//
// Observe 合并远端时间戳（经同步收到）并返回严格大于它的本地新时间戳——经典 HLC
// recv 规则。「收到未来事件」因此永不毒害本地单调性。
//
// 状态（v1，fix/dsh-review-followup）：未接线——暂无生产调用方。今日决胜键是
// fencing + 规范字节（见 sync-convergence §3 实现校正）；戳/租约里的 ts_hlc 是展示
// 与预留字段。待首个跨机 ts_hlc 消费方（A 类 G-Set / LWW）落地时接线，并配套时钟
// 持久化——仅进程内时钟给不出跨进程单调性。
func (c *Clock) Observe(remote Timestamp) Timestamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := c.wall().UnixMilli()
	switch {
	case wall > c.last.Wall && wall > remote.Wall:
		c.last = Timestamp{Wall: wall, Logical: 0}
	case c.last.Wall > remote.Wall:
		c.last = advanceLocked(c.last.Wall, c.last.Logical)
	case remote.Wall > c.last.Wall:
		c.last = advanceLocked(remote.Wall, remote.Logical)
	default: // remote.Wall == c.last.Wall (local wall may lag behind both)
		c.last = advanceLocked(c.last.Wall, max(c.last.Logical, remote.Logical))
	}
	return c.last
}

// tickLocked 对 wallMillis 严格单调推进 last。
func (c *Clock) tickLocked(wallMillis int64) Timestamp {
	if wallMillis > c.last.Wall {
		c.last = Timestamp{Wall: wallMillis, Logical: 0}
	} else {
		c.last = advanceLocked(c.last.Wall, c.last.Logical)
	}
	return c.last
}

// advanceLocked 递增逻辑计数器，到 math.MaxInt32 时饱和为 {wall+1, 0} 而非回绕
// 为负——回绕会静默破坏单调性且产出 Parse 拒收的字符串（负分量），毒害下游一切
// 合并。冻结墙钟 + MaxInt32 次递增远超真实负载，但失败必须响亮安全，不能静默。
func advanceLocked(wall int64, logical int32) Timestamp {
	if logical == math.MaxInt32 {
		return Timestamp{Wall: wall + 1, Logical: 0}
	}
	return Timestamp{Wall: wall, Logical: logical + 1}
}
