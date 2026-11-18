package ghostpath

import (
	"os"
	"path/filepath"
	"runtime"
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
