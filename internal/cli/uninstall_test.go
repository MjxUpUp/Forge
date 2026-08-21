package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uninstall_test.go — core side-effect guard for `forge uninstall`.
// Tests uninstallClearMarkers directly (does not call rootCmd.Execute, to avoid global pollution).
// refactor-data-home commit E: the marker store goes through forgedata.GlobalHome() (FORGE_DATA_HOME);
// tests isolate via FORGE_DATA_HOME (no longer via HOME — GlobalHome reads os.UserHomeDir, not the HOME env).
// All Chinese strings use raw strings to avoid Windows input-quote corruption.
//
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
	// When <GlobalHome>/.init-suggested/ does not exist, RemoveAll still returns nil — should return ok=true.
	//
	// <GlobalHome>/.init-suggested/ 不存在时 RemoveAll 也返 nil — 应返 ok=true。
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	_, ok := uninstallClearMarkers()
	if !ok {
		t.Fatal(`markers 不存在也应返 ok=true（RemoveAll 幂等）`)
	}
}

// TestUninstall_ClearsMarkers_ForgeDataHomeOverride pins commit E: uninstall must clear
// markers under the FORGE_DATA_HOME override root (not ~/.forge) — it shares the same store
// as the suggest command and the init-suggest hook. Prevents uninstall from secretly falling
// back to a hardcoded ~/.forge and clearing the wrong place for FORGE_DATA_HOME users.
//
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

