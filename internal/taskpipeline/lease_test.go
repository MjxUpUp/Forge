package taskpipeline

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/hlc"
)

// lease_test.go — task lease semantics (sync-convergence.md §4): fencing-monotonic
// claims, TTL expiry, foreign-lease advisory at gate time, and the merge rule
// (higher fencing wins — that is what fencing exists FOR).
//
// lease_test.go —— 任务租约语义（sync-convergence.md §4）：fencing 单调认领、
// TTL 过期、他机租约门禁 advisory、合并规则（fencing 高者胜——这正是 fencing
// 存在的意义）。

func leaseClock(wall int64) *hlc.Clock {
	return hlc.NewClock(func() time.Time { return time.UnixMilli(wall) })
}

func TestClaimLease_FencingMonotonic(t *testing.T) {
	s := &TaskState{TaskRef: `feat/l`}
	c := leaseClock(1000)
	ClaimLease(s, `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, c, 300)
	if s.Lease == nil || s.Lease.Fencing != 1 {
		t.Fatalf("first claim fencing = %+v, want 1", s.Lease)
	}
	if s.Lease.HolderNode != `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa` {
		t.Fatalf("holder = %q", s.Lease.HolderNode)
	}
	ClaimLease(s, `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, c, 300)
	if s.Lease.Fencing != 2 || s.Lease.HolderNode != `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb` {
		t.Fatalf("second claim = %+v, want fencing 2 new holder", s.Lease)
	}
	// Re-claim by the SAME node still bumps fencing (a fresh claim is a fresh term).
	ClaimLease(s, `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, c, 300)
	if s.Lease.Fencing != 3 {
		t.Fatalf("re-claim fencing = %d, want 3", s.Lease.Fencing)
	}
}

func TestLeaseStatus_ForeignActive(t *testing.T) {
	s := &TaskState{TaskRef: `feat/l`}
	c := leaseClock(1000)
	ClaimLease(s, `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, c, 300)

	st := LeaseStatus(s, `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, time.UnixMilli(2000))
	if !st.ForeignActive {
		t.Fatal("foreign active lease not detected")
	}
	if st.Message == `` {
		t.Fatal("advisory message empty")
	}
	// Holder sees no foreign advisory.
	if st2 := LeaseStatus(s, `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, time.UnixMilli(2000)); st2.ForeignActive {
		t.Fatal("holder flagged as foreign")
	}
	// No lease at all → no advisory.
	if st3 := LeaseStatus(&TaskState{}, `fnode_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`, time.UnixMilli(2000)); st3.ForeignActive {
		t.Fatal("no-lease task flagged")
	}
}

func TestLeaseStatus_Expiry(t *testing.T) {
	s := &TaskState{TaskRef: `feat/l`}
	c := leaseClock(1000) // claimed at t=1000ms with ttl 300s
	ClaimLease(s, `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, c, 300)
	// Way past TTL.
	st := LeaseStatus(s, `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, time.UnixMilli(1000).Add(301*time.Second))
	if st.ForeignActive {
		t.Fatal("expired lease still blocks")
	}
}

// TestLease_ActiveAt pins the single "expiry means free" rule that both LeaseStatus
// and the dashboard feed derive from (feed must not show stale holders).
//
// TestLease_ActiveAt 钉死 LeaseStatus 与 dashboard feed 共同派生的「过期即自由」
// 唯一规则（feed 不得显示过期持有者）。
func TestLease_ActiveAt(t *testing.T) {
	var nilLease *Lease
	if nilLease.ActiveAt(time.Now()) {
		t.Fatal("nil lease must read inactive")
	}
	l := &Lease{ClaimedAt: 1000, TTLSec: 300} // active window: t=1s … t=301s
	if !l.ActiveAt(time.UnixMilli(1000).Add(time.Second)) {
		t.Fatal("lease inside TTL reads inactive")
	}
	if l.ActiveAt(time.UnixMilli(1000).Add(301 * time.Second)) {
		t.Fatal("lease past TTL reads active")
	}
}

