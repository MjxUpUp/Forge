package cli

// verify_checks_test.go —— user-level-assets 重构后默认模式完整性检查的守卫：
// hooks/skill/settings 断言用户级位置（DataDir/hooks、<ClaudeHome>/skills、
// <ClaudeHome>/settings.json），项目级副本保留为团队模式兼容。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
)

// writeUserHooks 把全部 hook 脚本副本写到 root 的 DataDir/hooks 下。
func writeUserHooks(t *testing.T, root string) string {
	t.Helper()
	hooksDir := filepath.Join(forgedata.DataDirFor(root), "hooks")
	for _, name := range hooks.HookNames() {
		p := filepath.Join(hooksDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return hooksDir
}

// TestCheckHooks_UserLevel: hooks present at DataDir/hooks → OK; one missing → FAIL.
//
// TestCheckHooks_UserLevel：DataDir/hooks 齐全 → OK；缺一个 → FAIL。
func TestCheckHooks_UserLevel(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	writeUserHooks(t, root)
	if r := checkHooks(root); !r.ok {
		t.Errorf("DataDir/hooks 齐全应 OK: %+v", r)
	}

	// 删掉一个 hook——检查必须 FAIL 并点名。
	victim := filepath.Join(forgedata.DataDirFor(root), "hooks", hooks.HookNames()[0])
	if err := os.Remove(victim); err != nil {
		t.Fatal(err)
	}
	r := checkHooks(root)
	if r.ok {
		t.Errorf("缺 hook 应 FAIL: %+v", r)
	}
}

// TestCheckHooks_ProjectLevelCompat: team mode — hooks only under project-level .forge/hooks still pass.
//
// TestCheckHooks_ProjectLevelCompat：团队模式——hook 只在项目级 .forge/hooks
// 也通过。
func TestCheckHooks_ProjectLevelCompat(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	for _, name := range hooks.HookNames() {
		p := filepath.Join(root, ".forge", "hooks", name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if r := checkHooks(root); !r.ok {
		t.Errorf("项目级 .forge/hooks（团队模式）应 OK: %+v", r)
	}
}

// TestCheckQualitySkill_UserLevel: the skill at <CLAUDE_CONFIG_DIR>/skills/ forge-quality/SKILL.md passes; missing everywhere fails; the project-level copy is the team-mode fallback.
//
// TestCheckQualitySkill_UserLevel：<CLAUDE_CONFIG_DIR>/skills/forge-quality/
// SKILL.md 存在通过；两处都无失败；项目级副本是团队模式兜底。
func TestCheckQualitySkill_UserLevel(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	root := t.TempDir()

	if r := checkQualitySkill(root); r.ok {
		t.Errorf("两处都无 skill 应 FAIL: %+v", r)
	}

	// 项目级副本（团队模式）算数。
	pp := filepath.Join(root, ".claude", "skills", "forge-quality", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(pp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, []byte("# skill\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if r := checkQualitySkill(root); !r.ok {
		t.Errorf("项目级 skill（团队模式）应 OK: %+v", r)
	}
	if err := os.RemoveAll(filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}

	// 用户级副本（重构后默认）算数。
	up := filepath.Join(claudeHome, "skills", "forge-quality", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(up), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(up, []byte("# skill\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if r := checkQualitySkill(root); !r.ok {
		t.Errorf("用户级 skill 应 OK: %+v", r)
	}
}

// TestCheckSettings_UserLevel pins the user-level settings.json forge-hook check.
//
// TestCheckSettings_UserLevel：含 forge hook 的用户级 settings.json 通过；不含
// forge hook 的 settings.json 失败；项目级 settings.local.json 是团队模式兜底；
// 已装 user-level plugin 恒通过。
func TestCheckSettings_UserLevel(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	root := t.TempDir()

	// 两处都无 → FAIL。
	if r := checkSettings(root); r.ok {
		t.Errorf("无任何 settings 应 FAIL: %+v", r)
	}

	// 不含 forge hook 的 settings.json → FAIL（仅存在不够）。
	plain := `{"env": {"FOO": "bar"}}`
	if err := os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte(plain), 0644); err != nil {
		t.Fatal(err)
	}
	if r := checkSettings(root); r.ok {
		t.Errorf("不含 forge hook 的 settings.json 应 FAIL: %+v", r)
	}

	// 含 forge hook 的 settings.json → OK。
	withHook := `{"hooks": {"PreToolUse": [{"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "forge hook task-guard"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte(withHook), 0644); err != nil {
		t.Fatal(err)
	}
	if r := checkSettings(root); !r.ok {
		t.Errorf("用户级 settings.json 含 forge hook 应 OK: %+v", r)
	}
	if err := os.Remove(filepath.Join(claudeHome, "settings.json")); err != nil {
		t.Fatal(err)
	}

	// 项目级 settings.local.json（团队模式）→ OK。
	pp := filepath.Join(root, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(pp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if r := checkSettings(root); !r.ok {
		t.Errorf("项目级 settings.local.json（团队模式）应 OK: %+v", r)
	}
	if err := os.RemoveAll(filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}

	// 已装 user-level plugin → 无需任何 settings 文件即 OK。
	pluginsDir := filepath.Join(claudeHome, "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatal(err)
	}
	reg := `{"plugins": {"forge@forge": [{"scope": "user"}]}}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(reg), 0644); err != nil {
		t.Fatal(err)
	}
	if r := checkSettings(root); !r.ok {
		t.Errorf("plugin 已装应 OK: %+v", r)
	}
}
