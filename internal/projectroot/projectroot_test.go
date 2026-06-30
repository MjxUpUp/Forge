package projectroot

import (
	"os"
	"path/filepath"
	"testing"
)

// chdirTo 还原原 cwd（t.Cleanup），让 Find 的 os.Getwd 测试不污染其他测试。
// 不调 t.Parallel——os.Chdir 是进程全局，并行会互相踩。
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
}

// TestFind_LocatesForgeRoot：从深层子目录向上找到含 .forge/ 的根。
func TestFind_LocatesForgeRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	chdirTo(t, deep)
	got, err := Find()
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// 用 .forge 是否在 got 下做稳健断言（路径格式/符号链接差异不影响）。
	if _, err := os.Stat(filepath.Join(got, ".forge")); err != nil {
		t.Fatalf("Find 返回 %q，但其下无 .forge/: %v", got, err)
	}
}

// TestFind_AtProjectRoot：就在 .forge 同级也能找到（不越界）。
func TestFind_AtProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, root)
	got, err := Find()
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// macOS 上 /var 是 /private/var 的 symlink：os.Getwd 返回物理路径 /private/var/...，
	// 而 t.TempDir() 返回逻辑 /var/...，字符串比较会在 macOS 失败。用 samePath
	// (os.SameFile，inode 级) 比较——symlink/case/路径形式差异都不影响，且比字符串
	// 比较更严格（能识别同 inode 的不同路径写法）。Find 返回 canonical 物理路径本身正确。
	if !samePath(got, root) {
		t.Fatalf("Find()=%q want %q (same inode)", got, root)
	}
}

// TestFind_LocatesNearestNotFarthest：两个嵌套的 .forge/，Find 必须停在最*近*的，
// 不能贪心越过它找到更远的祖先。守护循环的"找到即返回"语义。
func TestFind_LocatesNearestNotFarthest(t *testing.T) {
	outer := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outer, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "sub")
	if err := os.MkdirAll(filepath.Join(inner, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, inner)
	got, err := Find()
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// samePath (os.SameFile) 比较——macOS /var↔/private/var 物理逻辑路径差异不影响。
	// 验证停在最近 inner：got 与 inner 同 inode，且不与 outer 同 inode（没越界到祖先）。
	if !samePath(got, inner) {
		t.Fatalf("Find 应停在最近的 .forge（%q），got %q（越过到更远祖先）", inner, got)
	}
	if samePath(got, outer) {
		t.Fatalf("Find 越界到 outer（%q），应停在 inner（%q）", outer, inner)
	}
}

// TestFind_NotInForgeProject：cwd 在 home 下但非任何项目 → 报错（不误判 home 为项目根）。
// 排除 home 后这条 not-found 路径才可测——此前 ~/.forge/ 在 t.TempDir() 祖先链里导致
// Find 必然命中 home，测试前提为假。t.TempDir() 是 home 的深层子目录（非 home 本身），
// 排除只作用于 home 那层，所以这里最终走到无 .forge 的根 → 报错。
func TestFind_NotInForgeProject(t *testing.T) {
	chdirTo(t, t.TempDir())
	if _, err := Find(); err == nil {
		t.Fatal("Find 应在非 forge 项目报错（排除 home 后），got nil")
	}
}

// TestIsProjectRoot_ExcludesHome：直接测 home 排除逻辑（不依赖 cwd/环境）。
// home 的 .forge/ 是全局状态目录（knowledge/hooks/skills），不是项目根——必须排除；
// home 子目录下的 .forge/ 是合法项目根——不能误杀。
func TestIsProjectRoot_ExcludesHome(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if isProjectRoot(home, home) {
		t.Error("home 的 .forge 应被排除（全局状态目录 ≠ 项目根）")
	}

	// home 子目录下的 .forge/ 仍是合法项目根（项目常就在 home 下，如 ~/projects/x）。
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isProjectRoot(proj, home) {
		t.Error("home 子目录的 .forge 是合法项目根，不应被排除")
	}

	// 无 .forge/ 的目录 → 非项目根。
	bare := t.TempDir()
	if isProjectRoot(bare, home) {
		t.Error("无 .forge/ 的目录不应判为项目根")
	}
}
