package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
)

// TestEnsureEmbeddedCache_ExtractsWhenMissing 首次：缓存不存在 → extract + 写版本标记。
func TestEnsureEmbeddedCache_ExtractsWhenMissing(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "embedded")
	if err := skillscanonical.EnsureEmbeddedCache(cache, "0.26.3"); err != nil {
		t.Fatalf("ensureEmbeddedCache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "CONVENTIONS.md")); err != nil {
		t.Fatal("CONVENTIONS.md 未 extract")
	}
	got, err := os.ReadFile(filepath.Join(cache, skillscanonical.VersionFile))
	if err != nil {
		t.Fatal("版本标记 .embedded-version 未写")
	}
	if string(got) != "0.26.3" {
		t.Fatalf("版本标记 = %q, want 0.26.3", string(got))
	}
}

// TestEnsureEmbeddedCache_ReextractsOnVersionChange 升级：版本标记不一致 → re-extract 并更新标记。
// 这是核心修复——旧逻辑此处不刷新，导致走 embedded fallback 的用户升级后拿不到新 skill 库。
func TestEnsureEmbeddedCache_ReextractsOnVersionChange(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "embedded")
	if err := skillscanonical.EnsureEmbeddedCache(cache, "0.26.2"); err != nil {
		t.Fatalf("首次 extract v0.26.2: %v", err)
	}
	// 升级到 0.26.3：版本标记不一致 → 必须重新 extract
	if err := skillscanonical.EnsureEmbeddedCache(cache, "0.26.3"); err != nil {
		t.Fatalf("升级 re-extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(cache, skillscanonical.VersionFile))
	if err != nil {
		t.Fatal("升级后版本标记缺失")
	}
	if string(got) != "0.26.3" {
		t.Fatalf("升级后版本标记 = %q, want 0.26.3", string(got))
	}
	// re-extract 后内容应仍在（CONVENTIONS.md 存在）
	if _, err := os.Stat(filepath.Join(cache, "CONVENTIONS.md")); err != nil {
		t.Fatal("re-extract 后 CONVENTIONS.md 缺失")
	}
}

// TestEnsureEmbeddedCache_ReusesWhenVersionMatches 版本一致：复用缓存，不 RemoveAll。
// 用 marker 文件验证缓存未被重建（一致时必须跳过 extract，否则每次调用都重解压 1.5M）。
func TestEnsureEmbeddedCache_ReusesWhenVersionMatches(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "embedded")
	if err := skillscanonical.EnsureEmbeddedCache(cache, "0.26.3"); err != nil {
		t.Fatalf("首次 extract: %v", err)
	}
	// 放一个 marker，模拟缓存里既有内容。版本一致时复用 → marker 应保留。
	marker := filepath.Join(cache, "user-marker")
	if err := os.WriteFile(marker, []byte("keep-me"), 0644); err != nil {
		t.Fatalf("写 marker: %v", err)
	}
	if err := skillscanonical.EnsureEmbeddedCache(cache, "0.26.3"); err != nil {
		t.Fatalf("复用调用: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("版本一致却重建了缓存(marker 丢失)——应复用不动")
	}
}

// TestEnsureEmbeddedCache_ReextractsOnCorruptedVersionMarker 标记损坏(缺失)：CONVENTIONS.md 在但标记文件丢失 → 重建。
// 防御：用户手动删了 .embedded-version 但留了 skill 目录，不能误判为版本一致。
func TestEnsureEmbeddedCache_ReextractsOnCorruptedVersionMarker(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "embedded")
	if err := skillscanonical.EnsureEmbeddedCache(cache, "0.26.3"); err != nil {
		t.Fatalf("首次 extract: %v", err)
	}
	// 删版本标记，模拟损坏
	if err := os.Remove(filepath.Join(cache, skillscanonical.VersionFile)); err != nil {
		t.Fatalf("删标记: %v", err)
	}
	// CONVENTIONS.md 还在但标记没了 → 应重建（ReadFile 失败 → 不匹配 → re-extract）
	if err := skillscanonical.EnsureEmbeddedCache(cache, "0.26.3"); err != nil {
		t.Fatalf("损坏后重建: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(cache, skillscanonical.VersionFile)); err != nil {
		t.Fatal("重建后标记未重写")
	}
}
