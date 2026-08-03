package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// generateCodeBuddyPack writes a codebuddy plugin pack into a temp dir (empty description
// exercises the DefaultPluginDescription fallback) and returns the dir.
//
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

// TestCodeBuddyHooksPayload_MirrorsSpec: BuildCodeBuddyHooksPayload must equal ForgeHookSpec
// (under one "hooks" key) — CodeBuddy's hook protocol is byte-identical to Claude Code's, so
// the spec migrates verbatim. Marshalling both to JSON and comparing catches any drift if
// someone hand-maintains a parallel roster. (encoding/json sorts map keys → stable compare.)
//
// TestCodeBuddyHooksPayload_MirrorsSpec：BuildCodeBuddyHooksPayload 必须等于 ForgeHookSpec
// （包在一层 "hooks" key 下）——CodeBuddy 的 hook 协议与 Claude Code 字节一致，故 spec 原样
// 迁移。两者 marshal 成 JSON 比对，抓住任何人手维护并行名册的 drift。（encoding/json 对 map
// 按 sorted key 序列化 → 稳定比对。）
func TestCodeBuddyHooksPayload_MirrorsSpec(t *testing.T) {
	payload := BuildCodeBuddyHooksPayload()
	a, _ := json.Marshal(payload.Hooks)
	b, _ := json.Marshal(hooks.ForgeHookSpec())
	if string(a) != string(b) {
		t.Fatalf("codebuddy hooks payload drifted from ForgeHookSpec:\n payload: %s\n spec:    %s", a, b)
	}
}

// TestCodeBuddyPluginPack_HooksMirrorSettings: the generated hooks.json must equal the hooks
// GenerateSettings writes to settings.local.json — single-source-of-truth guard (same shape
// as TestPluginPack_HooksMirrorSettings), end-to-end across two real files.
//
// TestCodeBuddyPluginPack_HooksMirrorSettings：生成的 hooks.json 必须等于 GenerateSettings
// 写到 settings.local.json 的 hooks——单一真相源守卫（与 TestPluginPack_HooksMirrorSettings
// 同形），跨两个真实文件端到端比对。
func TestCodeBuddyPluginPack_HooksMirrorSettings(t *testing.T) {
	sdir := t.TempDir()
	if err := hooks.GenerateSettings(sdir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	var settings map[string]any
	loadJSON(t, filepath.Join(sdir, ".claude", "settings.local.json"), &settings)

	pdir := generateCodeBuddyPack(t)
	var hj map[string]any
	loadJSON(t, filepath.Join(pdir, "plugins", "forge", "hooks", "hooks.json"), &hj)

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

// TestCodeBuddyPluginPack_Manifest: marketplace.json + plugin.json structure correct —
// marketplace name=forge-local with one plugin source=./plugins/forge; plugin name=forge,
// hooks=./hooks/hooks.json (relative path, NOT an inline object — CodeBuddy loads hooks via
// the path, verified in its ppt-implement plugin).
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

// TestCodeBuddyPluginPack_Idempotent: repeated generation does not error and yields a
// byte-identical hooks.json.
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

// TestCodeBuddyPluginPack_NoCurlyQuotes: regression guard for [[windows-input-quote-corruption]] —
// generated files must never contain curly quotes U+201C/U+201D. Target built from runes so the
// assertion holds even if the test source literal is itself corrupted.
//
// TestCodeBuddyPluginPack_NoCurlyQuotes：[[windows-input-quote-corruption]] 回归守卫——生成文件
// 绝不含弯引号 U+201C/U+201D。目标用 rune 构造，即使测试源码字面量被腐蚀断言仍成立。
func TestCodeBuddyPluginPack_NoCurlyQuotes(t *testing.T) {
	dir := generateCodeBuddyPack(t)
	curly := string([]rune{0x201c, 0x201d})
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if strings.ContainsAny(string(data), curly) {
			t.Errorf("%s contains curly quotes (Windows input corruption)", info.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
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

// TestParseAgentFlag_CodeBuddy_NotAutoDetected: auto-detection must NEVER include codebuddy —
// ~/.workbuddy always exists once WorkBuddy is installed, so auto-detect would wire codebuddy
// on every `forge init` on any WorkBuddy machine (the zcode trap, reverted 2026-08-03). This
// test pins that decision: codebuddy is opt-in via --agents codebuddy only.
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

// TestCodeBuddyTranslator_Registered: AllTranslators includes CodeBuddyTranslator.
//
// TestCodeBuddyTranslator_Registered：AllTranslators 含 CodeBuddyTranslator。
func TestCodeBuddyTranslator_Registered(t *testing.T) {
	var found bool
	for _, tr := range AllTranslators() {
		if tr.AgentType() == AgentCodeBuddy {
			found = true
		}
	}
	if !found {
		t.Fatal("CodeBuddyTranslator not registered in AllTranslators")
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

// TestCodeBuddyMarketplaceDir_RespectsDataHome: the asset dir lives under forge's global home
// and follows FORGE_DATA_HOME overrides (test isolation + the data-home refactor contract that
// all forge assets resolve through forgedata.GlobalHome).
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

// TestCodeBuddyRun_Command: [exe] form → exec.Command(exe, args...); [node, script] form
// → exec.Command(node, script, args...). Verifies the interpreter/script splicing that lets
// forge exec WorkBuddy's bare node CLI via the node interpreter (WorkBuddy ships codebuddy
// with no .cmd/.exe shim).
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
	// cmd.Args[0] is Path; the rest must be the subcommand args (no leading script).
	//
	// cmd.Args[0] 是 Path；余下须为子命令参数（无前导脚本）。
	if len(cmd.Args) != 3 || cmd.Args[1] != `marketplaces` || cmd.Args[2] != `list` {
		t.Errorf(`direct argv: cmd.Args = %v, want [/usr/bin/codebuddy marketplaces list]`, cmd.Args)
	}

	// node-interpreted form: the script path must sit between node and the subcommand.
	//
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

// TestIsWindowsExecutable: PATHEXT extensions (.exe/.cmd/.bat/.com, case-insensitive)
// accepted; bare scripts and unrelated extensions rejected. This is the guard that stops
// FindCodeBuddyCLI from returning WorkBuddy's bare node script as "directly executable"
// (which would make exec fail with "executable file not found in %PATH%").
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
