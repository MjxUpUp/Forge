package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/spf13/cobra"
)

// plugin_test.go — direct tests for the forge plugin status/dedupe commands and dedupeProjectLevelIfPlugin.
//
// Closes the N3 gap: cli TestMain pins CLAUDE_CONFIG_DIR to an empty dir (forcing IsClaudePluginInstalled()=false),
// so the plugin-installed branch of dedupeProjectLevelIfPlugin is never exercised. This file injects an installed
// fixture via t.Setenv (overriding the TestMain default) to pin both branches: installed→cleanup / not-installed→preserve.
//
// plugin_test.go — forge plugin status/dedupe 命令 + dedupeProjectLevelIfPlugin 的直接测试。
//
// N3 缺口补:cli TestMain 把 CLAUDE_CONFIG_DIR 钉到空目录（强制 IsClaudePluginInstalled()=false）,
// 致 dedupeProjectLevelIfPlugin 的「plugin 已装」分支从不被执行。本文件 t.Setenv 注入已装
// fixture（覆盖 TestMain 默认）,钉死已装→清理 / 未装→保留 两分支。

// writeForgePluginFixture writes a real-schema installed_plugins.json under home (forge@mp
// scope=user) so that IsClaudePluginInstalledAt(home)=true.
//
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

// writeProjectLevelForgeDupes preseeds project-level duplicate assets in dir (pure forge origin):
// .claude/settings.local.json (ForgeHookSpec hooks, legacy residue) + .mcp.json
// (pure forge MCP server). Simulates the state right after init/sync wrote them and before dedupe cleans up.
//
// writeProjectLevelForgeDupes 在 dir 预置 project-level 重复资产（纯 forge 来源）:
// .claude/settings.local.json（ForgeHookSpec hooks，历史残留）+ .mcp.json
// （纯 forge MCP server）。模拟 init/sync 刚写入、dedupe 尚未清理的状态。
func writeProjectLevelForgeDupes(t *testing.T, dir string) {
	t.Helper()
	writeClaudeSettingsFixture(t, dir)
	mcp := `{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcp), 0644); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}
}

// TestDedupeProjectLevelIfPlugin_PluginInstalled: when the plugin is installed, auto dedupe cleans project-level
// duplicates — settings.local.json keeps the file shell writing {} (keepEmpty=true inside dedupeProjectLevelIfPlugin;
// user pain point: forge never silently deletes personal config files), and .mcp.json is deleted. N3: this branch
// was never tested before because TestMain pinned not-installed.
//
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

// TestDedupeProjectLevelIfPlugin_PluginNotInstalled_NoOp: when the plugin is not installed, dedupe is a no-op
// (project-level is the only source, preserved).
//
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

// TestRunPluginDedupe_KeepEmptyFlag: pins the --keep-empty flag plumbing of forge plugin dedupe —
// with the flag (init-suggest SessionStart auto-call) settings.local.json is preserved writing {}; without it (manual
// cleanup) the file is deleted. In both cases .mcp.json is deleted (keepEmpty only affects settings).
// Guards against flag registration/read regressions.
//
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
			// .mcp.json is deleted in both cases (keepEmpty does not affect MCP).
			//
			// .mcp.json 两种情况都删空（keepEmpty 不影响 MCP）。
			if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
				t.Errorf(`.mcp.json 应删空（keepEmpty 不影响 MCP）, stat err=%v`, err)
			}
		})
	}
}

// TestDedupeProjectLevelIfPlugin_AlsoStripsUserLevel: pins the auto path of init/sync
// (dedupeProjectLevelIfPlugin) cleaning both project-level + user-level duplicates when the plugin is installed —
// the forge hooks in home's settings.local.json duplicate the plugin manifest (legacy global init residue).
//
// TestDedupeProjectLevelIfPlugin_AlsoStripsUserLevel：钉死 init/sync 这条 auto 路径
// （dedupeProjectLevelIfPlugin）在 plugin 已装时同时清 project-level + user-level 重复——
// home 下 settings.local.json 的 forge hook 与 plugin manifest 重复（历史 global init 残留）。
func TestDedupeProjectLevelIfPlugin_AlsoStripsUserLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)

	dir := t.TempDir()
	writeProjectLevelForgeDupes(t, dir)
	// user-level duplicate: forge hooks placed in home's settings.local.json (legacy global init residue).
	//
	// user-level 重复：home 下 settings.local.json 放 forge hook（历史 global init 残留）。
	userLevel := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"forge hook task-verify"}]}]}}`
	if err := os.WriteFile(filepath.Join(home, "settings.local.json"), []byte(userLevel), 0644); err != nil {
		t.Fatalf("write user-level: %v", err)
	}

	dedupeProjectLevelIfPlugin(dir)

	// project-level cleanup: settings.local.json writes {}, .mcp.json deleted.
	//
	// project-level 清理（settings.local.json 写 {},.mcp.json 删）。
	projData, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Errorf(`project settings.local.json 应保留写 {},不删: %v`, err)
	} else if got := strings.TrimSpace(string(projData)); got != "{}" {
		t.Errorf(`project settings 应写 {}, got %q`, got)
	}
	// user-level cleanup: writes {} preserving the shell; never deletes user global config.
	//
	// user-level 清理（写 {} 保留壳,绝不删用户全局配置）。
	userData, err := os.ReadFile(filepath.Join(home, "settings.local.json"))
	if err != nil {
		t.Fatalf(`user-level settings.local.json 应保留写 {},不删: %v`, err)
	}
	if got := strings.TrimSpace(string(userData)); got != "{}" {
		t.Errorf(`user-level settings 应写 {}, got %q`, got)
	}
}

