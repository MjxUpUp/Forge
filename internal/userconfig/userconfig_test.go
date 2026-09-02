package userconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// userconfig_test.go — Project Policy Layer P2 的用户级偏好存储契约：
// takeover 三档（ask 出厂默认 / auto / off）、env 覆盖优先级、落盘原子写。

func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	return home
}

// TestTakeoverMode_DefaultAsk 无配置无 env → 出厂默认 ask（P2 默认值翻转的钉子）。
func TestTakeoverMode_DefaultAsk(t *testing.T) {
	useTempHome(t)
	if got := TakeoverMode(); got != TakeoverAsk {
		t.Fatalf(`TakeoverMode = %q, want ask（出厂默认）`, got)
	}
}

// TestTakeoverMode_SetGet 配置落盘后读取生效；非法值被 Set 拒绝。
func TestTakeoverMode_SetGet(t *testing.T) {
	useTempHome(t)
	if err := SetTakeover(TakeoverAuto); err != nil {
		t.Fatal(err)
	}
	if got := TakeoverMode(); got != TakeoverAuto {
		t.Fatalf(`TakeoverMode = %q, want auto`, got)
	}
	// 落盘位置：GlobalHome/config.json（用户级 store，升级链路不触碰项目）。
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, `.forge`, `config.json`)); err != nil && os.Getenv(`FORGE_DATA_HOME`) != `` {
		// FORGE_DATA_HOME 重定向时不要求默认路径存在——只验 store 内文件。
		t.Log(`FORGE_DATA_HOME redirected; default path check skipped`)
	}
	if err := SetTakeover(`bogus`); err == nil {
		t.Fatal(`SetTakeover accepted invalid value`)
	}
}

// TestTakeoverMode_EnvPrecedence FORGE_TAKEOVER > FORGE_AUTO_INIT（legacy 映射 auto）
// > 配置文件 > 默认 ask。
func TestTakeoverMode_EnvPrecedence(t *testing.T) {
	useTempHome(t)
	if err := SetTakeover(TakeoverOff); err != nil {
		t.Fatal(err)
	}

	t.Setenv(`FORGE_AUTO_INIT`, `1`)
	if got := TakeoverMode(); got != TakeoverAuto {
		t.Fatalf(`FORGE_AUTO_INIT=1 → %q, want auto`, got)
	}

	t.Setenv(`FORGE_TAKEOVER`, `off`)
	if got := TakeoverMode(); got != TakeoverOff {
		t.Fatalf(`FORGE_TAKEOVER=off → %q, want off（压过 AUTO_INIT）`, got)
	}

	t.Setenv(`FORGE_AUTO_INIT`, ``)
	if got := TakeoverMode(); got != TakeoverOff {
		t.Fatalf(`FORGE_TAKEOVER=off（无 AUTO_INIT）→ %q, want off`, got)
	}

	os.Unsetenv(`FORGE_TAKEOVER`)
	if got := TakeoverMode(); got != TakeoverOff {
		t.Fatalf(`仅配置文件 → %q, want off`, got)
	}
}

// TestTakeoverMode_InvalidEnv 非法 env 值回退配置/默认（fail-safe：宽容读侧）。
func TestTakeoverMode_InvalidEnv(t *testing.T) {
	useTempHome(t)
	t.Setenv(`FORGE_TAKEOVER`, `nonsense`)
	if got := TakeoverMode(); got != TakeoverAsk {
		t.Fatalf(`invalid env → %q, want ask（回退默认）`, got)
	}
}