// TestUninstall_StripsKimiHooks pins the kimi cleanup added with the kimi plugin
// adapter: uninstall must strip the forge marker section from kimi's user-level
// config.toml (those entries would otherwise spawn a deleted binary on every kimi
// tool call) and print the removal guidance for the TUI-only plugin path.
//
// TestUninstall_StripsKimiHooks 钉住随 kimi plugin 适配加入的 kimi 清理：uninstall
// 必须剥除 kimi user-level config.toml 的 forge 标记段（否则这些条目会在每次
// kimi 工具调用时 spawn 一个已删除的二进制），并打印 TUI 专属 plugin 卸载指引。
func TestUninstall_StripsKimiHooks(t *testing.T) {
	t.Setenv(`FORGE_UNINSTALL_SKIP_NPM`, `1`)
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	kimiHome := t.TempDir()
	t.Setenv(`KIMI_CODE_HOME`, kimiHome)

	// Seed a kimi config.toml with a forge marker section (as `forge init --agents kimi` wrote).
	userConfig := "default_model = \"kimi-code/k3\"\n"
	cfg := filepath.Join(kimiHome, `config.toml`)
	seed := userConfig + "\n# FORGE:START — managed by `forge init --agents kimi`; do not edit between markers\n[[hooks]]\nevent = \"Stop\"\ncommand = \"forge hook task-verify --agent kimi\"\n# FORGE:END\n"
	if err := os.WriteFile(cfg, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureOutput(t, func() error {
		return uninstallCmd.RunE(uninstallCmd, nil)
	})
	if err != nil {
		t.Fatalf(`uninstall RunE: %v`, err)
	}

	data, _ := os.ReadFile(cfg)
	if string(data) != userConfig {
		t.Errorf(`kimi config.toml 未还原为用户原内容，实得：`+"\n"+`%q`, string(data))
	}
	if !strings.Contains(stdout, `已清除 kimi-code config.toml 中的 forge hooks`) {
		t.Errorf(`缺少 kimi hooks 清除提示，stdout：`+"\n"+`%s`, stdout)
	}
	if !strings.Contains(stdout, `Kimi Code:`) {
		t.Errorf(`缺少 kimi plugin 卸载指引，stdout：`+"\n"+`%s`, stdout)
	}
}

// TestUninstall_StripsReasonixHooks pins the reasonix hook-strip wiring: uninstall must
// remove forge hooks from reasonix's user-level settings.json (flat schema) while preserving
// user content, and print the removal guidance. Mirrors TestUninstall_StripsKimiHooks; full
// agent-home isolation (every agent home → TempDir via env + HOME/USERPROFILE) so RunE touches
// no real config.
//
// TestUninstall_StripsReasonixHooks 钉死 reasonix hook 剥除接线：uninstall 必须从 reasonix
// 用户级 settings.json（扁平 schema）移除 forge hooks 同时保留用户内容，并打印清除指引。
// 镜像 TestUninstall_StripsKimiHooks；全 agent home 隔离（每个 agent home 经 env +
// HOME/USERPROFILE 指向 TempDir），RunE 不碰真实配置。
func TestUninstall_StripsReasonixHooks(t *testing.T) {
	t.Setenv(`FORGE_UNINSTALL_SKIP_NPM`, `1`)
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	t.Setenv(`KIMI_CODE_HOME`, t.TempDir())
	t.Setenv(`CODEX_HOME`, t.TempDir())
	t.Setenv(`XDG_CONFIG_HOME`, t.TempDir())
	t.Setenv(`CLAUDE_CONFIG_DIR`, t.TempDir())
	home := t.TempDir()
	t.Setenv(`HOME`, home)
	t.Setenv(`USERPROFILE`, home)
	reasonixHome := t.TempDir()
	t.Setenv(`REASONIX_HOME`, reasonixHome)

	// Seed reasonix settings.json: a forge hook + a user hook in one event + a user top-level key.
	settingsPath := filepath.Join(reasonixHome, `settings.json`)
	seed := `{
		"myKey": "keep-me",
		"hooks": {
			"PreToolUse": [
				{ "match": "Bash", "command": "forge hook bash-guard" },
				{ "match": "Bash", "command": "echo user-hook" }
			]
		}
	}`
	if err := os.WriteFile(settingsPath, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureOutput(t, func() error {
		return uninstallCmd.RunE(uninstallCmd, nil)
	})
	if err != nil {
		t.Fatalf(`uninstall RunE: %v`, err)
	}

	data, _ := os.ReadFile(settingsPath)
	body := string(data)
	if strings.Contains(body, `forge hook`) {
		t.Errorf(`reasonix settings.json 中应无 forge hooks 残留，实得：`+"\n"+`%s`, body)
	}
	if !strings.Contains(body, `keep-me`) || !strings.Contains(body, `echo user-hook`) {
		t.Errorf(`reasonix 用户内容未原样保留，实得：`+"\n"+`%s`, body)
	}
	if !strings.Contains(stdout, `已清除 reasonix 用户级配置中的 forge hooks`) {
		t.Errorf(`缺少 reasonix hooks 清除提示，stdout：`+"\n"+`%s`, stdout)
	}
}

// TestUninstall_RemovesUserLevelQualitySkill pins the uninstall gap fix: the
// user-level ~/.claude/skills/forge-quality/ (written by every init/autoSync)
// must be removed on uninstall, respecting CLAUDE_CONFIG_DIR.
//
// TestUninstall_RemovesUserLevelQualitySkill 钉死 uninstall 漏删修复：用户级
// ~/.claude/skills/forge-quality/（每次 init/autoSync 都会写）必须在卸载时
// 删除，且尊重 CLAUDE_CONFIG_DIR。
func TestUninstall_RemovesUserLevelQualitySkill(t *testing.T) {
	t.Setenv(`FORGE_UNINSTALL_SKIP_NPM`, `1`)
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	t.Setenv(`KIMI_CODE_HOME`, t.TempDir())
	t.Setenv(`CODEX_HOME`, t.TempDir())
	t.Setenv(`XDG_CONFIG_HOME`, t.TempDir())
	home := t.TempDir()
	t.Setenv(`HOME`, home)
	t.Setenv(`USERPROFILE`, home)
	claudeHome := t.TempDir()
	t.Setenv(`CLAUDE_CONFIG_DIR`, claudeHome)

	skillDir := filepath.Join(claudeHome, `skills`, `forge-quality`)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, `SKILL.md`), []byte(`# forge-quality`), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureOutput(t, func() error {
		return uninstallCmd.RunE(uninstallCmd, nil)
	})
	if err != nil {
		t.Fatalf(`uninstall RunE: %v`, err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Errorf(`用户级 forge-quality skill 应被删除，实得 stat err=%v`, err)
	}
	if !strings.Contains(stdout, `已删除用户级 forge-quality skill`) {
		t.Errorf(`缺少 skill 删除提示，stdout：`+"\n"+`%s`, stdout)
	}
}

// TestUninstall_RemovesReasonixQualitySkill pins the symmetric reasonix uninstall:
// the user-level <reasonix home>/skills/forge-quality/ (written by the reasonix
// translator) must be removed on uninstall, respecting REASONIX_HOME.
//
// TestUninstall_RemovesReasonixQualitySkill 钉死对称的 reasonix 卸载：用户级
// <reasonix home>/skills/forge-quality/（由 reasonix translator 写入）必须在卸载时
// 删除，且尊重 REASONIX_HOME。
func TestUninstall_RemovesReasonixQualitySkill(t *testing.T) {
	t.Setenv(`FORGE_UNINSTALL_SKIP_NPM`, `1`)
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	t.Setenv(`KIMI_CODE_HOME`, t.TempDir())
	t.Setenv(`CODEX_HOME`, t.TempDir())
	t.Setenv(`XDG_CONFIG_HOME`, t.TempDir())
	t.Setenv(`CLAUDE_CONFIG_DIR`, t.TempDir())
	home := t.TempDir()
	t.Setenv(`HOME`, home)
	t.Setenv(`USERPROFILE`, home)
	reasonixHome := t.TempDir()
	t.Setenv(`REASONIX_HOME`, reasonixHome)

	skillDir := filepath.Join(reasonixHome, `skills`, `forge-quality`)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, `SKILL.md`), []byte(`# forge-quality`), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureOutput(t, func() error {
		return uninstallCmd.RunE(uninstallCmd, nil)
	})
	if err != nil {
		t.Fatalf(`uninstall RunE: %v`, err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Errorf(`reasonix 用户级 forge-quality skill 应被删除，实得 stat err=%v`, err)
	}
	if !strings.Contains(stdout, `已删除 reasonix 用户级 forge-quality skill`) {
		t.Errorf(`缺少 reasonix skill 删除提示，stdout：`+"\n"+`%s`, stdout)
	}
}

// TestUninstall_GuidanceNoStaleReset pins the guidance text: it must not reference
// the removed `forge init --reset` command, and must point at --restore for rollback.
//
// TestUninstall_GuidanceNoStaleReset 钉死指引文案：不得引用已删除的
// `forge init --reset` 命令，且必须指向 --restore 回滚。
func TestUninstall_GuidanceNoStaleReset(t *testing.T) {
	src, err := os.ReadFile("uninstall.go")
	if err != nil {
		t.Fatalf("read uninstall.go: %v", err)
	}
	if strings.Contains(string(src), "init --reset") {
		t.Errorf("uninstall.go 仍引用不存在的 forge init --reset")
	}
	if !strings.Contains(string(src), "--restore") {
		t.Errorf("uninstall.go 指引应包含 --restore 回滚路径")
	}
}
