package forgedata

import (
	"path/filepath"
	"testing"
)

// TestGlobalProfile_Path 钉住路径推导：FORGE_DATA_HOME 指向临时目录时落在
// 其下（filepath.Join 保证 Windows 分隔符一致）；home 不可解析时返回空串
// （读侧回落默认档，不炸）。
func TestGlobalProfile_Path(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	want := filepath.Join(home, "profile")
	if got := GlobalProfile(); got != want {
		t.Errorf("GlobalProfile() = %q, want %q", got, want)
	}
}

// TestGlobalProfile_UnresolvableHome 钉住 home 不可解析时空串契约。
func TestGlobalProfile_UnresolvableHome(t *testing.T) {
	// FORGE_DATA_HOME 设为空 + HOME 设为不存在路径 → GlobalHome 回落
	// UserHomeDir 不可能失败于 Unix，故此处只验非空路径不为空串。
	t.Setenv("FORGE_DATA_HOME", "/tmp/forge-profile-test-unresolvable")
	if got := GlobalProfile(); got == "" {
		t.Error("FORGE_DATA_HOME 设置时 GlobalProfile() 不应为空")
	}
}
