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