func TestLease_MergeHigherFencingWins(t *testing.T) {
	mk := func(holder string, fencing int64) *TaskState {
		s := &TaskState{TaskRef: `feat/l`}
		c := leaseClock(1000)
		for i := int64(0); i < fencing; i++ {
			ClaimLease(s, holder, c, 300)
		}
		return s
	}
	a := mk(`fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, 1)
	b := mk(`fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, 3)
	MergeTaskStateSync(a, b)
	if a.Lease == nil || a.Lease.Fencing != 3 || a.Lease.HolderNode != `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb` {
		t.Fatalf("merge kept %+v, want fencing-3 holder b", a.Lease)
	}
	// direction independence
	a2 := mk(`fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, 1)
	b2 := mk(`fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, 3)
	MergeTaskStateSync(b2, a2)
	if b2.Lease.Fencing != 3 {
		t.Fatalf("reverse merge fencing = %d", b2.Lease.Fencing)
	}
}

// TestLease_MergeEqualFencingDeterministic pins the equal-fencing tiebreak WITH an
// oracle: the canonical-JSON-smaller lease wins (holder_node is the first JSON field,
// so fnode_aaa… beats fnode_bbb… when everything else is equal), in BOTH directions.
// This is the dual-machine simultaneous-claim case — it must converge, not flip.
//
// TestLease_MergeEqualFencingDeterministic 带 oracle 钉死同值 fencing 破平：规范
// JSON 小者胜（holder_node 是 JSON 首字段，其余全等时 fnode_aaa… 胜 fnode_bbb…），
// 两个方向一致。这是双机同时认领的情形——必须收敛，不得来回翻。
func TestLease_MergeEqualFencingDeterministic(t *testing.T) {
	mk := func(holder string) *TaskState {
		s := &TaskState{TaskRef: `feat/l`}
		ClaimLease(s, holder, leaseClock(1000), 300) // both fencing=1, same wall clock
		return s
	}
	const winner = `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
	ab := mk(winner)
	MergeTaskStateSync(ab, mk(`fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`))
	ba := mk(`fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`)
	MergeTaskStateSync(ba, mk(winner))
	if ab.Lease.HolderNode != winner {
		t.Fatalf("equal-fencing ab winner = %q, want %q (canonical order)", ab.Lease.HolderNode, winner)
	}
	if ba.Lease.HolderNode != winner {
		t.Fatalf("equal-fencing ba winner = %q, want %q (direction-independent)", ba.Lease.HolderNode, winner)
	}
}

// TestLease_ExpiresAt pins the single expiry formula (claimed + TTL) that ActiveAt,
// LeaseStatus's advisory message, and the dashboard's state-block projection all
// derive from — the refactor extracted it precisely so the formula cannot drift
// between call sites.
//
// TestLease_ExpiresAt 钉死过期公式（认领 + TTL）的唯一出处——ActiveAt、LeaseStatus
// 的 advisory 文案、看板 state 块投影都从它派生；抽这个方法正是为了让公式无法在
// 各调用点间漂移。
func TestLease_ExpiresAt(t *testing.T) {
	l := &Lease{ClaimedAt: 1000, TTLSec: 300}
	if want := time.UnixMilli(1000).Add(300 * time.Second); !l.ExpiresAt().Equal(want) {
		t.Fatalf("ExpiresAt = %s, want %s", l.ExpiresAt(), want)
	}
	// ActiveAt 的边界语义经 ExpiresAt 派生：恰在过期时刻不活跃（Before 严格小于）。
	if l.ActiveAt(l.ExpiresAt()) {
		t.Fatal("lease at exact expiry must read inactive")
	}
	if !l.ActiveAt(l.ExpiresAt().Add(-time.Nanosecond)) {
		t.Fatal("lease one tick before expiry must read active")
	}
}
