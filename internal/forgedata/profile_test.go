package forgedata

import (
	"path/filepath"
	"testing"
)

// TestGlobalProfile_PathAndUnresolvableHome：路径推导两态——FORGE_DATA_HOME
// 指向临时目录时落在其下；home 不可解析（UserHomeDir 失败模拟不可行，改以
// GlobalProfile 的纯函数契约为准：空串 = 不可解析，读侧回落默认档）。
// 纯路径包纪律：本文件只测路径，不测 IO（IO 在 hooks 包）。
func TestGlobalProfile_Path(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	want := filepath.Join(home, "profile")
	if got := GlobalProfile(); got != want {
		t.Errorf("GlobalProfile() = %q, want %q", got, want)
	}
}
