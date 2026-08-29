package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// generateCodeBuddyPack 把一个 codebuddy plugin pack 写进临时目录（空 description 触发
// DefaultPluginDescription 回落）并返回该目录。
func generateCodeBuddyPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := GenerateCodeBuddyPluginPack(dir, ""); err != nil {
		t.Fatalf("GenerateCodeBuddyPluginPack: %v", err)
	}
	return dir
}

// setupWorkBuddyEnv 把 WorkBuddy 配置目录与 forge 数据根隔离进 temp dir（strip 的
// 各 seam 两者都碰），返回它们。
func setupWorkBuddyEnv(t *testing.T) (wb, forgeHome string) {
	t.Helper()
	wb = t.TempDir()
	t.Setenv("WORKBUDDY_CONFIG_DIR", wb)
	forgeHome = t.TempDir()
	t.Setenv("FORGE_DATA_HOME", forgeHome)
	return wb, forgeHome
}

// TestCodeBuddyHooksPayload_MirrorsSpec: BuildCodeBuddyHooksPayload must equal ForgeHookSpec (under one "hooks" key) with exactly ONE rewrite — every command gains the `--agent codebuddy` suffix (attribution identity; see BuildCodeBuddyHooksPayload).
//
// TestCodeBuddyHooksPayload_MirrorsSpec：BuildCodeBuddyHooksPayload 必须等于
// ForgeHookSpec（包在一层 "hooks" key 下）且只做一处改写——每条命令加 `--agent
// codebuddy` 后缀（归因身份；见 BuildCodeBuddyHooksPayload）。两者 marshal 成 JSON
// 比对，抓住任何人手维护并行名册的 drift。（encoding/json 对 map 按 sorted key
// 序列化 → 稳定比对。）
func TestCodeBuddyHooksPayload_MirrorsSpec(t *testing.T) {
	payload := BuildCodeBuddyHooksPayload()
	a, _ := json.Marshal(payload.Hooks)
	spec := hooks.ForgeHookSpec()
	withAgent := make(map[string][]hooks.HookMatcher, len(spec))
	for event, matchers := range spec {
		ms := make([]hooks.HookMatcher, len(matchers))
		for i, m := range matchers {
			entries := make([]hooks.HookEntry, len(m.Hooks))
			for j, h := range m.Hooks {
				entries[j] = hooks.HookEntry{Type: h.Type, Command: h.Command + " --agent codebuddy"}
			}
			ms[i] = hooks.HookMatcher{Matcher: m.Matcher, Hooks: entries}
		}
		withAgent[event] = ms
	}
	b, _ := json.Marshal(withAgent)
	if string(a) != string(b) {
		t.Fatalf("codebuddy hooks payload drifted from ForgeHookSpec (+ --agent codebuddy):\n payload: %s\n spec:    %s", a, b)
	}
}

// TestCodeBuddyPluginPack_HooksMirrorSettings: the generated hooks.json must equal the hooks the ForgeHookSpec fixture writes to settings.local.json — single-source-of-truth guard (same shape as TestPluginPack_HooksMirrorSettings), end-to-end across two real files.
//
// TestCodeBuddyPluginPack_HooksMirrorSettings：生成的 hooks.json 必须等于 ForgeHookSpec fixture
// 写到 settings.local.json 的 hooks——单一真相源守卫（与 TestPluginPack_HooksMirrorSettings
// 同形），跨两个真实文件端到端比对。CodeBuddy 命令带 `--agent codebuddy` 归因后缀
// （BuildCodeBuddyHooksPayload），故比对前从 codebuddy 侧剥掉该唯一后缀再 marshal。
func TestCodeBuddyPluginPack_HooksMirrorSettings(t *testing.T) {
	sdir := t.TempDir()
	writeClaudeSettingsFixture(t, sdir)
	var settings map[string]any
	loadJSON(t, filepath.Join(sdir, ".claude", "settings.local.json"), &settings)

	pdir := generateCodeBuddyPack(t)
	var hj map[string]any
	loadJSON(t, filepath.Join(pdir, "plugins", "forge", "hooks", "hooks.json"), &hj)

	// 剥掉归因后缀，使比对聚焦接线名册本身（哪个 hook 接在哪个 event/matcher），
	// 而非那一处已知改写。
	if hooksMap, ok := hj["hooks"].(map[string]any); ok {
		for _, matchers := range hooksMap {
			for _, m := range matchers.([]any) {
				for _, h := range m.(map[string]any)["hooks"].([]any) {
					entry := h.(map[string]any)
					if cmd, ok := entry["command"].(string); ok {
						entry["command"] = strings.TrimSuffix(cmd, " --agent codebuddy")
					}
				}
			}
		}
	}

	a, _ := json.Marshal(settings["hooks"])
	b, _ := json.Marshal(hj["hooks"])
	if string(a) != string(b) {
		t.Errorf("codebuddy hooks.json != settings.local.json hooks (drift):\n settings: %s\n codebuddy: %s", a, b)
	}
}

