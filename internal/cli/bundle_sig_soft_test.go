package cli

// bundle_sig_soft_test.go —— 个人档软路径必须把根因带到 stderr：
// `signed=false, err=nil` 而原因消失会让「bundle 为什么没签名」无从排查。同时钉
// 死 trust store 不可读时明确告示、而非无声按个人档处理。

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBundleSigRespectingPolicy_SoftPathKeepsReason(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)

	// CORRUPT identity → writeBundleSig fails at LoadOrCreate; no trust store file
	// means personal profile (soft path).
	if err := os.WriteFile(filepath.Join(home, `node.json`), []byte(`{not json`), 0644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), `bundle.tar.gz`)
	if err := os.WriteFile(bundle, []byte(`payload`), 0644); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	sigPath, signed, serr := writeBundleSigRespectingPolicy(bundle)
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	if serr != nil || signed || sigPath != `` {
		t.Fatalf("personal-profile signing failure must be soft, got sigPath=%q signed=%v err=%v", sigPath, signed, serr)
	}
	if !strings.Contains(string(stderr), `签名失败`) || !strings.Contains(string(stderr), `parse node identity`) {
		t.Fatalf("soft path must keep the root cause on stderr, got: %q", string(stderr))
	}

	// Unreadable trust store: soft path, but the stderr line must SAY the store was
	// unreadable instead of claiming plain personal profile.
	if err := os.WriteFile(filepath.Join(home, `trust.json`), []byte(`{not json`), 0644); err != nil {
		t.Fatal(err)
	}
	r2, w2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w2
	_, signed, serr = writeBundleSigRespectingPolicy(bundle)
	os.Stderr = old
	if err := w2.Close(); err != nil {
		t.Fatal(err)
	}
	stderr2, err := io.ReadAll(r2)
	if err != nil {
		t.Fatal(err)
	}
	if serr != nil || signed {
		t.Fatalf("unreadable trust store must stay soft (cannot prove team mode), got signed=%v err=%v", signed, serr)
	}
	if !strings.Contains(string(stderr2), `trust store 不可读`) {
		t.Fatalf("unreadable trust store must be announced, got: %q", string(stderr2))
	}
}
