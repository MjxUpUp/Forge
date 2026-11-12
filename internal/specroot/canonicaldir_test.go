package specroot

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCanonicalDir_ResolvesLinkedAncestorAboveMissingLeaf 钉住 canonicalDir 的逐级
// 上溯：叶子不存在时，也要解析最深已存在的祖先（symlink / Windows junction），再把余
// 下路径拼回真实前缀。
//
// 变异测试用：把循环里的 `parent == cur` 翻成 `!=` 后，函数第一轮就原样返回入参，
// 结果保留链接路径，本用例失败。宿主不支持可解析的链接时跳过（例如无 symlink 权限、
// 卷未启用 8.3 短名的 Windows）。
func TestCanonicalDir_ResolvesLinkedAncestorAboveMissingLeaf(t *testing.T) {
	real := t.TempDir()
	if err := os.MkdirAll(filepath.Join(real, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "alias")
	makeLinkOrSkip(t, link, real)

	// 前置条件：本机的链接必须真的可被 EvalSymlinks 解析，否则这条用例既杀不了
	// 变异体、也会把环境问题误报成回归。
	probe, err := filepath.EvalSymlinks(filepath.Join(link, "deep"))
	if err != nil {
		t.Skipf("链接不可解析（%v），跳过", err)
	}
	if samePathString(probe, filepath.Join(link, "deep")) {
		t.Skipf("EvalSymlinks 未解析链接祖先（%q），跳过", probe)
	}

	realResolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", real, err)
	}
	got, err := canonicalDir(filepath.Join(link, "deep", "missing"))
	if err != nil {
		t.Fatalf("canonicalDir: %v", err)
	}
	want := filepath.Join(realResolved, "deep", "missing")
	if !samePathString(got, want) {
		t.Fatalf("canonicalDir=%q, want %q（未解析链接祖先，直接返回了入参）", got, want)
	}
	if samePathString(got, filepath.Join(link, "deep", "missing")) {
		t.Fatalf("canonicalDir 保留了链接路径 %q", got)
	}
}

// makeLinkOrSkip 建目录链接：Unix 用 symlink；Windows 用 junction（mklink /J 不需要
// 管理员权限）。建不出来就跳过（例如禁用重解析点的环境）。
func makeLinkOrSkip(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Skipf("mklink /J: %v\n%s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
}

// samePathString 比较两个路径：Windows 大小写不敏感，其余平台逐字节。
func samePathString(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