// TestCodeBuddyPluginPack_WritesAllFiles: all expected files generated.
//
// TestCodeBuddyPluginPack_WritesAllFiles：所有预期文件都生成。
func TestCodeBuddyPluginPack_WritesAllFiles(t *testing.T) {
	dir := generateCodeBuddyPack(t)
	for _, rel := range []string{
		".codebuddy-plugin/marketplace.json",
		"plugins/forge/.codebuddy-plugin/plugin.json",
		"plugins/forge/hooks/hooks.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected file missing: %s (%v)", rel, err)
		}
	}
}

// TestCodeBuddyPluginPack_Manifest: marketplace.json + plugin.json structure correct — marketplace name=forge-local with one plugin source=./plugins/forge; plugin name=forge, hooks=./hooks/hooks.json (relative path, NOT an inline object — CodeBuddy loads hooks via the path, verified in its ppt-implement plugin).
//
// TestCodeBuddyPluginPack_Manifest：marketplace.json + plugin.json 结构正确——marketplace
// name=forge-local、唯一 plugin source=./plugins/forge；plugin name=forge、hooks=./hooks/hooks.json
// （相对路径，非内联对象——CodeBuddy 经此路径加载 hooks，已在其 ppt-implement plugin 验证）。
func TestCodeBuddyPluginPack_Manifest(t *testing.T) {
	dir := generateCodeBuddyPack(t)
	var mp codebuddyMarketplaceManifest
	loadJSON(t, filepath.Join(dir, ".codebuddy-plugin", "marketplace.json"), &mp)
	if mp.Name != codebuddyMarketplaceName {
		t.Errorf("marketplace name = %q, want %q", mp.Name, codebuddyMarketplaceName)
	}
	if len(mp.Plugins) != 1 || mp.Plugins[0].Name != codebuddyPluginName {
		t.Fatalf("marketplace plugins = %+v, want single %q", mp.Plugins, codebuddyPluginName)
	}
	if mp.Plugins[0].Source != "./plugins/"+codebuddyPluginName {
		t.Errorf("plugin source = %q, want ./plugins/%s", mp.Plugins[0].Source, codebuddyPluginName)
	}

	var pm codebuddyPluginManifest
	loadJSON(t, filepath.Join(dir, "plugins", "forge", ".codebuddy-plugin", "plugin.json"), &pm)
	if pm.Name != codebuddyPluginName {
		t.Errorf("plugin name = %q, want %q", pm.Name, codebuddyPluginName)
	}
	if pm.Hooks != "./hooks/hooks.json" {
		t.Errorf("plugin hooks = %q, want ./hooks/hooks.json (relative path, not inline object)", pm.Hooks)
	}
}

