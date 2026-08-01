package projectroot

import (
	"os"
	"path/filepath"
	"testing"
)

// chdirTo restores the original cwd (via t.Cleanup) so Find's os.Getwd tests do not pollute other tests.
// It does not call t.Parallel — os.Chdir is process-global and parallel runs would stomp each other.
//
// chdirTo 还原原 cwd（t.Cleanup），让 Find 的 os.Getwd 测试不污染其他测试。
// 不调 t.Parallel——os.Chdir 是进程全局，并行会互相踩。

// TestMain isolates the global forge home: Find self-heals by registering legacy
// .forge/ hits into the registry (~/.forge/projects.json) — without isolation,
// in-process tests would write the REAL registry and register the repo itself.
//
// TestMain 隔离全局 forge home：Find 会把遗留 .forge/ 命中自愈登记进注册表
// （~/.forge/projects.json）——不隔离的话，进程内测试会写真实注册表并登记
// 仓库自身。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "forge-projectroot-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("FORGE_DATA_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

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

// TestFind_LocatesForgeRoot: walks up from a deep subdir to find the root containing .forge/.
//
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
	// Robust assertion via checking whether .forge lives under got (path format / symlink differences do not affect it).
	//
	// 用 .forge 是否在 got 下做稳健断言（路径格式/符号链接差异不影响）。
	if _, err := os.Stat(filepath.Join(got, ".forge")); err != nil {
		t.Fatalf("Find 返回 %q，但其下无 .forge/: %v", got, err)
	}
}

// TestFind_AtProjectRoot: at the same level as .forge it is still found (does not overshoot).
//
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
	// On macOS /var is a symlink of /private/var: os.Getwd returns the physical path /private/var/...,
	// while t.TempDir() returns the logical /var/..., so string comparison would fail on macOS. Using samePath
	// (os.SameFile, inode-level) comparison — symlink/case/path-form differences do not affect it, and it is stricter
	// than string comparison (it can identify different path spellings of the same inode). Find returning the canonical physical path is itself correct.
	//
	// macOS 上 /var 是 /private/var 的 symlink：os.Getwd 返回物理路径 /private/var/...，
	// 而 t.TempDir() 返回逻辑 /var/...，字符串比较会在 macOS 失败。用 samePath
	// (os.SameFile，inode 级) 比较——symlink/case/路径形式差异都不影响，且比字符串
	// 比较更严格（能识别同 inode 的不同路径写法）。Find 返回 canonical 物理路径本身正确。
	if !samePath(got, root) {
		t.Fatalf("Find()=%q want %q (same inode)", got, root)
	}
}

// TestFind_LocatesNearestNotFarthest: with two nested .forge/ dirs, Find must stop at the *nearest* one,
// not greedily skip it to find a farther ancestor. Guards the find-and-return-immediately semantics of the loop.
//
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
	// Same-path (os.SameFile) comparison — the macOS /var vs /private/var physical/logical path difference does not affect it.
	// Verify it stops at the nearest inner: got shares the inode with inner, and does NOT share the inode with outer (did not overshoot to the ancestor).
	//
	// samePath (os.SameFile) 比较——macOS /var↔/private/var 物理逻辑路径差异不影响。
	// 验证停在最近 inner：got 与 inner 同 inode，且不与 outer 同 inode（没越界到祖先）。
	if !samePath(got, inner) {
		t.Fatalf("Find 应停在最近的 .forge（%q），got %q（越过到更远祖先）", inner, got)
	}
	if samePath(got, outer) {
		t.Fatalf("Find 越界到 outer（%q），应停在 inner（%q）", outer, inner)
	}
}

// TestFind_NotInForgeProject: cwd is under home but not in any project → error (must not misidentify home as a project root).
// Only after excluding home can this not-found path be tested — previously ~/.forge/ sat in t.TempDir()'s ancestor chain so
// Find always hit home, making the test premise false. t.TempDir() is a deep subdir of home (not home itself),
// and the exclusion applies only to the home layer, so we eventually walk to a root with no .forge → error.
//
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

// TestLegacyFind_Boundaries: the legacy walk-up must (a) stop at the effective user
// home (~/.forge there is the global store, not a project), (b) skip any .forge/
// containing projects.json (the global store, content-sniffed — covers HOME-overridden
// environments where the effective-home comparison no longer helps), and (c) still
// accept a legitimate project .forge/ under home.
//
// TestLegacyFind_Boundaries：遗留 walk-up 必须 (a) 到有效用户 home 即停（那里的
// ~/.forge 是全局 store，不是项目），(b) 跳过内含 projects.json 的 .forge/
// （内容嗅探出的全局 store——覆盖 HOME 被覆盖、有效 home 比较失效的环境），
// (c) 仍接受 home 下合法的项目 .forge/。
func TestLegacyFind_Boundaries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	// (a) home itself carries .forge (the global store) → boundary, no match.
	//
	// (a) home 自身带 .forge（全局 store）→ 边界，不命中。
	if err := os.MkdirAll(filepath.Join(home, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := legacyFind(home); ok {
		t.Error("home 的 .forge 应被边界挡住（全局状态目录 ≠ 项目根）")
	}

	// (c) .forge/ under a home subdir is a legitimate project root.
	//
	// (c) home 子目录下的 .forge/ 是合法项目根。
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := legacyFind(proj)
	if !ok || !samePath(got, proj) {
		t.Errorf("home 子目录的 .forge 是合法项目根，got %q ok=%v", got, ok)
	}

	// (b) a .forge/ containing projects.json is the global store → rejected even
	// when it is NOT the effective home (HOME-overridden trap).
	//
	// (b) 内含 projects.json 的 .forge/ 是全局 store——即使不是有效 home 也拒判
	// （HOME 被覆盖的陷阱）。
	store := t.TempDir()
	if err := os.MkdirAll(filepath.Join(store, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, ".forge", "projects.json"), []byte(`{"projects":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(store, "sub", "dir")
	if err := os.MkdirAll(under, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := legacyFind(under); ok {
		t.Error("内含 projects.json 的 .forge 应判为全局 store，不应命中")
	}
}
