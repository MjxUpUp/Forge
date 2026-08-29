package projectroot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdirTo 还原原 cwd（t.Cleanup），让 Find 的 os.Getwd 测试不污染其他测试。
// 不调 t.Parallel——os.Chdir 是进程全局，并行会互相踩。

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

// TestLegacyFind_Boundaries：遗留 walk-up 必须 (a) 到有效用户 home 即停（那里的
// ~/.forge 是全局 store，不是项目），(b) 跳过内含 projects.json 的 .forge/
// （内容嗅探出的全局 store——覆盖 HOME 被覆盖、有效 home 比较失效的环境），
// (c) 仍接受 home 下合法的项目 .forge/。
func TestLegacyFind_Boundaries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	// (a) home 自身带 .forge（全局 store）→ 边界，不命中。
	if err := os.MkdirAll(filepath.Join(home, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := legacyFind(home); ok {
		t.Error("home 的 .forge 应被边界挡住（全局状态目录 ≠ 项目根）")
	}

	// (c) home 子目录下的 .forge/ 是合法项目根。
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := legacyFind(proj)
	if !ok || !samePath(got, proj) {
		t.Errorf("home 子目录的 .forge 是合法项目根，got %q ok=%v", got, ok)
	}

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

// TestIsGlobalForgeStore 钉死内容嗅探：每个全局 store 标记（projects.json /
// projects/ / skills-cache/ / skills-manifest.json / backups/ /
// .init-suggested/）都必须被识别——包括只有 projects/ 还没有 projects.json
// 的 store（CI runner home 案例）；普通项目 .forge/（hooks/、protocol.yml）
// 绝不误判。
func TestIsGlobalForgeStore(t *testing.T) {
	for _, marker := range globalStoreMarkers {
		dir := t.TempDir()
		forge := filepath.Join(dir, ".forge")
		target := filepath.Join(forge, marker)
		if strings.HasSuffix(marker, ".json") {
			if err := os.MkdirAll(forge, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if !isGlobalForgeStore(forge) {
			t.Errorf("含标记 %q 的 .forge 应判为全局 store", marker)
		}
	}

	// 项目 .forge/（hooks/、protocol.yml、team-mode）不是全局 store。
	proj := t.TempDir()
	forge := filepath.Join(proj, ".forge")
	if err := os.MkdirAll(filepath.Join(forge, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(forge, "protocol.yml"), []byte("version: 1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isGlobalForgeStore(forge) {
		t.Error("项目 .forge/ 不应判为全局 store")
	}
}