// TestCodeBuddyPluginPack_Idempotent: repeated generation does not error and yields a byte-identical hooks.json.
//
// TestCodeBuddyPluginPack_Idempotent：反复生成不报错且 hooks.json 字节一致。
func TestCodeBuddyPluginPack_Idempotent(t *testing.T) {
	dir := t.TempDir()
	var first, second []byte
	for i := 0; i < 2; i++ {
		if err := GenerateCodeBuddyPluginPack(dir, ""); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "plugins", "forge", "hooks", "hooks.json"))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = data
		} else {
			second = data
		}
	}
	if string(first) != string(second) {
		t.Errorf("idempotent run changed hooks.json:\n first:  %s\n second: %s", first, second)
	}
}

// TestCodeBuddyPluginPack_NoCurlyQuotes: regression guard for [[windows-input-quote-corruption]] — generated files must never contain curly quotes U+201C/U+201D.
//
// TestCodeBuddyPluginPack_NoCurlyQuotes：[[windows-input-quote-corruption]] 回归守卫——生成文件
// 绝不含弯引号 U+201C/U+201D。目标用 rune 构造，即使测试源码字面量被腐蚀断言仍成立。
func TestCodeBuddyPluginPack_NoCurlyQuotes(t *testing.T) {
	assertNoCurlyQuotes(t, generateCodeBuddyPack(t))
}

// TestParseAgentFlag_CodeBuddy_Explicit: --agents codebuddy resolves to [AgentCodeBuddy].
//
// TestParseAgentFlag_CodeBuddy_Explicit：--agents codebuddy 解析为 [AgentCodeBuddy]。
func TestParseAgentFlag_CodeBuddy_Explicit(t *testing.T) {
	agents := ParseAgentFlag("", "codebuddy")
	if len(agents) != 1 || agents[0] != AgentCodeBuddy {
		t.Fatalf("ParseAgentFlag(codebuddy) = %v, want [codebuddy]", agents)
	}
}

// TestParseAgentFlag_CodeBuddy_NotAutoDetected: auto-detection must NEVER include codebuddy — ~/.workbuddy always exists once WorkBuddy is installed, so auto-detect would wire codebuddy on every `forge init` on any WorkBuddy machine (the zcode trap, reverted 2026-08-03).
//
// TestParseAgentFlag_CodeBuddy_NotAutoDetected：auto 检测绝不包含 codebuddy——~/.workbuddy 装了
// WorkBuddy 就恒存在，auto-detect 会让任何 WorkBuddy 机器每次 `forge init` 都接 codebuddy
// （zcode 陷阱，2026-08-03 回滚）。此测试钉死该决策：codebuddy 仅经 --agents codebuddy 显式接入。
func TestParseAgentFlag_CodeBuddy_NotAutoDetected(t *testing.T) {
	for _, a := range ParseAgentFlag("", "auto") {
		if a == AgentCodeBuddy {
			t.Fatal("codebuddy must NOT be auto-detected (~/.workbuddy always-exists trap; use --agents codebuddy explicitly)")
		}
	}
}

// TestCodeBuddyTranslator_AgentType: the translator reports AgentCodeBuddy.
//
// TestCodeBuddyTranslator_AgentType：translator 报告 AgentCodeBuddy。
func TestCodeBuddyTranslator_AgentType(t *testing.T) {
	if (&CodeBuddyTranslator{}).AgentType() != AgentCodeBuddy {
		t.Fatal("CodeBuddyTranslator.AgentType() != AgentCodeBuddy")
	}
}

// TestCodeBuddyMarketplaceDir_RespectsDataHome: the asset dir lives under forge's global home and follows FORGE_DATA_HOME overrides (test isolation + the data-home refactor contract that all forge assets resolve through forgedata.GlobalHome).
//
// TestCodeBuddyMarketplaceDir_RespectsDataHome：资产目录在 forge 全局 home 下，并跟随
// FORGE_DATA_HOME 覆盖（测试隔离 + 所有 forge 资产经 forgedata.GlobalHome 解析的 data-home
// 重构契约）。
func TestCodeBuddyMarketplaceDir_RespectsDataHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", tmp)
	got, err := CodeBuddyMarketplaceDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, "agents", "codebuddy", codebuddyMarketplaceName)
	if got != want {
		t.Errorf("CodeBuddyMarketplaceDir = %q, want %q", got, want)
	}
}

