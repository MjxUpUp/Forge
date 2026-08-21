package taskpipeline

// lease.go — cross-machine task leases (docs/design/sync-convergence.md §4).
//
// A lease says "node X is actively working this task". v1 semantics are ADVISORY
// (personal profile): a foreign active lease surfaces a gate-time note, never a
// block — the Redlock lesson is that TTL leases cannot guarantee mutual exclusion
// under clock skew/GC pauses, so correctness never depends on them. The fencing
// counter is what upgrades a claim to a comparable term: higher fencing always wins
// a merge, so a stale holder's late write cannot displace a newer claim.
//
// lease.go —— 跨机器任务租约（docs/design/sync-convergence.md §4）。
//
// 租约表达「节点 X 正在处理此任务」。v1 语义是 advisory（个人档）：他机活跃租约
// 在门禁时提示，绝不阻断——Redlock 的教训是 TTL 租约在时钟偏斜/GC 停顿下保证不了
// 互斥，正确性绝不依赖它。fencing 计数器把认领升级为可比较的任期：合并时高
// fencing 恒胜，过期持有者的迟到写入顶替不了更新的认领。

import (
	"fmt"
	"time"

	"github.com/MjxUpUp/Forge/internal/hlc"
	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// Lease is one node's claim on a task (TaskState.lease).
//
// Lease 是一个节点对任务的认领（TaskState.lease）。
type Lease struct {
	HolderNode string `json:"holder_node"`       // fnode_<32hex>
	TsHLC      string `json:"ts_hlc"`            // 认领时刻 HLC
	TTLSec     int64  `json:"ttl_sec"`           // 租约时长（秒）
	Fencing    int64  `json:"fencing"`           // 单调任期号（合并决胜）
	ClaimedAt  int64  `json:"claimed_at_unixms"` // 认领时刻墙钟毫秒（TTL 判定用）
}

// ClaimLease stamps a fresh lease for nodeID: fencing increments monotonically (a
// re-claim by the same holder is a new term). Fail-soft on bad HLC state is not
// needed — the clock is caller-provided and always ticks.
//
// ClaimLease 为 nodeID 落新租约：fencing 单调递增（同一持有者再认领也是新任期）。
func ClaimLease(s *TaskState, nodeID string, clock *hlc.Clock, ttlSec int64) {
	var fencing int64
	if s.Lease != nil {
		fencing = s.Lease.Fencing
	}
	ts := clock.Now()
	s.Lease = &Lease{
		HolderNode: nodeID,
		TsHLC:      ts.String(),
		TTLSec:     ttlSec,
		Fencing:    fencing + 1,
		ClaimedAt:  ts.Wall,
	}
}

// LeaseState is the advisory verdict for one node's view of a task's lease.
//
// LeaseState 是一个节点视角下任务租约的 advisory 判定。
type LeaseState struct {
	ForeignActive bool   // 他机持有且未过期
	Message       string // 人类可读 advisory（门禁输出用）
}

// LeaseStatus evaluates the task's lease from nodeID's perspective at now.
//
// LeaseStatus 以 nodeID 视角在 now 时刻评估任务租约。
func LeaseStatus(s *TaskState, nodeID string, now time.Time) LeaseState {
	if s.Lease == nil || s.Lease.HolderNode == `` || s.Lease.HolderNode == nodeID {
		return LeaseState{}
	}
	expires := time.UnixMilli(s.Lease.ClaimedAt).Add(time.Duration(s.Lease.TTLSec) * time.Second)
	if !now.Before(expires) {
		return LeaseState{} // 过期即自由（TTL 租约不做正确性承诺）
	}
	return LeaseState{
		ForeignActive: true,
		Message: fmt.Sprintf(`任务 %s 当前由节点 %s 持有租约（fencing %d，%s 前有效）——若是你的另一台机器可忽略；若非，先 forge project sync pull 对齐状态再动手`,
			s.TaskRef, s.Lease.HolderNode, s.Lease.Fencing, expires.Format(`15:04`)),
	}
}

// defaultLeaseTTLSec is the v1 lease TTL: long enough to cover a work sitting,
// short enough that a crashed machine's lease frees the task the same day.
//
// defaultLeaseTTLSec 是 v1 租约 TTL：长到覆盖一次工作坐段，短到崩溃机器的租约
// 当天释放任务。
const defaultLeaseTTLSec = 4 * 3600

// ClaimLeaseForCurrentNode claims the task lease for THIS machine, failing open
// (identity load problems never block task work — a lease is advisory metadata).
//
// ClaimLeaseForCurrentNode 为本机认领任务租约，fail-open（身份加载问题绝不阻塞
// 任务工作——租约是 advisory 元数据）。
func ClaimLeaseForCurrentNode(s *TaskState) {
	id, err := nodeid.LoadOrCreate()
	if err != nil {
		return
	}
	ClaimLease(s, id.NodeID, hlc.NewClock(nil), defaultLeaseTTLSec)
}

// LeaseStatusForCurrentNode evaluates the lease from THIS machine's perspective
// (fail-open: identity problems read as "no foreign lease").
//
// LeaseStatusForCurrentNode 以本机视角评估租约（fail-open：身份问题按「无他机
// 租约」处理）。
func LeaseStatusForCurrentNode(s *TaskState) LeaseState {
	id, err := nodeid.LoadOrCreate()
	if err != nil {
		return LeaseState{}
	}
	return LeaseStatus(s, id.NodeID, time.Now())
}

// mergeLeaseSync resolves lease conflicts: HIGHER FENCING WINS (a newer claim
// displaces an older one — that is the entire point of a fencing token); equal
// fencing breaks by holder node id for determinism.
//
// mergeLeaseSync 裁决租约冲突：fencing 高者胜（更新的认领取代旧的——这正是 fencing
// token 的全部意义）；fencing 相等按持有者 node id 字典序保证确定性。
func mergeLeaseSync(local, incoming *TaskState) {
	switch {
	case incoming.Lease == nil:
		return
	case local.Lease == nil:
		local.Lease = incoming.Lease
	case incoming.Lease.Fencing > local.Lease.Fencing:
		local.Lease = incoming.Lease
	case incoming.Lease.Fencing == local.Lease.Fencing && canonicalJSON(incoming.Lease) < canonicalJSON(local.Lease):
		local.Lease = incoming.Lease
	}
}
