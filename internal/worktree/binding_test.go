package worktree

import (
	"path/filepath"
	"testing"
)

func TestIDStableAndDistinct(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if ID(a) == ID(b) {
		t.Fatal("不同路径 wtid 必须不同")
	}
	if ID(a) != ID(filepath.Join(a, "sub", "..")) {
		t.Fatal("同一路径的变体拼写 wtid 必须一致（EvalSymlinks+Clean 归一）")
	}
}