// TestRunPluginDedupe_AlsoStripsUserLevel: pins that runPluginDedupe (the RunE of forge plugin dedupe)
// cleans user-level duplicates when the plugin is installed and prints a standalone user-level notice.
//
// Path clarification (review M1): this test calls runPluginDedupe directly without going through root PersistentPreRunE —
// this corresponds to the scenario where the user manually runs `forge plugin dedupe` in a non-forge project (e.g. home):
// findProjectRoot fails (root.go:37) → autoSync does not run (sync.go's defer dedupeProjectLevelIfPlugin is not registered) →
// runPluginDedupe is the sole cleaner, stripping + emitting the user-level notice. This is the most common entry point for
// cleaning user-level global duplicates (cd ~ && forge plugin dedupe); the user-level branch of runPluginDedupe is a live path here, not dead code.
//
// When this command runs inside a forge project (incl. the dedupe branch of init-suggest SessionStart — $ROOT has .forge, embed.go:1290),
// autoSync's defer silently cleans user-level first (dedupeProjectLevelIfPlugin), then runPluginDedupe runs as a no-op with no output —
// that path is covered by TestDedupeProjectLevelIfPlugin_AlsoStripsUserLevel (which calls the same function invoked by autoSync's defer).
// The two tests are complementary and jointly cover both the forge and non-forge invocation sites.
//
// Even when --keep-empty is not passed (manual semantics: project-level is deleted), user-level still always preserves the shell (never deletes user global config).
//
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
	// Only seed user-level duplicates (project-level clean) to isolate verification of the user-level branch + output.
	//
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

	// user-level cleanup: writes {} to preserve the shell (not deleted), even when --keep-empty is not passed (user-level always preserves the shell).
	//
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

