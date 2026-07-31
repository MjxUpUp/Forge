package util

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDirEntryIsDir: DirEntryIsDir must follow junction/symlink (os.Stat semantics) —
// e.IsDir() is Lstat-based and drops link-form entries.
//
// TestDirEntryIsDir：DirEntryIsDir 必须跟随 junction/symlink（os.Stat 语义）——
// e.IsDir() 基于 Lstat，会漏掉 link 形态的条目。
func TestDirEntryIsDir(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, "real-dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "a-file"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	realTarget := t.TempDir()
	if err := os.Symlink(realTarget, filepath.Join(parent, "linked-dir")); err != nil {
		t.Skipf("symlinks unavailable on host（Windows 可能需开发者模式）: %v", err)
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = DirEntryIsDir(parent, e)
	}
	if !got["real-dir"] {
		t.Error("real-dir 应判 true")
	}
	if got["a-file"] {
		t.Error("a-file 应判 false")
	}
	if !got["linked-dir"] {
		t.Error("linked-dir（symlink → 真实目录）应判 true（e.IsDir 基于 Lstat 会漏）")
	}
}

// TestDirEntryIsDir_BrokenLink: a dangling symlink stats to nothing → false (safe skip).
//
// TestDirEntryIsDir_BrokenLink：断链 stat 不到目标 → false（安全跳过）。
func TestDirEntryIsDir_BrokenLink(t *testing.T) {
	parent := t.TempDir()
	if err := os.Symlink(filepath.Join(parent, "nonexistent-target"), filepath.Join(parent, "broken")); err != nil {
		t.Skipf("symlinks unavailable on host（Windows 可能需开发者模式）: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if DirEntryIsDir(parent, e) {
			t.Errorf("broken link %s 应判 false", e.Name())
		}
	}
}