// TestCodeBuddyRun_Command: [exe] form → exec.Command(exe, args...); [node, script] form → exec.Command(node, script, args...).
//
// TestCodeBuddyRun_Command：[exe] 形 → exec.Command(exe, args...)；[node, script] 形 →
// exec.Command(node, script, args...)。验证让 forge 经 node 解释器执行 WorkBuddy 裸 node CLI
// 的解释器/脚本拼接（WorkBuddy 发布 codebuddy 无 .cmd/.exe shim）。
func TestCodeBuddyRun_Command(t *testing.T) {
	r1 := codebuddyRun{argv: []string{`/usr/bin/codebuddy`}}
	cmd := r1.Command(`marketplaces`, `list`)
	if cmd.Path != `/usr/bin/codebuddy` {
		t.Errorf(`direct argv: cmd.Path = %q, want /usr/bin/codebuddy`, cmd.Path)
	}
	// cmd.Args[0] 是 Path；余下须为子命令参数（无前导脚本）。
	if len(cmd.Args) != 3 || cmd.Args[1] != `marketplaces` || cmd.Args[2] != `list` {
		t.Errorf(`direct argv: cmd.Args = %v, want [/usr/bin/codebuddy marketplaces list]`, cmd.Args)
	}

	// node 解释形：脚本路径须在 node 与子命令之间。
	r2 := codebuddyRun{argv: []string{`/usr/bin/node`, `/opt/codebuddy`}}
	cmd2 := r2.Command(`marketplaces`, `list`)
	wantArgs := []string{`/usr/bin/node`, `/opt/codebuddy`, `marketplaces`, `list`}
	if len(cmd2.Args) != len(wantArgs) {
		t.Fatalf(`node argv: len(cmd.Args) = %d, want %d (%v)`, len(cmd2.Args), len(wantArgs), cmd2.Args)
	}
	for i, w := range wantArgs {
		if cmd2.Args[i] != w {
			t.Errorf(`node argv: cmd.Args[%d] = %q, want %q`, i, cmd2.Args[i], w)
		}
	}
}

// TestIsWindowsExecutable: PATHEXT extensions (.exe/.cmd/.bat/.com, case-insensitive) accepted; bare scripts and unrelated extensions rejected.
//
// TestIsWindowsExecutable：PATHEXT 扩展名（.exe/.cmd/.bat/.com，大小写不敏感）接受；裸脚本与
// 无关扩展名拒绝。正是此守卫阻止 FindCodeBuddyCLI 把 WorkBuddy 的裸 node 脚本当"直接可执行"
// 返回（那会让 exec 报 "executable file not found in %PATH%"）。
func TestIsWindowsExecutable(t *testing.T) {
	for _, p := range []string{`codebuddy.exe`, `cbc.CMD`, `x.bat`, `y.com`} {
		if !isWindowsExecutable(p) {
			t.Errorf(`isWindowsExecutable(%q) = false, want true`, p)
		}
	}
	for _, p := range []string{`codebuddy`, `codebuddy.js`, `codebuddy.txt`, ``} {
		if isWindowsExecutable(p) {
			t.Errorf(`isWindowsExecutable(%q) = true, want false`, p)
		}
	}
}

