package ghostpath

import (
	"runtime"
	"testing"
)

func TestHasReservedSegment(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"nul/win.go", true},
		{"aux.go", true},
		{"CON.txt", true},
		{"internal/aux/helpers.go", true},
		{"com1/main.go", true},
		{"LPT9.log", true},
		{"conin$/x.go", true},
		{"nul /win.go", true},
		{"internal/cli/main.go", false},
		{"auxiliary.go", false},
		{"console.go", false},
		{"nulx.go", false},
		{"com10/x.go", false},
		{"../nul/win.go", true},
	}
	for _, c := range cases {
		if got := HasReservedSegment(c.path); got != c.want {
			t.Errorf("HasReservedSegment(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestIsReservedOnlyOnWindows(t *testing.T) {
	// IsReserved 带平台判断：非 Windows 上同名文件是正常源码，不能被当成幻影过滤。
	got := IsReserved("aux.go")
	if runtime.GOOS == "windows" {
		if !got {
			t.Fatal("Windows 上 aux.go 应判为保留名")
		}
		return
	}
	if got {
		t.Fatal("非 Windows 上 aux.go 不是保留名——过滤它会漏掉真实源码变更")
	}
}
