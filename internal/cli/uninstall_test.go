package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
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
	// codebuddy strip 上线后 RunE 会碰 WorkBuddy 配置——本测试未重定向 HOME，
	// 必须显式隔离 WORKBUDDY_CONFIG_DIR，否则在装有 WorkBuddy 的机器上跑测试
	// 会删真实接线（forge-local 条目 + enabledPlugins 键）。
	t.Setenv(`WORKBUDDY_CONFIG_DIR`, t.TempDir())
	// REASONIX_HOME 同理（复审 M，2026-08-22）：ReasonixConfigHome 回落
	// os.UserConfigDir()=%AppData%，HOME/USERPROFILE 重定向不覆盖——不隔离则
	// 2c 会清真实 reasonix settings.json、2e' 删其 forge-quality skill。
	t.Setenv(`REASONIX_HOME`, t.TempDir())

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

// isolateAllAgentHomes redirects every agent home (kimi/codex/XDG/claude/
// workbuddy/reasonix) plus HOME/USERPROFILE and FORGE_DATA_HOME to fresh temp
// dirs, and sets FORGE_UNINSTALL_SKIP_NPM — the full-RunE isolation the
// uninstall tests share. Returns the reasonix home, claude config dir,
// workbuddy config dir, and forge data home for the seams each test seeds.
//
// isolateAllAgentHomes 把每个 agent home（kimi/codex/XDG/claude/workbuddy/
// reasonix）连同 HOME/USERPROFILE 与 FORGE_DATA_HOME 重定向到全新 temp dir，
// 并置 FORGE_UNINSTALL_SKIP_NPM——uninstall 测试共享的全量 RunE 隔离。返回
// reasonix home、claude 配置目录、workbuddy 配置目录与 forge data home，供
// 各测试种各自的接缝。
func isolateAllAgentHomes(t *testing.T) (reasonixHome, claudeHome, wbHome, dataHome string) {
	t.Helper()
	t.Setenv(`FORGE_UNINSTALL_SKIP_NPM`, `1`)
	dataHome = t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, dataHome)
	t.Setenv(`KIMI_CODE_HOME`, t.TempDir())
	t.Setenv(`CODEX_HOME`, t.TempDir())
	t.Setenv(`XDG_CONFIG_HOME`, t.TempDir())
	claudeHome = t.TempDir()
	t.Setenv(`CLAUDE_CONFIG_DIR`, claudeHome)
	// WORKBUDDY_CONFIG_DIR 显式隔离：codebuddy strip 上线后，HOME 重定向不足以
	// 覆盖 WORKBUDDY_CONFIG_DIR 指向真实 WorkBuddy 安装的机器。
	wbHome = t.TempDir()
	t.Setenv(`WORKBUDDY_CONFIG_DIR`, wbHome)
	home := t.TempDir()
	t.Setenv(`HOME`, home)
	t.Setenv(`USERPROFILE`, home)
	// REASONIX_HOME 显式隔离（复审 M，2026-08-22）：ReasonixConfigHome 回落
	// os.UserConfigDir()=%AppData%，HOME/USERPROFILE 重定向不覆盖——不隔离则
	// 2c/2e' 的 reasonix 两条路径会碰真机配置。
	reasonixHome = t.TempDir()
	t.Setenv(`REASONIX_HOME`, reasonixHome)
	return
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
	reasonixHome, _, _, _ := isolateAllAgentHomes(t)

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
	// Full-RunE isolation; the claude seam is seeded below (REASONIX_HOME is
	// isolated too — 2c's reasonix strip and 2e's skill removal would otherwise
	// touch the real %AppData%\reasonix).
	//
	// 全量 RunE 隔离；claude 接缝在下面种入（REASONIX_HOME 同样隔离——否则 2c
	// 的 reasonix strip 与 2e' 的 skill 删除会碰真机 %AppData%\reasonix）。
	_, claudeHome, _, _ := isolateAllAgentHomes(t)

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
	reasonixHome, _, _, _ := isolateAllAgentHomes(t)

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

