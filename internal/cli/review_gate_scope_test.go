package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/spf13/cobra"
)

func initGateRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // 隔离 DataDir（含归属台账与 stamps）
	t.Setenv("FORGE_SESSION_ID", "sess-a")   // 本会话身份（无宿主身份时的注入通道）
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	for _, p := range []string{".gitkeep", ".forge/.keep"} {
		path := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	t.Chdir(dir)
	return dir
}

// TestReviewGate_ForeignChangeIsAdvisory 是 2026-10-24 事故的端到端回归：共享检出里
// 只有别的会话的源码变更时，`forge review gate` 必须出 ADVISORY 放行，不得 FAIL——
// 否则无关会话被迫去审别人的代码才能结束（A 会话 61% 事件被此事吃掉）。
func TestReviewGate_ForeignChangeIsAdvisory(t *testing.T) {
	root := initGateRepo(t)
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	attribution.Record(root, attribution.Event{Ts: time.Now(), Sid: "sess-b", Kind: attribution.KindWrite, Path: "b.go"})

	var err error
	out := captureStdout(t, func() { err = runReviewGate(&cobra.Command{}, nil) })
	if err != nil {
		t.Fatalf("gate 返回错误：%v（输出：%s）", err, out)
	}
	if !strings.Contains(out, "ADVISORY") {
		t.Fatalf("他会话变更应 ADVISORY 放行，得 %q", out)
	}
	if strings.Contains(out, "FAIL") {
		t.Fatalf("他会话变更不得 FAIL 阻断，得 %q", out)
	}
	if !strings.Contains(out, "其他活跃会话") {
		t.Errorf("放行理由应点名其他活跃会话，得 %q", out)
	}
}
