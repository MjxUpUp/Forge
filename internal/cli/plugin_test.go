package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/spf13/cobra"
)

// plugin_test.go — forge plugin status/dedupe 命令 + dedupeProjectLevelIfPlugin 的直接测试。
//
// N3 缺口补:cli TestMain 把 CLAUDE_CONFIG_DIR 钉到空目录（强制 IsClaudePluginInstalled()=false）,
// 致 dedupeProjectLevelIfPlugin 的"plugin 已装"分支从不被执行。本文件 t.Setenv 注入已装
// fixture（覆盖 TestMain 默认）,钉死已装→清理 / 未装→保留 两分支。

// writeForgePluginFixture 在 home 下写真机 schema 的 installed_plugins.json（forge@mp
// scope=user）,使 IsClaudePluginInstalledAt(home)=true。
func writeForgePluginFixture(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	reg := `{"version":2,"plugins":{"forge@mp":[{"scope":"user"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "installed_plugins.json"), []byte(reg), 0644); err != nil {
		t.Fatalf("write installed_plugins: %v", err)
	}
}

// writeProjectLevelForgeDupes 在 dir 预置 project-level 重复资产（纯 forge 来源）:
// .claude/settings.local.json（GenerateSettings 写的 ForgeHookSpec hooks）+ .mcp.json
// （纯 forge MCP server）。模拟 init/sync 刚写入、dedupe 尚未清理的状态。
func writeProjectLevelForgeDupes(t *testing.T, dir string) {
	t.Helper()
	if err := hooks.GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	mcp := `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcp), 0644); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}
}

// TestDedupeProjectLevelIfPlugin_PluginInstalled：plugin 已装时,自动 dedupe 清 project-level
// 重复——settings.local.json 保留文件壳写 {}（dedupeProjectLevelIfPlugin 内 keepEmpty=true,
// 用户痛点:forge 不静默删个人配置文件）,.mcp.json 删空。N3：该分支此前因 TestMain 钉死未装从未被测。
func TestDedupeProjectLevelIfPlugin_PluginInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)

	dir := t.TempDir()
	writeProjectLevelForgeDupes(t, dir)

	dedupeProjectLevelIfPlugin(dir)

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Errorf(`自动 dedupe 应保留 settings.local.json 写 {},不删: %v`, err)
	} else if got := strings.TrimSpace(string(data)); got != "{}" {
		t.Errorf(`自动 dedupe 应写 {}, got %q`, got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf(`plugin 已装时 .mcp.json 应被 dedupe 删除,stat err=%v`, err)
	}
}

// TestDedupeProjectLevelIfPlugin_PluginNotInstalled_NoOp：plugin 未装时,dedupe no-op
// （project-level 是唯一来源,保留）。
func TestDedupeProjectLevelIfPlugin_PluginNotInstalled_NoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home) // 无 fixture → IsClaudePluginInstalled=false

	dir := t.TempDir()
	writeProjectLevelForgeDupes(t, dir)

	dedupeProjectLevelIfPlugin(dir)

	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); err != nil {
		t.Errorf(`plugin 未装时不应删 settings.local.json,stat err=%v`, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err != nil {
		t.Errorf(`plugin 未装时不应删 .mcp.json,stat err=%v`, err)
	}
}

// TestRunPluginDedupe_KeepEmptyFlag：钉死 forge plugin dedupe 的 --keep-empty flag 传递——
// 带 flag（init-suggest SessionStart 自动调用）保留 settings.local.json 写 {};不带（手动清理）
// 删空文件。两种情况 .mcp.json 都删空（keepEmpty 只影响 settings）。防 flag 注册/读取回归。
func TestRunPluginDedupe_KeepEmptyFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)

	cases := []struct {
		name      string
		keepEmpty bool
	}{
		{"manual_no_flag_deletes", false},
		{"auto_keep_empty_preserves", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectLevelForgeDupes(t, dir)

			cmd := &cobra.Command{RunE: runPluginDedupe}
			cmd.Flags().Bool("keep-empty", false, "")
			if tc.keepEmpty {
				if err := cmd.Flags().Set("keep-empty", "true"); err != nil {
					t.Fatalf("set keep-empty: %v", err)
				}
			}
			if err := runPluginDedupe(cmd, []string{dir}); err != nil {
				t.Fatalf("runPluginDedupe: %v", err)
			}

			settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
			data, statErr := os.ReadFile(settingsPath)
			if tc.keepEmpty {
				if statErr != nil {
					t.Fatalf(`--keep-empty 应保留 settings.local.json 写 {},不删: %v`, statErr)
				}
				if got := strings.TrimSpace(string(data)); got != "{}" {
					t.Errorf(`--keep-empty 应写 {}, got %q`, got)
				}
			} else {
				if !os.IsNotExist(statErr) {
					t.Errorf(`无 --keep-empty 应删 settings.local.json, stat err=%v`, statErr)
				}
			}
			// .mcp.json 两种情况都删空（keepEmpty 不影响 MCP）。
			if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
				t.Errorf(`.mcp.json 应删空（keepEmpty 不影响 MCP）, stat err=%v`, err)
			}
		})
	}
}