// TestUninstall_StripsCodeBuddyHooks pins the codebuddy wiring in the 2c strip roster:
// uninstall must reverse all three seams of the CodeBuddy/WorkBuddy plugin wiring
// (known_marketplaces.json 的 forge-local 条目、settings.json 的 enabledPlugins 键、
// forge 自有资产目录) while preserving user content, and print the removal line.
// Mirrors TestUninstall_StripsReasonixHooks; WORKBUDDY_CONFIG_DIR + FORGE_DATA_HOME
// isolation keeps RunE off the real WorkBuddy install.
//
// TestUninstall_StripsCodeBuddyHooks 钉死 codebuddy 进 2c strip 名册：uninstall 必须
// 反转 CodeBuddy/WorkBuddy plugin 接线的全部三处（known_marketplaces.json 的
// forge-local 条目、settings.json 的 enabledPlugins 键、forge 自有资产目录）同时
// 保留用户内容，并打印清除提示。镜像 TestUninstall_StripsReasonixHooks；
// WORKBUDDY_CONFIG_DIR + FORGE_DATA_HOME 隔离确保 RunE 不碰真实 WorkBuddy 安装。
func TestUninstall_StripsCodeBuddyHooks(t *testing.T) {
	_, _, wbHome, fh := isolateAllAgentHomes(t)

	// Seed both WorkBuddy configs with user content + forge wiring, plus the asset dir.
	kmPath := filepath.Join(wbHome, `plugins`, `known_marketplaces.json`)
	if err := os.MkdirAll(filepath.Dir(kmPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kmPath, []byte(`{
  "user-market": {"source": "github"},
  "forge-local": {"source": "directory", "path": "somewhere"}
}`), 0644); err != nil {
		t.Fatal(err)
	}
	setPath := filepath.Join(wbHome, `settings.json`)
	if err := os.WriteFile(setPath, []byte(`{
  "theme": "dark",
  "enabledPlugins": {"other@market": true, "forge@forge-local": true}
}`), 0644); err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(fh, `agents`, `codebuddy`, `forge-local`)
	if err := os.MkdirAll(assets, 0755); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := captureOutput(t, func() error {
		return uninstallCmd.RunE(uninstallCmd, nil)
	})
	if err != nil {
		t.Fatalf(`uninstall RunE: %v`, err)
	}

	data, _ := os.ReadFile(kmPath)
	if strings.Contains(string(data), `forge-local`) {
		t.Errorf(`known_marketplaces.json 应无 forge-local 残留，实得：`+"\n"+`%s`, data)
	}
	if !strings.Contains(string(data), `user-market`) {
		t.Errorf(`用户 marketplace 条目必须保留，实得：`+"\n"+`%s`, data)
	}
	data, _ = os.ReadFile(setPath)
	if strings.Contains(string(data), `forge@forge-local`) {
		t.Errorf(`settings.json 应无 forge@forge-local 残留，实得：`+"\n"+`%s`, data)
	}
	if !strings.Contains(string(data), `other@market`) || !strings.Contains(string(data), `dark`) {
		t.Errorf(`用户 settings 内容必须保留，实得：`+"\n"+`%s`, data)
	}
	if _, err := os.Stat(assets); !os.IsNotExist(err) {
		t.Errorf(`forge 资产目录应被删除，实得 stat err=%v`, err)
	}
	if !strings.Contains(stdout, `已清除 codebuddy 用户级配置中的 forge hooks`) {
		t.Errorf(`缺少 codebuddy hooks 清除提示，stdout：`+"\n"+`%s`, stdout)
	}
}

// TestUninstall_StripRosterPinned pins the 2c roster's key set (review M-1): every host
// wired with a user-level Strip function must appear in userLevelStripRoster. Each host
// is only guarded by its own TestUninstall_StripsXxxHooks — a host missing from the
// roster (the codebuddy gap, 2026-08-21) silently survived uninstall. Adding a host:
// update the expected set alongside the roster entry; forgetting both remains possible
// but now requires ignoring this test's name in the diff.
//
// TestUninstall_StripRosterPinned 钉死 2c 名册的 key 集合（评审 M-1）：每个有用户级
// Strip 函数的 host 都必须出现在 userLevelStripRoster。各 host 只被自己的
// TestUninstall_StripsXxxHooks 守卫——名册缺席的 host（codebuddy 缺口，2026-08-21）
// 会静默躲过卸载。新增 host：随名册条目同步更新期望集合；两处都忘仍可能，但现在
// 必须在 diff 里无视本测试的名字。
func TestUninstall_StripRosterPinned(t *testing.T) {
	want := []string{"cline", "codebuddy", "codex", "cursor", "opencode", "reasonix", "windsurf", "zcode"}
	got := make([]string, 0, len(userLevelStripRoster))
	for name := range userLevelStripRoster {
		got = append(got, name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("userLevelStripRoster keys = %v, want %v — 新 host 的 Strip 函数进名册了吗？（评审 M-1）", got, want)
	}
}

// TestRunUserLevelStrips_PartiaFailureReporting pins the L-4 contract through the
// hoisted 2c loop: a strip returning (true, err) reports BOTH the durable progress
// line (已部分清除) and the failure warning; (true, nil) reports full success;
// (false, nil) stays silent; a failing agent never aborts the remaining roster.
// Before the hoist this branch was a zero-execution dead path inside RunE (review
// finding, 2026-08-22) — only cline and codebuddy strips can produce (true, err)
// in production.
//
// TestRunUserLevelStrips_PartiaFailureReporting 经提升后的 2c 循环钉死 L-4 契约：
// strip 返回 (true, err) 时既报持久进展行（已部分清除）又报失败警告；(true, nil)
// 报完全成功；(false, nil) 静默；单个 agent 失败不中止名册其余部分。提升前该分支
// 是 RunE 内零执行的死路径（复审发现，2026-08-22）——生产里只有 cline 和
// codebuddy 的 strip 能产生 (true, err)。
func TestRunUserLevelStrips_PartiaFailureReporting(t *testing.T) {
	boom := errors.New(`seam2 write failed`)
	calls := 0
	roster := map[string]func() (bool, error){
		`agent-partial`: func() (bool, error) { calls++; return true, boom },
		`agent-full`:    func() (bool, error) { calls++; return true, nil },
		`agent-clean`:   func() (bool, error) { calls++; return false, nil },
	}

	stdout, stderr, err := captureOutput(t, func() error {
		runUserLevelStrips(roster)
		return nil
	})
	if err != nil {
		t.Fatalf(`runUserLevelStrips 不应返回错误（best-effort 契约）: %v`, err)
	}
	if calls != 3 {
		t.Fatalf(`全部 3 个 agent 都须被执行（失败不中止）, calls=%d`, calls)
	}
	if !strings.Contains(stdout, `已部分清除 agent-partial 用户级配置中的 forge hooks`) {
		t.Errorf(`(true, err) 须报「已部分清除」进展行, stdout=%q`, stdout)
	}
	if !strings.Contains(stderr, `警告：清理 agent-partial 用户级 hooks 失败：seam2 write failed`) {
		t.Errorf(`(true, err) 须同时报失败警告, stderr=%q`, stderr)
	}
	if !strings.Contains(stdout, `已清除 agent-full 用户级配置中的 forge hooks`) {
		t.Errorf(`(true, nil) 须报完全成功行, stdout=%q`, stdout)
	}
	if strings.Contains(stdout, `agent-clean`) {
		t.Errorf(`(false, nil) 无可报进展应静默, stdout=%q`, stdout)
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

// TestUninstall_GitHubChannel_ManualGuidanceNotNpm pins the install-channel split of
// the binary-removal step (review fix): a GitHub-release / manually-placed binary is
// NOT an npm package, so `npm uninstall -g` is a guaranteed failure — the step must
// print the resolved executable path + manual deletion guidance instead. PATH is
// isolated to a dir without npm so a regression to the npm branch surfaces as the
// 「npm 不可用」warning (asserted absent) rather than an actual global uninstall.
//
// TestUninstall_GitHubChannel_ManualGuidanceNotNpm 钉死二进制移除步骤的安装通道分流
// （审查修复）：GitHub Release/手动放置的二进制不是 npm 包，`npm uninstall -g` 注定
// 失败——该步骤必须打印解析后的可执行路径 + 手动删除指引。PATH 隔离到无 npm 的目录，
// 分流若退化回 npm 分支会浮出「npm 不可用」告警（断言其缺席）而非真的全局卸载。
func TestUninstall_GitHubChannel_ManualGuidanceNotNpm(t *testing.T) {
	orig := detectInstallChannelFn
	detectInstallChannelFn = func() installChannel { return installChannel{kind: channelGitHub} }
	t.Cleanup(func() { detectInstallChannelFn = orig })

	// PATH isolation (no npm inside): regression to the npm branch fails LookPath
	// here instead of running a real global npm uninstall.
	//
	// PATH 隔离（目录内无 npm）：退化回 npm 分支时 LookPath 在此失败，
	// 而不是真的跑一次全局 npm uninstall。
	t.Setenv(`PATH`, t.TempDir())

	var out, errBuf bytes.Buffer
	uninstallBinaryStep(&out, &errBuf)

	if want, err := getExecutablePath(); err == nil {
		if !strings.Contains(out.String(), want) {
			t.Errorf(`GitHub 通道应打印解析后的二进制实际路径 %s, stdout=%q`, want, out.String())
		}
	} else {
		t.Fatalf(`getExecutablePath 在测试进程中不应失败: %v`, err)
	}
	if !strings.Contains(out.String(), `手动删除`) {
		t.Errorf(`GitHub 通道应打印手动删除指引, stdout=%q`, out.String())
	}
	if strings.Contains(errBuf.String(), `npm 不可用`) || strings.Contains(errBuf.String(), `npm uninstall 失败`) {
		t.Errorf(`GitHub 通道不得尝试 npm uninstall, stderr=%q`, errBuf.String())
	}
	if runtime.GOOS == `windows` && !strings.Contains(out.String(), `.exe.old`) {
		t.Errorf(`Windows 上应提示 .exe.old 残留清理, stdout=%q`, out.String())
	}
}

// TestUninstall_NpmChannel_RoutesToNpm pins the other half of the split: the npm
// channel keeps the existing npm-uninstall path. PATH isolation (no npm) makes the
// branch observable as the「npm 不可用」warning, and the manual guidance must NOT
// print for npm installs.
//
// TestUninstall_NpmChannel_RoutesToNpm 钉死分流的另一半：npm 通道保持现有 npm
// uninstall 路径。PATH 隔离（无 npm）让该分支以「npm 不可用」告警可观测，
// 且 npm 安装不得打印手动删除指引。
func TestUninstall_NpmChannel_RoutesToNpm(t *testing.T) {
	orig := detectInstallChannelFn
	detectInstallChannelFn = func() installChannel { return installChannel{kind: channelNPM, pm: "npm"} }
	t.Cleanup(func() { detectInstallChannelFn = orig })

	t.Setenv(`PATH`, t.TempDir())

	var out, errBuf bytes.Buffer
	uninstallBinaryStep(&out, &errBuf)

	if !strings.Contains(errBuf.String(), `npm 不可用`) {
		t.Errorf(`npm 通道应走 npm uninstall 分支（PATH 无 npm → 应打「npm 不可用」告警）, stderr=%q`, errBuf.String())
	}
	if strings.Contains(out.String(), `手动删除`) {
		t.Errorf(`npm 通道不应打印手动删除指引, stdout=%q`, out.String())
	}
}

// TestUninstall_SkipNpmHookSkipsWholeStep pins the test hook semantics post-split:
// FORGE_UNINSTALL_SKIP_NPM=1 skips the ENTIRE binary step for both channels (no npm
// attempt, no guidance printout) — the existing RunE tests rely on this contract.
//
// TestUninstall_SkipNpmHookSkipsWholeStep 钉死分流后的测试钩子语义：
// FORGE_UNINSTALL_SKIP_NPM=1 对两个通道都跳过整个二进制步骤（不试 npm、不打印指引）
// ——既有 RunE 测试依赖该契约。
func TestUninstall_SkipNpmHookSkipsWholeStep(t *testing.T) {
	t.Setenv(`FORGE_UNINSTALL_SKIP_NPM`, `1`)
	for _, ch := range []installChannel{
		{kind: channelGitHub},
		{kind: channelNPM, pm: "npm"},
	} {
		orig := detectInstallChannelFn
		detectInstallChannelFn = func() installChannel { return ch }
		t.Cleanup(func() { detectInstallChannelFn = orig })

		var out, errBuf bytes.Buffer
		uninstallBinaryStep(&out, &errBuf)
		if out.Len() != 0 || errBuf.Len() != 0 {
			t.Errorf(`SKIP_NPM=1 时通道 %v 应整体静默, stdout=%q stderr=%q`, ch.kind, out.String(), errBuf.String())
		}
	}
}