// TestCodeBuddyWorkBuddyHome: WORKBUDDY_CONFIG_DIR wins when set (incl. over whitespace); otherwise the home defaults to ~/.workbuddy (the app's read location).
//
// TestCodeBuddyWorkBuddyHome：设了 WORKBUDDY_CONFIG_DIR 就胜出（含覆盖纯空白）；否则 home 默认
// ~/.workbuddy（app 读取处）。这是配置分离修复——没它 CLI 写 ~/.codebuddy，app 从不加载 plugin。
func TestCodeBuddyWorkBuddyHome(t *testing.T) {
	t.Setenv(`WORKBUDDY_CONFIG_DIR`, `/custom/wb`)
	got, err := codebuddyWorkBuddyHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != `/custom/wb` {
		t.Errorf(`WORKBUDDY_CONFIG_DIR set: got %q, want /custom/wb`, got)
	}
	// whitespace-only = unset → default.
	t.Setenv(`WORKBUDDY_CONFIG_DIR`, `   `)
	got, err = codebuddyWorkBuddyHome()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, `.workbuddy`) {
		t.Errorf(`default home should end in .workbuddy, got %q`, got)
	}
}

// TestWithCodeBuddyConfigDir: the override replaces any existing CODEBUDDY_CONFIG_DIR (no duplicate), appends the new value, and leaves other env entries in place.
//
// TestWithCodeBuddyConfigDir：覆盖替换任何已有 CODEBUDDY_CONFIG_DIR（无重复），追加新值，其余 env
// 原位保留。
func TestWithCodeBuddyConfigDir(t *testing.T) {
	got := withCodeBuddyConfigDir(
		[]string{`PATH=/usr/bin`, `CODEBUDDY_CONFIG_DIR=/old`, `CODEBUDDY_CONFIG_DIR_BACKUP=/keep`, `HOME=/root`},
		`/new`)
	last := got[len(got)-1]
	if last != `CODEBUDDY_CONFIG_DIR=/new` {
		t.Errorf(`override should be appended last, got %q`, last)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, `CODEBUDDY_CONFIG_DIR_BACKUP=/keep`) {
		t.Errorf(`sibling key CODEBUDDY_CONFIG_DIR_BACKUP must survive exact-key strip, got %s`, joined)
	}
	if strings.Contains(joined, `CODEBUDDY_CONFIG_DIR=/old`) {
		t.Errorf(`old CODEBUDDY_CONFIG_DIR must be replaced, got %s`, joined)
	}
	exact := 0
	for _, kv := range got {
		if k, _, ok := strings.Cut(kv, `=`); ok && k == `CODEBUDDY_CONFIG_DIR` {
			exact++
		}
	}
	if exact != 1 {
		t.Errorf(`expected exactly 1 exact CODEBUDDY_CONFIG_DIR key, got %d (%v)`, exact, got)
	}
}

// TestCodeBuddyTranslator_Translate: Translate must write all on-disk assets and return no error regardless of whether the CLI is found.
//
// TestCodeBuddyTranslator_Translate：无论 CLI 是否找到，Translate 必须写全部盘上资产且不报错。
// FORGE_DATA_HOME 隔离 pack 位置；WORKBUDDY_CONFIG_DIR 隔离 CLI 写入处（故 CLI 在的机器不污染真实
// ~/.workbuddy）。CLI 在时 plugin 必须注册进该隔离 home。
func TestCodeBuddyTranslator_Translate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, tmp)
	t.Setenv(`WORKBUDDY_CONFIG_DIR`, tmp)
	if err := (&CodeBuddyTranslator{}).Translate(``, nil); err != nil {
		t.Fatalf(`Translate returned error: %v`, err)
	}
	dir := filepath.Join(tmp, `agents`, `codebuddy`, codebuddyMarketplaceName)
	for _, rel := range []string{
		filepath.Join(`.codebuddy-plugin`, `marketplace.json`),
		filepath.Join(`plugins`, codebuddyPluginName, `.codebuddy-plugin`, `plugin.json`),
		filepath.Join(`plugins`, codebuddyPluginName, `hooks`, `hooks.json`),
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf(`asset missing after Translate: %s (%v)`, rel, err)
		}
	}
	// If the CLI was found, registration must land in the isolated home (proving the config-dir
	// redirect works end-to-end). CLI-absent hosts skip this assertion.
	if data, err := os.ReadFile(filepath.Join(tmp, `plugins`, `known_marketplaces.json`)); err == nil {
		if !strings.Contains(string(data), codebuddyMarketplaceName) {
			t.Errorf(`CLI present but %s not registered in isolated home: %s`, codebuddyMarketplaceName, data)
		}
	}
}

