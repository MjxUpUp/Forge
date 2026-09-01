package taskpipeline

// lease.go —— 跨机器任务租约（docs/design/sync-convergence.md §4）。
//
// 租约表达「节点 X 正在处理此任务」。v1 语义是 advisory（个人档）：他机活跃租约
// 在门禁时提示，绝不阻断——Redlock 的教训是 TTL 租约在时钟偏斜/GC 停顿下保证不了
// 互斥，正确性绝不依赖它。fencing 计数器把认领升级为可比较的任期：合并时高
// fencing 恒胜，过期持有者的迟到写入顶替不了更新的认领。

import (
	"fmt"
	"os"
	"time"

	"github.com/MjxUpUp/Forge/internal/hlc"
	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// ClaimLease stamps a fresh lease for nodeID: fencing increments monotonically (a re-claim by the same holder is a new term).
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
	if !s.Lease.ActiveAt(now) {
		return LeaseState{} // 过期即自由（TTL 租约不做正确性承诺）
	}
	expires := s.Lease.ExpiresAt()
	return LeaseState{
		ForeignActive: true,
		Message: fmt.Sprintf(`任务 %s 当前由节点 %s 持有租约（fencing %d，%s 前有效）——若是你的另一台机器可忽略；若非，先 forge project sync pull 对齐状态再动手`,
			s.TaskRef, s.Lease.HolderNode, s.Lease.Fencing, expires.Format(`15:04`)),
	}
}

// defaultLeaseTTLSec 是 v1 租约 TTL：长到覆盖一次工作坐段，短到崩溃机器的租约
// 当天释放任务。
const defaultLeaseTTLSec = 4 * 3600

// leaseDegradeNoted 让 fail-open 告警每进程只打一行（下面两个入口都跑在短命 CLI
// 进程里；无锁 bool 即可——重复告警无害，漏告警才有害）。
var leaseDegradeNoted bool

// warnLeaseDegrade 暴露 fail-open 背后的身份失败：否则损坏的 node.json 在门禁时
// 读作「无他机租约」、task start 后读作「无租约」——跨机 advisory 静默失效，且与
// 「没什么可报」无法区分。
func warnLeaseDegrade(op string, err error) {
	if leaseDegradeNoted {
		return
	}
	leaseDegradeNoted = true
	fmt.Fprintf(os.Stderr, `⚠ forge: %s 跳过租约（fail-open——身份加载失败：%v）`+"\n", op, err)
}

// ClaimLeaseForCurrentNode claims the task lease for THIS machine, failing open (identity load problems never block task work — a lease is advisory metadata).
//
// ClaimLeaseForCurrentNode 为本机认领任务租约，fail-open（身份加载问题绝不阻塞
// 任务工作——租约是 advisory 元数据）。
func ClaimLeaseForCurrentNode(s *TaskState) {
	id, err := nodeid.LoadOrCreate()
	if err != nil {
		warnLeaseDegrade(`task start/attach`, err)
		return
	}
	ClaimLease(s, id.NodeID, hlc.NewClock(nil), defaultLeaseTTLSec)
}

// LeaseStatusForCurrentNode evaluates the lease from THIS machine's perspective (fail-open: identity problems read as "no foreign lease").
//
// LeaseStatusForCurrentNode 以本机视角评估租约（fail-open：身份问题按「无他机
// 租约」处理）。
func LeaseStatusForCurrentNode(s *TaskState) LeaseState {
	id, err := nodeid.LoadOrCreate()
	if err != nil {
		warnLeaseDegrade(`gate 租约检查`, err)
		return LeaseState{}
	}
	return LeaseStatus(s, id.NodeID, time.Now())
}

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
