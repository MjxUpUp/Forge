package ghostpath

import (
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
	if runtime.GOOS != "windows" {
		if LineHasReservedPath("?? nul/win.go") {
			t.Fatal("非 Windows 上不应把 nul/win.go 判成保留名路径")
		}
		return
	}
	for _, line := range []string{"?? nul/win.go", "?? internal/aux/helpers.go", " M con.txt", "R  a.go -> lpt1/x.go"} {
		if !LineHasReservedPath(line) {
			t.Fatalf("LineHasReservedPath(%q) = false, want true", line)
		}
	}
	// 注意：整段名为 aux 的目录在 Windows 上同样建不出来，属于保留名（IsReserved 按段判定）。
	for _, line := range []string{" M internal/cli/docs.go", "?? README.md", "?? auxiliary/helpers.go"} {
		if LineHasReservedPath(line) {
			t.Fatalf("LineHasReservedPath(%q) = true, want false", line)
		}
	}
}

func TestFilterReserved(t *testing.T) {
	in := []string{"a.go", "b.md", "a.go"}
	got := FilterReserved(in)
	if len(got) != len(in) {
		t.Fatalf("FilterReserved 在非保留名上必须原样返回，got %v", got)
	}
	if len(in) == 0 || got[0] != "a.go" || got[len(got)-1] != "a.go" {
		t.Fatalf("FilterReserved 必须保持顺序，got %v", got)
	}
}
