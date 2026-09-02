package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/userconfig"
)

// config_test.go — forge config / forge policy 命令面契约（P2）。
// stdout 捕获复用 hook_helpers_test.go 的 captureOutput。

func TestConfigGetSet_Roundtrip(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	t.Setenv(`FORGE_TAKEOVER`, ``)
	t.Setenv(`FORGE_AUTO_INIT`, ``)

	// 未设置 → 生效 ask（出厂默认）
	stdout, _, err := captureOutput(t, func() error {
		return configGetCmd.RunE(configGetCmd, []string{`takeover`})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `ask`) {
		t.Fatalf(`get takeover (unset) = %q, want ask`, stdout)
	}

	if _, _, err := captureOutput(t, func() error {
		return configSetCmd.RunE(configSetCmd, []string{`takeover`, `auto`})
	}); err != nil {
		t.Fatal(err)
	}
	if got := userconfig.TakeoverMode(); got != userconfig.TakeoverAuto {
		t.Fatalf(`after set, TakeoverMode = %q, want auto`, got)
	}
	// 非法值拒绝
	if err := configSetCmd.RunE(configSetCmd, []string{`takeover`, `bogus`}); err == nil {
		t.Fatal(`set bogus accepted`)
	}
}

// TestPolicyState_TriState 三态输出（managed/declined/unknown），退出恒 0。
func TestPolicyState_TriState(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	proj := t.TempDir()
	t.Chdir(proj)

	run := func() string {
		stdout, _, err := captureOutput(t, func() error {
			return policyStateCmd.RunE(policyStateCmd, nil)
		})
		if err != nil {
			t.Fatalf(`policy state: %v`, err)
		}
		return strings.TrimSpace(stdout)
	}

	if got := run(); got != registry.StatusUnknown {
		t.Fatalf(`unknown project state = %q, want unknown`, got)
	}
	if err := registry.SetStatus(proj, registry.StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}
	if got := run(); got != registry.StatusDeclined {
		t.Fatalf(`declined state = %q, want declined`, got)
	}
	if err := registry.SetStatus(proj, registry.StatusManaged, `forge on`); err != nil {
		t.Fatal(err)
	}
	// 显示层映射：StatusManaged 空串 → 字面 "managed"（与帮助文案一致）。
	if got := run(); got != `managed` {
		t.Fatalf(`managed state display = %q, want "managed"`, got)
	}
}
