package util

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCompareVersions_SemverTieBreak 钉住 semver 家族的核心契约（自 cli
// update_test.go 的用例精选 + §11 tie-break 专例——家族自 update.go 下沉，
// cli 侧经薄委托消费同一实现）。
func TestCompareVersions_SemverTieBreak(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.11.1", "0.12.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "0.12.0", 1},
		// semver §11：数字核心相等时正式版高于 pre-release。
		{"0.12.0", "0.12.0-beta.1", 1},
		{"0.12.0-beta.1", "0.12.0", -1},
		// pre-release 分段：数字段按数值、数字段 < 字母段。
		{"0.12.0-beta.2", "0.12.0-beta.1", 1},
		{"0.12.0-beta.1", "0.12.0-alpha", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestGetCurrentVersion 钉住完整版本串的裸版本提取。
func TestGetCurrentVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"dev", "dev"},
		{"0.11.1", "0.11.1"},
		{"2.0.0-beta.1 (commit: deadbeef, built: 2026-06-11)", "2.0.0-beta.1"},
	}
	for _, c := range cases {
		if got := GetCurrentVersion(c.in); got != c.want {
			t.Errorf("GetCurrentVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPackageLeaf pins the dependency direction: util must never depend on
// cli/hookdispatch — it is the leaf both sides consume (2026-09 census A2-2).
//
// TestPackageLeaf 钉住依赖方向：util 永不依赖 cli/hookdispatch——它是两侧
// 共同消费的叶子包（2026-09 普查 A2-2）。
func TestPackageLeaf(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable in test environment: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasSuffix(dep, "/internal/cli") || strings.HasSuffix(dep, "/internal/hookdispatch") {
			t.Errorf("util must not depend on %s", dep)
		}
	}
}
