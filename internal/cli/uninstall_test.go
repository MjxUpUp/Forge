package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// uninstall_test.go — `forge uninstall` 的核心 side effect 守卫。
// 测 uninstallClearMarkers（不调 rootCmd.Execute 避全局污染）。
// refactor-data-home commit E：marker store 走 forgedata.GlobalHome()（FORGE_DATA_HOME），
// 测试用 FORGE_DATA_HOME 隔离（不再用 HOME——GlobalHome 读 os.UserHomeDir 不读 HOME env）。
// 所有中文字符串 raw string 规避 Windows 输入引号腐蚀。

func TestUninstall_ClearsSuggestMarkers(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, fakeHome)
	markerDir := filepath.Join(fakeHome, `.init-suggested`)
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
	// <GlobalHome>/.init-suggested/ 不存在时 RemoveAll 也返 nil — 应返 ok=true。
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	_, ok := uninstallClearMarkers()
	if !ok {
		t.Fatal(`markers 不存在也应返 ok=true（RemoveAll 幂等）`)
	}
}

// TestUninstall_ClearsMarkers_ForgeDataHomeOverride 钉死 commit E：uninstall 必须清
// FORGE_DATA_HOME 覆盖根下的 marker（不是 ~/.forge）——与 suggest 命令 + init-suggest
// hook 读写同一 store。防 uninstall 偷偷回硬编码 ~/.forge 致 FORGE_DATA_HOME 用户清错地方。
func TestUninstall_ClearsMarkers_ForgeDataHomeOverride(t *testing.T) {
	dd := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, dd)
	markerDir := filepath.Join(dd, `.init-suggested`)
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, ok := uninstallClearMarkers(); !ok {
		t.Fatal(`应清成功`)
	}
	if _, err := os.Stat(markerDir); !os.IsNotExist(err) {
		t.Errorf(`FORGE_DATA_HOME 覆盖根下 marker 应被删，实得 stat err=%v`, err)
	}
}
