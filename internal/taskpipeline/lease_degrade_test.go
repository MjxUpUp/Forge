package taskpipeline

// lease_degrade_test.go — the discoverability half of lease fail-open: a broken node
// identity must not silently read as "no lease / no foreign lease". The claim and
// status paths each announce the degradation once per process on stderr.
//
// lease_degrade_test.go —— 租约 fail-open 的可发现性半边：损坏的节点身份不得静默
// 读作「无租约 / 无他机租约」。认领与判定两条路径都须每进程一次在 stderr 告知降级。

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureLeaseStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestLease_FailOpenIsAnnouncedOnStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	leaseDegradeNoted = false

	// CORRUPT identity JSON (present but unparseable) → LoadOrCreate fails.
	if err := os.WriteFile(filepath.Join(home, `node.json`), []byte(`{not json`), 0644); err != nil {
		t.Fatal(err)
	}

	s := &TaskState{TaskRef: `feat/x`}

	// Claim path: still fails open (no lease), but says so.
	var stderr string
	stderr = captureLeaseStderr(t, func() {
		ClaimLeaseForCurrentNode(s)
	})
	if s.Lease != nil {
		t.Fatalf("identity failure must not block task work (no lease), got %+v", s.Lease)
	}
	if !strings.Contains(stderr, `跳过租约`) {
		t.Fatalf("claim degrade must be announced on stderr, got: %q", stderr)
	}

	// Status path with a FOREIGN ACTIVE lease present: identity failure must read as
	// "no foreign lease" (fail-open) — and say so, once. The lease is genuinely
	// ACTIVE (claimed a minute ago): a healthy identity would report ForeignActive,
	// so the false verdict below is the fail-open behavior, not an expired lease.
	//
	// 状态路径带一条他机活跃租约：身份失败必须读作「无他机租约」（fail-open）——
	// 且告知一次。租约真实活跃（一分钟前认领）：健康身份会报 ForeignActive，因此
	// 下面的假判定才是 fail-open 行为，而非租约恰好过期。
	s.Lease = &Lease{HolderNode: `fnode_00000000000000000000000000000000`, TTLSec: 3600,
		ClaimedAt: time.Now().Add(-time.Minute).UnixMilli()}
	leaseDegradeNoted = false
	var st LeaseState
	stderr = captureLeaseStderr(t, func() {
		st = LeaseStatusForCurrentNode(s)
	})
	if st.ForeignActive {
		t.Fatalf("identity failure must read as no-foreign-lease (fail-open), got %+v", st)
	}
	if !strings.Contains(stderr, `跳过租约`) {
		t.Fatalf("status degrade must be announced on stderr, got: %q", stderr)
	}

	// One line per process per process-lifetime: the next degraded call stays quiet.
	leaseDegradeNoted = false
	stderr = captureLeaseStderr(t, func() {
		ClaimLeaseForCurrentNode(s)
	})
	if !strings.Contains(stderr, `跳过租约`) {
		t.Fatalf("first call in a fresh process must warn, got: %q", stderr)
	}
	stderr = captureLeaseStderr(t, func() {
		ClaimLeaseForCurrentNode(s)
	})
	if strings.Contains(stderr, `跳过租约`) {
		t.Fatalf("degrade must warn once per process, saw it again: %q", stderr)
	}
}
