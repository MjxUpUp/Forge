package nodestamp

// nodestamp_degrade_test.go —— fail-open 的可发现性半边：打戳禁用（身份或计数器
// 损坏）时，降级必须每进程至少一次到达 stderr。静默降级与「没打过戳」无法区分，
// 之后每条事件的机器归因都无声丢失、无任何痕迹。

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/nodeid"
)

// captureStderr runs fn with os.Stderr replaced by a pipe and returns what fn wrote.
func captureStderr(t *testing.T, fn func()) string {
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

func TestNext_DegradeIsAnnouncedOnStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	resetForTest()

	// Valid identity (created on demand), CORRUPT counter → stamping disables.
	if err := os.WriteFile(filepath.Join(home, `node-seq`), []byte(`not-a-number`), 0644); err != nil {
		t.Fatal(err)
	}

	var stamp Stamp
	var stderr string
	stderr = captureStderr(t, func() {
		stamp = Next()
	})
	if stamp != (Stamp{}) {
		t.Fatalf("corrupt node-seq must still fail open (zero stamp), got %+v", stamp)
	}
	if !strings.Contains(stderr, `打戳已禁用`) {
		t.Fatalf("degrade must be announced on stderr once, got: %q", stderr)
	}

	// One line per process — the second degraded event must not re-warn.
	stderr = captureStderr(t, func() {
		stamp = Next()
	})
	if stamp != (Stamp{}) {
		t.Fatalf("corrupt node-seq must still fail open, got %+v", stamp)
	}
	if strings.Contains(stderr, `打戳已禁用`) {
		t.Fatalf("degrade warning must fire once per process, saw it again: %q", stderr)
	}
}

// TestNext_IdentityCreatedByNonStampingPathStillStamps pins the STANDARD machine
// bring-up order: `forge task start` (lease claim), `node show`, bundle signing
// or sync materialize the identity BEFORE any hook stamps.
//
// TestNext_IdentityCreatedByNonStampingPathStillStamps 钉死标准装机顺序：
// `forge task start`（租约认领）、`node show`、bundle 签名、sync 会在任何 hook
// 打戳之前物化身份——首个打戳必须正常工作（计数器在身份诞生时已播种），且无重
// 启告示。回归形态（fix/dsh-review-followup 自审发现）：进程级「身份已存在 ⇒
// 计数器被删」的启发式恰好在每台新机器的这条路径上降级。
func TestNext_IdentityCreatedByNonStampingPathStillStamps(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	resetForTest()

	// The lease-claim path: identity created OUTSIDE stamping.
	if _, err := nodeid.LoadOrCreate(); err != nil {
		t.Fatal(err)
	}

	var stamp Stamp
	stderr := captureStderr(t, func() {
		stamp = Next()
	})
	if stamp.NodeID == `` || stamp.Seq != 1 {
		t.Fatalf("first stamp after non-stamping identity creation must work, got %+v", stamp)
	}
	if strings.Contains(stderr, `重新起算`) {
		t.Fatalf("seeded counter must not trigger the restart notice, got: %q", stderr)
	}
}

// TestNext_MissingCounterRestartsFrom1WithNotice: a counter lost on a machine whose
// identity already exists restarts FROM 1 with a one-time notice — announced, not
// blocking (stamping never blocks events; the hard disable is deferred until a
// (node_id, seq) consumer exists, per the node-identity §4 实现校正).
//
// TestNext_MissingCounterRestartsFrom1WithNotice：身份已存在的机器丢计数器后从 1
// 重启并一次性告示——告示而不阻断（打戳绝不阻塞事件；硬禁用按 node-identity §4
// 实现校正推迟到 (node_id, seq) 消费方出现）。
func TestNext_MissingCounterRestartsFrom1WithNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	resetForTest()

	// Stamp once (identity + counter live), then lose the counter.
	if s := Next(); s.NodeID == `` || s.Seq != 1 {
		t.Fatalf("fresh machine must stamp (seq 1), got %+v", s)
	}
	if err := os.Remove(filepath.Join(home, `node-seq`)); err != nil {
		t.Fatal(err)
	}
	resetForTest()

	var stamp Stamp
	stderr := captureStderr(t, func() {
		stamp = Next()
	})
	if stamp.NodeID == `` || stamp.Seq != 1 {
		t.Fatalf("restart must continue stamping from 1 (fail-open), got %+v", stamp)
	}
	if !strings.Contains(stderr, `重新起算`) || !strings.Contains(stderr, `node-seq`) {
		t.Fatalf("restart must be announced once with the reuse risk, got: %q", stderr)
	}
	stderr = captureStderr(t, func() {
		_ = Next()
	})
	if strings.Contains(stderr, `重新起算`) {
		t.Fatalf("restart notice must fire once per process, saw it again: %q", stderr)
	}
}

func TestNext_IdentityFailureIsAnnouncedOnStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	resetForTest()

	// CORRUPT identity JSON (present but unparseable) → identity load fails.
	if err := os.WriteFile(filepath.Join(home, `node.json`), []byte(`{not json`), 0644); err != nil {
		t.Fatal(err)
	}

	var stamp Stamp
	stderr := captureStderr(t, func() {
		stamp = Next()
	})
	if stamp != (Stamp{}) {
		t.Fatalf("broken identity must still fail open, got %+v", stamp)
	}
	if !strings.Contains(stderr, `打戳已禁用`) {
		t.Fatalf("identity degrade must be announced on stderr, got: %q", stderr)
	}
}