// TestGenerateCodeBuddyPluginPack_EmptyDir: empty dir is rejected (guard against silent writes to cwd).
//
// TestGenerateCodeBuddyPluginPack_EmptyDir：空 dir 被拒（防静默写进 cwd）。
func TestGenerateCodeBuddyPluginPack_EmptyDir(t *testing.T) {
	if err := GenerateCodeBuddyPluginPack(``, ``); err == nil {
		t.Fatal(`GenerateCodeBuddyPluginPack("") should error on empty dir`)
	}
}

// TestCodeBuddyRun_Command_EmptyArgv: a zero-value codebuddyRun must not panic — it yields a Cmd whose Start fails clearly (defensive guard for an unchecked FindCodeBuddyCLI error).
//
// TestCodeBuddyRun_Command_EmptyArgv：零值 codebuddyRun 不得 panic——产出 Start 会明确失败的 Cmd
// （FindCodeBuddyCLI 错误未检查的防御守卫）。
func TestCodeBuddyRun_Command_EmptyArgv(t *testing.T) {
	r := codebuddyRun{}
	cmd := r.Command(`plugin`, `list`)
	if cmd == nil {
		t.Fatal(`empty argv: Command returned nil`)
	}
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Kill()
		t.Errorf(`empty argv: expected Start to fail, got nil`)
	}
}

