package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestUncommittedChanges_IgnoresOnlyPhantomPaths 钉住三处接线的过滤语义：
// 只存在于 index、磁盘上不存在的保留名路径不算改动；真实存在的同名文件必须算。
func TestUncommittedChanges_IgnoresOnlyPhantomPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("保留名只在 Windows 上成立")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "real.go")
	gitIn(t, dir, "commit", "-qm", "initial")

	// 干净树：0 条改动。
	if n, ok := uncommittedChanges(dir); !ok || n != 0 {
		t.Fatalf("干净树应为 (0,true)，got (%d,%v)", n, ok)
	}
	if _, dirty := GitBranchDirty(dir); dirty {
		t.Fatal("干净树不该判脏")
	}

	// 把 nul/win.go 写进 index 再从工作区移除 → git 报 `AD nul/win.go`，磁盘上不存在。
	blob := exec.Command("git", "-C", dir, "hash-object", "-w", "--stdin")
	blob.Stdin = strings.NewReader("package nul\n")
	out, err := blob.Output()
	if err != nil {
		t.Fatalf("hash-object: %v", err)
	}
	gitIn(t, dir, "-c", "core.protectNTFS=false", "update-index", "--add", "--cacheinfo", "100644,"+strings.TrimSpace(string(out))+",nul/win.go")
	os.Remove(filepath.Join(dir, "nul", "win.go"))

	if n, ok := uncommittedChanges(dir); !ok || n != 0 {
		t.Fatalf("纯幻影路径应被忽略，got (%d,%v)", n, ok)
	}

	// 真实存在的保留名文件（\\?\ 前缀）是真改动，必须计入。
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(`\\?\`+filepath.Join(dir, "aux.go"), []byte("package aux\n"), 0o644); err != nil {
		t.Skipf("无法用 \\\\?\\ 前缀建真实保留名文件：%v", err)
	}
	if n, ok := uncommittedChanges(dir); !ok || n == 0 {
		t.Fatalf("真实存在的 aux.go 应计入改动，got (%d,%v)", n, ok)
	}
}
