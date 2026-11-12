package registry

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestKeyOf_TrustsStoredKeyOverRecompute 钉住 keyOf 的第一条分支：条目已存非空
// Key 时必须原样返回，而不是按 path 重算。重算会把「非 git 时登记、之后 git init」
// 的陈旧 PathKey 覆盖成 git-key，成员资格判定就再也命中不了旧条目。
//
// 变异测试用：把 `e.Key != ""` 翻成 `==` 后，本用例返回重算 key 而失败。
func TestKeyOf_TrustsStoredKeyOverRecompute(t *testing.T) {
	useTempHome(t)
	dir := t.TempDir()
	gitInit(t, dir) // 让 forgedata.Key(dir) 走 git 分支，返回与存量 key 不同的值

	recomputed, err := forgedata.Key(dir)
	if err != nil {
		t.Fatalf("forgedata.Key: %v", err)
	}
	const stored = "stale-path-key"
	if recomputed == stored {
		t.Fatalf("前置条件不成立：重算 key 与存量 key 相同（%q），无法区分两条分支", stored)
	}

	if got := keyOf(Entry{Path: dir, Key: stored}); got != stored {
		t.Fatalf("keyOf 应信任已存 key %q，得重算值 %q", stored, got)
	}
}
