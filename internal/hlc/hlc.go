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

// String renders the canonical fixed-width form "<wall 19-digit>.<logical 10-digit>".
//
// String 渲染为规范定宽形 "<wall 19 位>.<logical 10 位>"。两分量按类型位宽零填充，
// 故所有非负值上字符串序 == Compare 序——ts_hlc 字符串因此可直接当合并路径的
// 排序键（sync-convergence §2），无需自定义比较器。
func (t Timestamp) String() string {
	return fmt.Sprintf(`%019d.%010d`, t.Wall, t.Logical)
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