// TestDedupeProjectLevelIfPlugin_AlsoStripsUserLevel：钉死 init/sync 这条 auto 路径
// （dedupeProjectLevelIfPlugin）在 plugin 已装时同时清 project-level + user-level 重复——
// home 下 settings.local.json 的 forge hook 与 plugin manifest 重复（历史 global init 残留）。
func TestDedupeProjectLevelIfPlugin_AlsoStripsUserLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)

	dir := t.TempDir()
	writeProjectLevelForgeDupes(t, dir)
	// user-level 重复：home 下 settings.local.json 放 forge hook（历史 global init 残留）。
	userLevel := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"forge hook task-verify"}]}]}}`
	if err := os.WriteFile(filepath.Join(home, "settings.local.json"), []byte(userLevel), 0644); err != nil {
		t.Fatalf("write user-level: %v", err)
	}

	dedupeProjectLevelIfPlugin(dir)

	// project-level 清理（settings.local.json 写 {},.mcp.json 删）。
	projData, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Errorf(`project settings.local.json 应保留写 {},不删: %v`, err)
	} else if got := strings.TrimSpace(string(projData)); got != "{}" {
		t.Errorf(`project settings 应写 {}, got %q`, got)
	}
	// user-level 清理（写 {} 保留壳,绝不删用户全局配置）。
	userData, err := os.ReadFile(filepath.Join(home, "settings.local.json"))
	if err != nil {
		t.Fatalf(`user-level settings.local.json 应保留写 {},不删: %v`, err)
	}
	if got := strings.TrimSpace(string(userData)); got != "{}" {
		t.Errorf(`user-level settings 应写 {}, got %q`, got)
	}
}

// TestRunPluginDedupe_AlsoStripsUserLevel：钉死 runPluginDedupe（forge plugin dedupe 的 RunE）
// 在 plugin 已装时清 user-level 重复 + 输出独立提示 user-level。
//
// 路径澄清（review M1）：本测试直接调 runPluginDedupe，不经 root PersistentPreRunE——这对应
// 用户在【非 forge 项目】（如 home）手动跑 `forge plugin dedupe` 的场景：findProjectRoot 失败
// （root.go:37）→ autoSync 不跑（sync.go 的 defer dedupeProjectLevelIfPlugin 不注册）→
// runPluginDedupe 是唯一清理者,strip + 输出 user-level。这是清 user 级全局重复最常见的入口
// （cd ~ && forge plugin dedupe）,runPluginDedupe 的 user 级分支在此现场是 live 路径,非死代码。
//
// 在【forge 项目】内（含 init-suggest SessionStart 的 dedupe 分支——$ROOT 有 .forge,embed.go:1290）
// 跑本命令时,autoSync 的 defer 先静默清完 user 级（dedupeProjectLevelIfPlugin）,runPluginDedupe
// 再跑成 no-op 无输出——该路径由 TestDedupeProjectLevelIfPlugin_AlsoStripsUserLevel 覆盖
// （直接调 autoSync defer 调用的同一函数）。两条测试互补,合覆盖 forge + 非 forge 两种调用现场。
//
// 即便 --keep-empty 未传（手动语义 project-level 删空）,user-level 仍固定保留壳（绝不删用户全局配置）。
func TestRunPluginDedupe_AlsoStripsUserLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)
	// 只放 user-level 重复（project-level 干净）,隔离验证 user-level 分支 + 输出。
	userLevel := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"forge hook task-verify"}]}]}}`
	if err := os.WriteFile(filepath.Join(home, "settings.local.json"), []byte(userLevel), 0644); err != nil {
		t.Fatalf("write user-level: %v", err)
	}

	var out strings.Builder
	cmd := &cobra.Command{RunE: runPluginDedupe}
	cmd.SetOut(&out)
	cmd.Flags().Bool("keep-empty", false, "")
	dir := t.TempDir() // project-level 无重复,隔离 user-level
	if err := runPluginDedupe(cmd, []string{dir}); err != nil {
		t.Fatalf("runPluginDedupe: %v", err)
	}

	// user-level 清理:写 {} 保留壳（不删）,即便 --keep-empty 未传（user-level 固定保留壳）。
	userData, err := os.ReadFile(filepath.Join(home, "settings.local.json"))
	if err != nil {
		t.Fatalf(`user-level 应保留写 {},不删: %v`, err)
	}
	if got := strings.TrimSpace(string(userData)); got != "{}" {
		t.Errorf(`user-level 应写 {}, got %q`, got)
	}
	if !strings.Contains(out.String(), "user-level") {
		t.Errorf(`应输出 user-level 提示, got %q`, out.String())
	}
}

// TestRunPluginDedupe_ProjectAndUserBothDirty：钉死 runPluginDedupe 的独立输出分支——
// project-level（hooks+MCP）与 user-level 同时脏时,两段输出都打印（非 else-if 互斥）。
// 防回归：未来若误改成 `else if userChanged` 会吞掉 project 段（或反之）,单测照过而行为错。
// 路径同 TestRunPluginDedupe_AlsoStripsUserLevel：直接调 RunE = 非 forge 项目手动跑现场。
func TestRunPluginDedupe_ProjectAndUserBothDirty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)
	// user-level 重复（home 下 settings.local.json）。
	userLevel := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"forge hook task-verify"}]}]}}`
	if err := os.WriteFile(filepath.Join(home, "settings.local.json"), []byte(userLevel), 0644); err != nil {
		t.Fatalf("write user-level: %v", err)
	}
	// project-level 重复（hooks + MCP）。
	dir := t.TempDir()
	writeProjectLevelForgeDupes(t, dir)

	var out strings.Builder
	cmd := &cobra.Command{RunE: runPluginDedupe}
	cmd.SetOut(&out)
	cmd.Flags().Bool("keep-empty", false, "") // 默认不保留壳:project 级纯 forge 文件会被删（file 态由 KeepEmptyFlag 测试覆盖,本测试只钉输出组合）
	if err := runPluginDedupe(cmd, []string{dir}); err != nil {
		t.Fatalf("runPluginDedupe: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "项目级重复") {
		t.Errorf(`应输出 project-level 段, got %q`, got)
	}
	if !strings.Contains(got, "user-level") {
		t.Errorf(`应输出 user-level 段, got %q`, got)
	}
}
