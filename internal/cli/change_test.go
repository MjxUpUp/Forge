package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/change"
)

func TestChangeRoot_FallsBackToGitRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	got, err := changeRoot()
	if err != nil {
		t.Fatalf("changeRoot: %v", err)
	}
	if !samePath(got, root) {
		t.Errorf("changeRoot=%s，期望 git 根 %s", got, root)
	}
}

func TestChangeRoot_PlainDirUsesCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := changeRoot()
	if err != nil {
		t.Fatalf("changeRoot: %v", err)
	}
	if !samePath(got, dir) {
		t.Errorf("changeRoot=%s，期望 cwd %s", got, dir)
	}
}

func TestChangeFirstLine(t *testing.T) {
	cases := map[string]string{
		"go build ./...":             "go build ./...",
		"  go test ./internal/...  ": "go test ./internal/...",
		"cd sub\nmake test":          "cd sub …",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestChangeDur(t *testing.T) {
	if got := dur(420); got != "420ms" {
		t.Errorf("dur(420)=%q", got)
	}
	if got := dur(2500); got != "2.5s" {
		t.Errorf("dur(2500)=%q", got)
	}
}

func TestChangeEvidenceCell(t *testing.T) {
	if got := evidenceCell(change.Change{}); got != "-" {
		t.Errorf("无证据应显示 -，got %q", got)
	}
	if got := evidenceCell(change.Change{EvidenceCount: 2, Sealed: true}); !strings.Contains(got, "2") || !strings.Contains(got, "✓") {
		t.Errorf("有封印证据应带计数与勾，got %q", got)
	}
	if got := evidenceCell(change.Change{EvidenceCount: 3}); got != "3" {
		t.Errorf("未封印证据只显示计数，got %q", got)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// samePath 比较两个路径，容忍 Windows 短路径（ADMINI~1）与大小写差异。
func samePath(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	if strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb)) {
		return true
	}
	ra, erra := filepath.EvalSymlinks(aa)
	rb, errb := filepath.EvalSymlinks(bb)
	return erra == nil && errb == nil && strings.EqualFold(ra, rb)
}
