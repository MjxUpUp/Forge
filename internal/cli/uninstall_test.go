package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// uninstall_test.go — `forge uninstall` 的核心 side effect 守卫。
// 测 uninstallClearMarkers + uninstallHomeDir（不调 rootCmd.Execute 避全局污染）。
// HOME env 优先于 os.UserHomeDir（Windows 测试隔离 + Unix 通用）。
// 所有中文字符串 raw string 规避 Windows 输入引号腐蚀。

func TestUninstall_ClearsSuggestMarkers(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv(`HOME`, fakeHome)
	markerDir := filepath.Join(fakeHome, `.forge`, `.init-suggested`)
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatalf(`mkdir marker dir: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, `tag-suggested`), []byte(`suggested`), 0644); err != nil {
		t.Fatalf(`seed marker: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, `tag-declined`), []byte(`declined`), 0644); err != nil {
		t.Fatalf(`seed marker: %v`, err)
	}

	dir, ok := uninstallClearMarkers()
	if !ok {
		t.Errorf(`uninstallClearMarkers 返 false（%s 应被删）`, dir)
	}
	if _, err := os.Stat(markerDir); !os.IsNotExist(err) {
		t.Errorf(`markers 目录应被删除，实得 stat err=%v`, err)
	}
}

func TestUninstall_IdempotentWhenNoMarkers(t *testing.T) {
	// ~/.forge/.init-suggested/ 不存在时 RemoveAll 也返 nil — 应返 ok=true。
	fakeHome := t.TempDir()
	t.Setenv(`HOME`, fakeHome)
	_, ok := uninstallClearMarkers()
	if !ok {
		t.Fatal(`markers 不存在也应返 ok=true（RemoveAll 幂等）`)
	}
}

func TestUninstall_HomeDirPrefersHOME(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv(`HOME`, fakeHome)
	if got := uninstallHomeDir(); got != fakeHome {
		t.Errorf(`期望 HOME 优先：got=%s want=%s`, got, fakeHome)
	}
}

func TestUninstall_HomeDirFallsBackWhenHOMEEmpty(t *testing.T) {
	// HOME 空时退到 os.UserHomeDir()，确保非空（任何真实 home）。
	t.Setenv(`HOME`, ``)
	if got := uninstallHomeDir(); got == `` {
		t.Fatal(`fallback 不应返空`)
	}
}
