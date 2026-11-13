package userconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// userconfig_test.go — 用户级偏好存储契约（Phase C 之后只剩 compat）：默认空、
// 落盘后读回、空串清除、损坏文件 fail-open。

func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	return home
}

// TestCompat_DefaultEmpty 无配置 → 空声明（跟最新，不钉版本）。
func TestCompat_DefaultEmpty(t *testing.T) {
	useTempHome(t)
	if got := CompatPref(); got != `` {
		t.Fatalf(`CompatPref = %q, want empty（默认跟最新）`, got)
	}
}

// TestCompat_SetGetClear 落盘后读回；空串清除。
func TestCompat_SetGetClear(t *testing.T) {
	home := useTempHome(t)
	if err := SetCompat(`1.80`); err != nil {
		t.Fatal(err)
	}
	if got := CompatPref(); got != `1.80` {
		t.Fatalf(`CompatPref = %q, want 1.80`, got)
	}
	if _, err := os.Stat(filepath.Join(home, `config.json`)); err != nil {
		t.Fatalf(`config.json not written under store root: %v`, err)
	}
	if err := SetCompat(``); err != nil {
		t.Fatal(err)
	}
	if got := CompatPref(); got != `` {
		t.Fatalf(`CompatPref after clear = %q, want empty`, got)
	}
}

// TestCompat_CorruptFileFailsOpen 损坏文件按零值读（回落跟最新，不阻断）。
func TestCompat_CorruptFileFailsOpen(t *testing.T) {
	home := useTempHome(t)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, `config.json`), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CompatPref(); got != `` {
		t.Fatalf(`CompatPref on corrupt file = %q, want empty`, got)
	}
}