// TestRunPluginDedupe_ProjectAndUserBothDirty: pins the independent output branches of runPluginDedupe —
// when project-level (hooks+MCP) and user-level are both dirty, both output sections are printed (not else-if exclusive).
// Regression guard: if the future mistakenly changes to `else if userChanged`, the project section would be swallowed (or vice versa), and unit tests would still pass while behavior is wrong.
// Path is the same as TestRunPluginDedupe_AlsoStripsUserLevel: directly calling RunE = the manual-run site of a non-forge project.
//
// TestRunPluginDedupe_ProjectAndUserBothDirty：钉死 runPluginDedupe 的独立输出分支——
// project-level（hooks+MCP）与 user-level 同时脏时,两段输出都打印（非 else-if 互斥）。
// 防回归：未来若误改成 `else if userChanged` 会吞掉 project 段（或反之）,单测照过而行为错。
// 路径同 TestRunPluginDedupe_AlsoStripsUserLevel：直接调 RunE = 非 forge 项目手动跑现场。
func TestRunPluginDedupe_ProjectAndUserBothDirty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)
	// user-level duplicate (home's settings.local.json).
	//
	// user-level 重复（home 下 settings.local.json）。
	userLevel := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"forge hook task-verify"}]}]}}`
	if err := os.WriteFile(filepath.Join(home, "settings.local.json"), []byte(userLevel), 0644); err != nil {
		t.Fatalf("write user-level: %v", err)
	}
	// project-level duplicate (hooks + MCP).
	//
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

// TestPluginPackCmd_ReasonixHelpAndGeneration: the `forge plugin pack` command must advertise
// reasonix as a host in its help AND actually write the reasonix native manifest when run.
// Two coupled assertions: (1) the Short/Long help mentions reasonix + the reasonix-plugin.json
// file, guarding help-vs-generator drift (the 5th host was added to the generator, so the
// user-facing --help must reflect it or it under-advertises); (2) running runPluginPack writes
// plugins/forge/reasonix-plugin.json, guarding the cli→generator wiring (the new
// writeReasonixPluginManifest call in GeneratePluginPack must be reachable from the command).
//
// TestPluginPackCmd_ReasonixHelpAndGeneration：`forge plugin pack` 命令须在帮助里宣传
// reasonix 这个 host 且跑起来真的写出 reasonix native manifest。两条耦合断言：(1) Short/Long
// 帮助提到 reasonix + reasonix-plugin.json 文件，守护帮助与生成器不漂移（第 5 host 已加进
// 生成器，故用户可见的 --help 须反映它，否则低报）；(2) 跑 runPluginPack 写出
// plugins/forge/reasonix-plugin.json，守护 cli→generator 接线（GeneratePluginPack 里新加的
// writeReasonixPluginManifest 调用须从命令可达）。
func TestPluginPackCmd_ReasonixHelpAndGeneration(t *testing.T) {
	// (1) Help reflects the 5th host — guards help-vs-generator drift.
	if !strings.Contains(pluginPackCmd.Short, "reasonix") {
		t.Errorf("plugin pack Short must advertise the reasonix host, got %q", pluginPackCmd.Short)
	}
	if !strings.Contains(pluginPackCmd.Long, "reasonix-plugin.json") {
		t.Errorf("plugin pack Long must mention the reasonix-plugin.json manifest the generator emits, got %q", pluginPackCmd.Long)
	}

	// (2) Running the command actually writes the reasonix manifest (cli→generator wiring).
	dir := t.TempDir()
	cmd := &cobra.Command{RunE: runPluginPack}
	cmd.Flags().String("out", "", "")
	if err := cmd.Flags().Set("out", dir); err != nil {
		t.Fatalf("set out: %v", err)
	}
	if err := runPluginPack(cmd, nil); err != nil {
		t.Fatalf("runPluginPack: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "forge", "reasonix-plugin.json")); err != nil {
		t.Errorf("runPluginPack must write plugins/forge/reasonix-plugin.json (5th-host wiring): %v", err)
	}
}

// TestPluginPackCmd_SkillsHelpAndGeneration: the `forge plugin pack` help must advertise the
// skills distribution AND running runPluginPack must actually write the skills tree — guards
// the cli→generator wiring for writePluginSkills (the pack shipped hooks-only for its whole
// life; if this wiring regresses, plugin users see zero skills again).
//
// TestPluginPackCmd_SkillsHelpAndGeneration：`forge plugin pack` 帮助须宣传 skills 分发，
// 且跑 runPluginPack 必须真的写出 skills 树——守护 writePluginSkills 的 cli→generator 接线
// （pack 有史以来只带 hooks；此接线回退，plugin 用户再次看不到任何 skill）。
func TestPluginPackCmd_SkillsHelpAndGeneration(t *testing.T) {
	// (1) Help reflects skills distribution — guards help-vs-generator drift.
	//
	// (1) 帮助反映 skills 分发——守护帮助与生成器不漂移。
	if !strings.Contains(pluginPackCmd.Long, "skills/<skill>/") {
		t.Errorf("plugin pack Long must mention the skills/<skill>/ distribution the generator emits, got %q", pluginPackCmd.Long)
	}

	// (2) Running the command actually writes the skills tree (cli→generator wiring): at
	// least one skill dir with SKILL.md under plugins/<name>/skills/.
	//
	// (2) 跑命令真的写出 skills 树（cli→generator 接线）：plugins/<name>/skills/ 下至少
	// 一个带 SKILL.md 的 skill 目录。
	dir := t.TempDir()
	cmd := &cobra.Command{RunE: runPluginPack}
	cmd.Flags().String("out", "", "")
	if err := cmd.Flags().Set("out", dir); err != nil {
		t.Fatalf("set out: %v", err)
	}
	if err := runPluginPack(cmd, nil); err != nil {
		t.Fatalf("runPluginPack: %v", err)
	}
	skillsDir := filepath.Join(dir, "plugins", "forge", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("runPluginPack must write plugins/forge/skills/ (skills distribution wiring): %v", err)
	}
	shipped := 0
	for _, e := range entries {
		if e.IsDir() {
			if _, serr := os.Stat(filepath.Join(skillsDir, e.Name(), "SKILL.md")); serr == nil {
				shipped++
			}
		}
	}
	if shipped == 0 {
		t.Errorf("plugins/forge/skills/ has 0 dirs with SKILL.md — skills distribution regressed to hooks-only")
	}
}

// writeClaudeSettingsFixture writes a project-level .claude/settings.local.json whose
// hooks section is exactly hooks.ForgeHookSpec — the test-side stand-in for the removed
// hooks.GenerateSettings writer. Dedupe tests use it to simulate legacy project-level
// residue; parity-style tests read the file as the Claude Code wiring ground truth.
//
// writeClaudeSettingsFixture 写项目级 .claude/settings.local.json，hooks 段恰为
// hooks.ForgeHookSpec——已删除的 hooks.GenerateSettings writer 的测试侧替身。
// dedupe 测试用它模拟历史项目级残留；parity 类测试把它读作 Claude Code 接线基准。
func writeClaudeSettingsFixture(t *testing.T, dir string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"hooks": hooks.ForgeHookSpec()})
	if err != nil {
		t.Fatalf("marshal ForgeHookSpec: %v", err)
	}
	path := filepath.Join(dir, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		t.Fatalf("write settings.local.json: %v", err)
	}
}

// TestPluginKimiManifestCmd pins the CLI regen outlet for the committed kimi manifest
// (2026-08 audit follow-up: the Build trio had no production caller). Three contracts:
// (a) --write rewrites a drifted/absent .kimi-plugin/plugin.json from npm/package.json's
// version + ForgeHookSpec + the shared description; (b) a second --write run is an in-sync
// no-op (byte-stable, no rewrite churn); (c) default run is report-only — prints the
// manifest, never touches the file, exit 0 on drift too (enforcement belongs to the guard
// test). Runs in a temp forge repo (npm/package.json + cwd) so the real repo is untouched.
//
// TestPluginKimiManifestCmd 钉死已提交 kimi manifest 的 CLI 再生成出口（2026-08 审计
// 后续项：Build 三件套此前没有生产调用方）。三份契约：(a) --write 把漂移/缺失的
// .kimi-plugin/plugin.json 从 npm/package.json 版本 + ForgeHookSpec + 共享 description
// 重写；(b) 二次 --write 是 in-sync no-op（字节稳定，不反复改写）；(c) 默认运行只
// 报告——打印 manifest、不碰文件、漂移也退出 0（执法归守卫测试）。在临时 forge 仓库
// （npm/package.json + cwd）里跑，真实仓库零接触。
func TestPluginKimiManifestCmd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "npm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "npm", "package.json"), []byte(`{"name":"@agent_forge/forge","version":"9.9.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed go.mod: the --write guard (review L-1) requires a second forge-repo marker
	// beyond npm/package.json — the real forge repo root always has go.mod. Without it
	// contract (a) would trip the guard instead of exercising the write path.
	//
	// 补种 go.mod：--write 守卫（评审 L-1）要求 npm/package.json 之外的第二 forge
	// 仓库标记——真实 forge 仓库根必有 go.mod。没有它，契约 (a) 会撞守卫而非走到写路径。
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/MjxUpUp/Forge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	manifestPath := filepath.Join(root, ".kimi-plugin", "plugin.json")

	// (a) --write on absent file: creates it, version from npm/package.json.
	pluginKimiManifestCmd.SetOut(os.Stderr)
	if err := pluginKimiManifestCmd.Flags().Set("write", "true"); err != nil {
		t.Fatal(err)
	}
	if err := pluginKimiManifestCmd.RunE(pluginKimiManifestCmd, nil); err != nil {
		t.Fatalf(`--write RunE: %v`, err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf(`manifest 应被创建: %v`, err)
	}
	var manifest struct {
		Name        string                       `json:"name"`
		Version     string                       `json:"version"`
		Description string                       `json:"description"`
		Hooks       []agentbridge.KimiPluginHook `json:"hooks"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf(`manifest 应为合法 JSON: %v`, err)
	}
	if manifest.Version != "9.9.9" {
		t.Errorf(`version = %q, want 9.9.9（npm/package.json 单一真相源）`, manifest.Version)
	}
	if manifest.Name != "forge" || manifest.Description != agentbridge.KimiPluginDescription {
		t.Errorf(`name/description 须与共享常量渲染一致, got %q / %q`, manifest.Name, manifest.Description)
	}
	specHooks := agentbridge.BuildKimiPluginHooks()
	if len(manifest.Hooks) != len(specHooks) {
		t.Errorf(`hooks 数 = %d, want %d（ForgeHookSpec 派生）`, len(manifest.Hooks), len(specHooks))
	}

	// (b) second --write on the just-written file: in-sync no-op, bytes unchanged.
	before := string(data)
	if err := pluginKimiManifestCmd.RunE(pluginKimiManifestCmd, nil); err != nil {
		t.Fatalf(`二次 --write RunE: %v`, err)
	}
	after, _ := os.ReadFile(manifestPath)
	if string(after) != before {
		t.Errorf(`in-sync 时不得改写字节（幂等契约被破坏）`)
	}

	// (c) default (report-only): file untouched on drift, exit 0.
	if err := os.WriteFile(manifestPath, []byte(`{"version":"0.0.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pluginKimiManifestCmd.Flags().Set("write", "false"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	pluginKimiManifestCmd.SetOut(&buf)
	if err := pluginKimiManifestCmd.RunE(pluginKimiManifestCmd, nil); err != nil {
		t.Fatalf(`report-only RunE（漂移也应退出 0）: %v`, err)
	}
	if !strings.Contains(buf.String(), `"version": "9.9.9"`) {
		t.Errorf(`默认运行应打印渲染的 manifest, got: %s`, buf.String())
	}
	drifted, _ := os.ReadFile(manifestPath)
	if string(drifted) != `{"version":"0.0.1"}` {
		t.Errorf(`report-only 不得改写文件, got: %s`, drifted)
	}
}

// TestPluginKimiManifestCmd_WriteGuard pins the L-1 --write rejection and the L-2
// non-NotExist read failure — both error paths were added with zero coverage (review
// finding, 2026-08-22): (a) a root with npm/package.json but NO go.mod and NO existing
// .kimi-plugin/plugin.json must be refused (the monorepo miswrite guard); (b) a
// manifest path that fails to read for a non-NotExist reason (a DIRECTORY at the
// manifest path — Windows read of a dir is ERROR_ACCESS_DENIED) must surface as an
// explicit error, never be conflated with drift. Report-only on such a root stays
// permissive (contract c of the main test).
//
// TestPluginKimiManifestCmd_WriteGuard 钉 L-1 的 --write 拒绝与 L-2 的非 NotExist
// 读取失败——两条错误路径落地时零覆盖（复审发现，2026-08-22）：(a) 有
// npm/package.json 但无 go.mod 且无既有 .kimi-plugin/plugin.json 的根必须被拒
// （monorepo 误写守卫）；(b) manifest 路径因非 NotExist 原因读失败（把 manifest
// 路径做成目录——Windows 读目录报 ERROR_ACCESS_DENIED）必须显式报错，绝不与
// 漂移混同。这种根上的 report-only 保持宽容（主测试的契约 c）。
func TestPluginKimiManifestCmd_WriteGuard(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "npm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "npm", "package.json"), []byte(`{"version":"9.9.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deliberately NO go.mod — this is the "user monorepo with an npm/ subdir" shape.
	t.Chdir(root)
	pluginKimiManifestCmd.SetOut(os.Stderr)

	// (a) --write with no forge-repo markers: refused with guidance, nothing written.
	if err := pluginKimiManifestCmd.Flags().Set("write", "true"); err != nil {
		t.Fatal(err)
	}
	err := pluginKimiManifestCmd.RunE(pluginKimiManifestCmd, nil)
	if err == nil {
		t.Fatal(`无 go.mod 且无既有 manifest 的根必须拒绝 --write（防误写非 forge 仓库）`)
	}
	if !strings.Contains(err.Error(), `拒绝 --write`) {
		t.Errorf(`拒绝文案须指明是 --write 守卫，got: %v`, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".kimi-plugin")); !os.IsNotExist(statErr) {
		t.Errorf(`被拒时不得创建 .kimi-plugin 目录，stat err = %v`, statErr)
	}

	// (b) manifest path unreadable for a non-NotExist reason: explicit error naming
	// the read, never "已提交文件漂移". A directory at the path yields
	// ERROR_ACCESS_DENIED on Windows / EISDIR-adjacent on Unix — both non-NotExist.
	if err := os.MkdirAll(filepath.Join(root, ".kimi-plugin", "plugin.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pluginKimiManifestCmd.Flags().Set("write", "false"); err != nil {
		t.Fatal(err)
	}
	runErr := pluginKimiManifestCmd.RunE(pluginKimiManifestCmd, nil)
	if runErr == nil {
		t.Fatal(`非 NotExist 读取失败必须显式报错（不得伪装成漂移/健康）`)
	}
	if !strings.Contains(runErr.Error(), `read committed manifest`) {
		t.Errorf(`报错须指明是已提交 manifest 的读取失败，got: %v`, runErr)
	}
}
