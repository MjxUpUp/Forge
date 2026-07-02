package skillscanonical

// resolve_test.go — Resolve 三条路径（env 命中 / env 路径不存在 / embed fallback）。
// EnsureEmbeddedCache 的纯函数契约由 cli 包覆盖（cli 用 FORGE_SKILLS_CANONICAL 走 env，
// 这里专测从 cli 下沉来的 Resolve 自身）。

import (
	"os"
	"path/filepath"
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
