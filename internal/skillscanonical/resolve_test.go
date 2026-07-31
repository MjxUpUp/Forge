package skillscanonical

// resolve_test.go — three Resolve paths (env hit / env path missing / embed fallback).
// The pure-function contract of EnsureEmbeddedCache is covered by the cli package (cli uses
// FORGE_SKILLS_CANONICAL via env; this file tests Resolve itself, sunk out of cli).
//
// resolve_test.go — Resolve 三条路径（env 命中 / env 路径不存在 / embed fallback）。
// EnsureEmbeddedCache 的纯函数契约由 cli 包覆盖（cli 用 FORGE_SKILLS_CANONICAL 走 env，
// 这里专测从 cli 下沉来的 Resolve 自身）。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvName, dir)
	got, isExternal, err := Resolve("v1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != dir {
		t.Fatalf("dir=%q want %q", got, dir)
	}
	if !isExternal {
		t.Fatal("isExternal want true（env 覆盖）")
	}
}

func TestResolve_EnvNotFound(t *testing.T) {
	t.Setenv(EnvName, filepath.Join(t.TempDir(), "does-not-exist"))
	_, _, err := Resolve("v1")
	if err == nil {
		t.Fatal("env 指向不存在路径应报错")
	}
}

// TestResolve_EmbedFallback: no env → embed fallback, returns the cache dir + isExternal=false,
// and the cache has CONVENTIONS.md extracted (EnsureEmbeddedCache has done its work).
//
// TestResolve_EmbedFallback：无 env → embed fallback，返回缓存目录 + isExternal=false，
// 且缓存里 extract 出 CONVENTIONS.md（EnsureEmbeddedCache 已工作）。
func TestResolve_EmbedFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(EnvName, "") // 强制 embed fallback

	got, isExternal, err := Resolve("0.28.1")
	if err != nil {
		t.Fatalf("Resolve embed fallback: %v", err)
	}
	if isExternal {
		t.Fatal("isExternal want false（embed 缓存）")
	}
	if _, err := os.Stat(filepath.Join(got, "CONVENTIONS.md")); err != nil {
		t.Fatalf("embed 缓存缺 CONVENTIONS.md: %v", err)
	}
}

// TestEnsureEmbeddedCache_RemoveAllError: a failed RemoveAll must abort with an error —
// overwriting a half-deleted cache and stamping a fresh version marker would leave a
// mixed-version cache claiming to be a pure snapshot.
// Failure is made deterministic per-platform: Windows refuses to delete a directory
// containing an open file; Unix refuses to delete entries inside a directory without
// write permission.
//
// TestEnsureEmbeddedCache_RemoveAllError：RemoveAll 失败必须报错中止——在半删除的缓存上
// 覆盖写并打新版本标记，会得到谎称纯净的混合版本缓存。
// 失败按平台确定性构造：Windows 拒绝删除含打开文件的目录；Unix 拒绝删除无写权限目录里的条目。
func TestEnsureEmbeddedCache_RemoveAllError(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "embedded")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(cacheDir, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		defer f.Close() // 保持句柄打开：Windows 下 RemoveAll 含打开文件的目录必失败
	} else {
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(cacheDir, 0500); err != nil { // 无写权限：Unix 下删除其内容必失败
			t.Fatal(err)
		}
		defer os.Chmod(cacheDir, 0700) //nolint:errcheck // 清理，便于 TempDir 回收
	}
	if err := EnsureEmbeddedCache(cacheDir, "v9.9.9"); err == nil {
		t.Fatal("RemoveAll 失败应返回 error（旧实现 `_ = os.RemoveAll` 吞错后带病覆盖写），got nil")
	}
}