// TestStripCodeBuddyHooks guards the uninstall counterpart of the CodeBuddy wiring: three seams removed surgically (marketplace entry, enabledPlugins key, forge asset dir), user content preserved, emptied enabledPlugins kept as a shell, idempotent re-run, and malformed config fails WITHOUT touching the file.
//
// TestStripCodeBuddyHooks 守卫 CodeBuddy 接线的 uninstall 对应面：三处外科式移除
// （marketplace 条目、enabledPlugins 键、forge 资产目录），用户内容保留，删空的
// enabledPlugins 保留空壳，幂等重跑，malformed 配置报错且不碰文件。补 2026-08
// 卸载审计缺口：codebuddy 此前不在 2c strip 名册，`forge uninstall` 后 WorkBuddy
// 仍持有指向已删二进制的目录指针。
func TestStripCodeBuddyHooks(t *testing.T) {
	writeFile := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("full wiring", func(t *testing.T) {
		wb, fh := setupWorkBuddyEnv(t)

		kmPath := filepath.Join(wb, "plugins", "known_marketplaces.json")
		writeFile(t, kmPath, `{
  "user-market": {"source": "github", "repo": "someone/else"},
  "forge-local": {"source": "directory", "path": "somewhere"}
}`)
		setPath := filepath.Join(wb, "settings.json")
		writeFile(t, setPath, `{
  "theme": "dark",
  "enabledPlugins": {"other@market": true, "forge@forge-local": true}
}`)
		assets := filepath.Join(fh, "agents", "codebuddy", codebuddyMarketplaceName)
		writeFile(t, filepath.Join(assets, "marketplace.json"), `{}`)

		changed, err := StripCodeBuddyHooks()
		if err != nil || !changed {
			t.Fatalf("首次 strip = (%v, %v)，want (true, nil)", changed, err)
		}

		var km map[string]json.RawMessage
		data, _ := os.ReadFile(kmPath)
		if err := json.Unmarshal(data, &km); err != nil {
			t.Fatalf("strip 后 known_marketplaces.json 须仍是合法 JSON: %v", err)
		}
		if _, ok := km[codebuddyMarketplaceName]; ok {
			t.Errorf("forge-local 条目应被删除: %s", data)
		}
		if _, ok := km["user-market"]; !ok {
			t.Errorf("用户 marketplace 条目必须保留: %s", data)
		}

		var settings struct {
			Theme          string                     `json:"theme"`
			EnabledPlugins map[string]json.RawMessage `json:"enabledPlugins"`
		}
		data, _ = os.ReadFile(setPath)
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("strip 后 settings.json 须仍是合法 JSON: %v", err)
		}
		key := codebuddyPluginName + "@" + codebuddyMarketplaceName
		if _, ok := settings.EnabledPlugins[key]; ok {
			t.Errorf("enabledPlugins[%s] 应被删除: %s", key, data)
		}
		if _, ok := settings.EnabledPlugins["other@market"]; !ok {
			t.Errorf("用户插件条目必须保留: %s", data)
		}
		if settings.Theme != "dark" {
			t.Errorf("无关 settings 字段必须保留: %s", data)
		}
		if _, err := os.Stat(assets); !os.IsNotExist(err) {
			t.Errorf("forge 资产目录应被删除，stat err = %v", err)
		}

		// 幂等：重跑为干净 no-op，用户内容不动。
		changed, err = StripCodeBuddyHooks()
		if err != nil || changed {
			t.Fatalf("二次 strip = (%v, %v)，want (false, nil)", changed, err)
		}
	})

	t.Run("empty enabledPlugins shell", func(t *testing.T) {
		wb, _ := setupWorkBuddyEnv(t)
		setPath := filepath.Join(wb, "settings.json")
		writeFile(t, setPath, `{"enabledPlugins": {"forge@forge-local": true}}`)

		if _, err := StripCodeBuddyHooks(); err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		data, _ := os.ReadFile(setPath)
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		shell, ok := raw["enabledPlugins"]
		if !ok || strings.TrimSpace(string(shell)) != "{}" {
			t.Errorf("删空的 enabledPlugins 应保留空壳 {}，got %s", shell)
		}
	})

	t.Run("malformed config untouched", func(t *testing.T) {
		wb, _ := setupWorkBuddyEnv(t)
		kmPath := filepath.Join(wb, "plugins", "known_marketplaces.json")
		garbage := "{ not json"
		writeFile(t, kmPath, garbage)

		if _, err := StripCodeBuddyHooks(); err == nil {
			t.Fatal("malformed known_marketplaces.json 应报错而非静默吞掉")
		}
		data, _ := os.ReadFile(kmPath)
		if string(data) != garbage {
			t.Errorf("报错时不得改写用户文件，got %q", data)
		}
	})

	// malformed 用户配置不得挡住 seam 3（复审发现，2026-08-22）：即使
	// known_marketplaces.json 解析失败，forge 自有资产目录也要被删——提前 return
	// 的旧实现会让资产目录活过卸载，直到用户手修自己损坏的文件。
	t.Run("malformed config still removes forge assets", func(t *testing.T) {
		wb, fh := setupWorkBuddyEnv(t)
		kmPath := filepath.Join(wb, "plugins", "known_marketplaces.json")
		writeFile(t, kmPath, "{ not json")
		assets := filepath.Join(fh, "agents", "codebuddy", codebuddyMarketplaceName)
		writeFile(t, filepath.Join(assets, "marketplace.json"), `{}`)

		changed, err := StripCodeBuddyHooks()
		if err == nil {
			t.Fatal("seam1 解析失败必须上报（Join 后仍非 nil）")
		}
		if !changed {
			t.Fatal("seam3 删除了资产目录，changed 须为 true（部分进展须可见）")
		}
		if _, statErr := os.Stat(assets); !os.IsNotExist(statErr) {
			t.Errorf("seam1 失败时 forge 自有资产目录仍须被删除，stat err = %v", statErr)
		}
	})

	t.Run("clean env no-op", func(t *testing.T) {
		setupWorkBuddyEnv(t)
		changed, err := StripCodeBuddyHooks()
		if err != nil || changed {
			t.Fatalf("clean strip = (%v, %v)，want (false, nil)", changed, err)
		}
	})
}
