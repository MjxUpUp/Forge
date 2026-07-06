package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// plugin_detect_test.go — IsClaudePluginInstalledAt 的真行为守卫。
//
// fixture 基于真机核实的 installed_plugins.json schema（C:\Users\Administrator\.claude\
// plugins\installed_plugins.json）：
//
//	{"version":2,"plugins":{"<name>@<marketplace>":[{"scope":"user"|"project",...}]}}
//
// 非 guessing——真机文件确认顶层 plugins map + key 是 <name>@<mp> + value 是数组 +
// 每个元素 scope 字段。真机还含 superpowers@... scope=project 带 projectPath,正好
// 印证"只认 scope=user"（project-scope 非全机器接管）。

// writeInstalledPlugins 写 <claudeHome>/plugins/installed_plugins.json。plugins 形如
// {"forge@mp":[{"scope":"user"}]}。多个 install 元素时数组多项。
func writeInstalledPlugins(t *testing.T, claudeHome string, plugins map[string][]map[string]string) {
	t.Helper()
	dir := filepath.Join(claudeHome, "plugins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	reg := map[string]any{
		"version": float64(2),
		"plugins": plugins,
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		t.Fatalf("marshal installed_plugins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "installed_plugins.json"), data, 0644); err != nil {
		t.Fatalf("write installed_plugins: %v", err)
	}
}

func TestIsClaudePluginInstalledAt_EmptyHome(t *testing.T) {
	if IsClaudePluginInstalledAt("") {
		t.Error(`空 claudeHome 应返回 false`)
	}
}

func TestIsClaudePluginInstalledAt_NoFile(t *testing.T) {
	home := t.TempDir()
	if IsClaudePluginInstalledAt(home) {
		t.Error(`无 installed_plugins.json 应返回 false（fail-safe）`)
	}
}

func TestIsClaudePluginInstalledAt_CorruptJSON(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "plugins")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "installed_plugins.json"), []byte("{not json"), 0644)
	if IsClaudePluginInstalledAt(home) {
		t.Error(`损坏 JSON 应返回 false（fail-safe，不 panic）`)
	}
}

func TestIsClaudePluginInstalledAt_UserScope(t *testing.T) {
	home := t.TempDir()
	writeInstalledPlugins(t, home, map[string][]map[string]string{
		"forge@my-marketplace": {{"scope": "user"}},
	})
	if !IsClaudePluginInstalledAt(home) {
		t.Error(`forge@<任意 mp> scope=user 应识别为已装`)
	}
}

func TestIsClaudePluginInstalledAt_ProjectScopeOnly(t *testing.T) {
	home := t.TempDir()
	// 真机 superpowers@ 的形状：scope=project + projectPath。非 user-level 全机器接管。
	writeInstalledPlugins(t, home, map[string][]map[string]string{
		"forge@mp": {{"scope": "project", "projectPath": "/some/path"}},
	})
	if IsClaudePluginInstalledAt(home) {
		t.Error(`scope=project 不是 user-level 接管，应返回 false`)
	}
}

func TestIsClaudePluginInstalledAt_NonForgePlugin(t *testing.T) {
	home := t.TempDir()
	writeInstalledPlugins(t, home, map[string][]map[string]string{
		"glm-plan-usage@zai-coding-plugins": {{"scope": "user"}},
	})
	if IsClaudePluginInstalledAt(home) {
		t.Error(`非 forge@ 前缀的 user plugin 不应误判为 forge 已装`)
	}
}

func TestIsClaudePluginInstalledAt_MixedScopes(t *testing.T) {
	home := t.TempDir()
	// 同一 plugin 多 install：一个 project 一个 user —— 任一 user 即视为已装。
	writeInstalledPlugins(t, home, map[string][]map[string]string{
		"forge@mp": {
			{"scope": "project"},
			{"scope": "user"},
		},
	})
	if !IsClaudePluginInstalledAt(home) {
		t.Error(`多 install 中至少一个 scope=user 即视为已装`)
	}
}

func TestIsClaudePluginInstalledAt_ForgePrefixNotSubstring(t *testing.T) {
	home := t.TempDir()
	// "notforge@mp" 含 forge 子串但非 forge@ 前缀——HasPrefix 防 substring 误判。
	writeInstalledPlugins(t, home, map[string][]map[string]string{
		"notforge@mp": {{"scope": "user"}},
	})
	if IsClaudePluginInstalledAt(home) {
		t.Error(`notforge@ 含 forge 子串但非 forge@ 前缀，不应误判`)
	}
}

func TestClaudeHome_PrefersEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/claude")
	if got := ClaudeHome(); got != "/custom/claude" {
		t.Errorf(`ClaudeHome 应优先 CLAUDE_CONFIG_DIR，得 %q`, got)
	}
}

func TestClaudeHome_Fallback(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".claude")
	if got := ClaudeHome(); got != want {
		t.Errorf(`ClaudeHome fallback 应为 ~/.claude，得 %q want %q`, got, want)
	}
}

func TestIsClaudePluginInstalled_Wrapper(t *testing.T) {
	// 注入 CLAUDE_CONFIG_DIR = temp home，wrapper 应走 IsClaudePluginInstalledAt(home)。
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeInstalledPlugins(t, home, map[string][]map[string]string{
		"forge@mp": {{"scope": "user"}},
	})
	if !IsClaudePluginInstalled() {
		t.Error(`IsClaudePluginInstalled（wrapper）应反映 home 下已装`)
	}
}
