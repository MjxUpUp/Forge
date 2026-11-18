package ghostpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPorcelainPaths(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"?? nul/win.go", []string{"nul/win.go"}},
		{" M internal/cli/docs.go", []string{"internal/cli/docs.go"}},
		{"R  old/aux.go -> new/aux.go", []string{"old/aux.go", "new/aux.go"}},
		{"?? \"a b/c.md\"", []string{"a b/c.md"}},
		{"", nil},
		{"??", nil},
	}
	for _, c := range cases {
		got := porcelainPaths(c.line)
		if len(got) != len(c.want) {
			t.Fatalf("porcelainPaths(%q) = %v, want %v", c.line, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("porcelainPaths(%q) = %v, want %v", c.line, got, c.want)
			}
		}
	}
}

// TestLineHasReservedPath 只在 Windows 上断言 true：IsReserved 按平台生效，
// Linux/macOS 上 `aux.go` 是正常文件，工作区状态必须照实算。
func TestLineHasReservedPath(t *testing.T) {
	root := t.TempDir()
	if runtime.GOOS != "windows" {
		if LineHasReservedPath(root, "?? nul/win.go") {
			t.Fatal("非 Windows 上不应把 nul/win.go 判成保留名路径")
		}
		return
	}
	for _, line := range []string{"?? nul/win.go", "?? internal/aux/helpers.go", " M con.txt", "R  a.go -> lpt1/x.go"} {
		if !LineHasReservedPath(root, line) {
			t.Fatalf("LineHasReservedPath(%q) = false, want true", line)
		}
	}
	for _, line := range []string{" M internal/cli/docs.go", "?? README.md", "?? auxiliary/helpers.go"} {
		if LineHasReservedPath(root, line) {
			t.Fatalf("LineHasReservedPath(%q) = true, want false", line)
		}
	}
}

// TestLineHasReservedPath_KeepsExistingFile 钉住假阴性：磁盘上真实存在的保留名
// 文件（Windows 用 \\?\ 前缀可建）是真改动，不能整行跳过。
func TestLineHasReservedPath_KeepsExistingFile(t *testing.T) {
	root := t.TempDir()
	if runtime.GOOS != "windows" {
		t.Skip("保留名只在 Windows 上成立")
	}
	if err := os.WriteFile(`\\?\`+filepath.Join(root, "aux.go"), []byte("real"), 0o644); err != nil {
		t.Skipf("无法用 \\\\?\\ 前缀建真实保留名文件：%v", err)
	}
	if LineHasReservedPath(root, " M aux.go") {
		t.Fatal("真实存在的 aux.go 被当成幻影：会让工作区误判为干净")
	}
	if IsPhantomPath(root, "aux.go") {
		t.Fatal("真实存在的 aux.go 不该是幻影路径")
	}
	if !IsPhantomPath(root, "nul/win.go") {
		t.Fatal("磁盘上不存在的 nul/win.go 应当是幻影路径")
	}
}

func TestFilterReserved(t *testing.T) {
	root := t.TempDir()
	in := []string{"a.go", "b.md", "a.go"}
	got := FilterReserved(root, in)
	if len(got) != len(in) {
		t.Fatalf("FilterReserved 在非保留名上必须原样返回，got %v", got)
	}
	if got[0] != "a.go" || got[len(got)-1] != "a.go" {
		t.Fatalf("FilterReserved 必须保持顺序，got %v", got)
	}
}

// gitReservedFile 在 root 里用 \\?\ 前缀建一个真实的保留名文件并提交进 HEAD。
// 返回提交后的仓库；失败时测试自己 skip（本机文件系统可能不允许）。
func gitReservedFile(t *testing.T, root, name string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("保留名只在 Windows 上成立")
	}
	if err := os.WriteFile(`\\?\`+filepath.Join(root, name), []byte("real\n"), 0o644); err != nil {
		t.Skipf("无法用 \\\\?\\ 前缀建真实保留名文件：%v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "core.protectNTFS=false"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v 失败（本机文件系统不接受保留名）：%v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "--", name)
	run("commit", "-qm", "add "+name)
}

// TestIsPhantomPath_DeletedTrackedFileIsNotPhantom 钉住第二形态的假阴性：
// 删掉一个已提交的真实保留名文件同样是真改动，不能被当成幻影吞掉。
func TestIsPhantomPath_DeletedTrackedFileIsNotPhantom(t *testing.T) {
	root := t.TempDir()
	gitReservedFile(t, root, "aux.go")
	if err := os.Remove(`\\?\` + filepath.Join(root, "aux.go")); err != nil {
		t.Skipf("删不掉已提交的保留名文件：%v", err)
	}
	if IsPhantomPath(root, "aux.go") {
		t.Fatal("HEAD 里仍被跟踪的 aux.go 删除后是真改动，不该判幻影")
	}
	if LineHasReservedPath(root, " D aux.go") {
		t.Fatal("删除已提交的 aux.go 被整行跳过：工作区会误判为干净")
	}
}

// TestFilterReserved_DropsOnlyPhantom 钉住过滤函数本体（不是只验「原样返回」）。
func TestFilterReserved_DropsOnlyPhantom(t *testing.T) {
	root := t.TempDir()
	if runtime.GOOS != "windows" {
		t.Skip("保留名只在 Windows 上成立")
	}
	got := FilterReserved(root, []string{"nul/win.go", "a.go", "con.txt"})
	if strings.Join(got, ",") != "a.go" {
		t.Fatalf("FilterReserved 必须只剔掉幻影路径，got %v", got)
	}
}
