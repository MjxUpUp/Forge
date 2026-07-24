package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// expectedPluginFiles 是 GeneratePluginPack(DefaultPluginPack) 应生成的相对路径集（相对
// RepoDir）。加新输出文件忘加这里，TestPluginPack_WritesAllFiles 会漏检——故意列死，逼生成器
// 与测试同步。路径含 "forge" 因 DefaultPluginPack.PluginName="forge"。
var expectedPluginFiles = []string{
	".claude-plugin/marketplace.json",
	".cursor-plugin/marketplace.json",
	"plugins/forge/.claude-plugin/plugin.json",
	"plugins/forge/README.md",
}

// generatePack 生成一个默认 pack 到临时目录，返回该目录。DefaultPluginPack 预填 owner=MjxUpUp
// 满足 schema required。
func generatePack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := GeneratePluginPack(DefaultPluginPack(dir)); err != nil {
		t.Fatalf("GeneratePluginPack: %v", err)
	}
	return dir
}

// TestPluginPack_WritesAllFiles：所有预期文件都生成。
func TestPluginPack_WritesAllFiles(t *testing.T) {
	dir := generatePack(t)
	for _, rel := range expectedPluginFiles {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected file missing: %s (%v)", rel, err)
		}
	}
}

// TestPluginPack_HooksMirrorSettings：plugin.json 的 hooks 字段必须等于 GenerateSettings
// 写到 settings.local.json 的 hooks 字段——单一真相源守卫。端到端比对（读两个真实文件，
// 非函数返回值）。若有人改 ForgeHookSpec 但 pluginpack 改用硬编码副本，此测试抓住 drift。
func TestPluginPack_HooksMirrorSettings(t *testing.T) {
	sdir := t.TempDir()
	if err := hooks.GenerateSettings(sdir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	var settings map[string]any
	loadJSON(t, filepath.Join(sdir, ".claude", "settings.local.json"), &settings)

	pdir := generatePack(t)
	var manifest map[string]any
	loadJSON(t, filepath.Join(pdir, "plugins", "forge", ".claude-plugin", "plugin.json"), &manifest)

	a, _ := json.Marshal(settings["hooks"])
	b, _ := json.Marshal(manifest["hooks"])
	if string(a) != string(b) {
		t.Errorf("plugin.json hooks != settings.local.json hooks (single-source-of-truth drift):\n settings: %s\n plugin:   %s", a, b)
	}
}

// TestPluginPack_Marketplace：两份 marketplace.json 结构正确——name=forge、owner 必有（schema
// required）、唯一 plugin、source=./plugins/forge（跟随 PluginName）、author 字段、省略 version。
func TestPluginPack_Marketplace(t *testing.T) {
	dir := generatePack(t)
	for _, mp := range []string{".claude-plugin", ".cursor-plugin"} {
		var cfg map[string]any
		loadJSON(t, filepath.Join(dir, mp, "marketplace.json"), &cfg)
		if cfg["name"] != "forge" {
			t.Errorf("%s marketplace name = %v, want forge", mp, cfg["name"])
		}
		// owner 是 claude marketplace schema 的 required 字段。
		owner, ok := cfg["owner"].(map[string]any)
		if !ok {
			t.Fatalf("%s marketplace missing required owner field (schema violation)", mp)
		}
		if owner["name"] != "MjxUpUp" {
			t.Errorf("%s owner.name = %v, want MjxUpUp", mp, owner["name"])
		}
		plugins, ok := cfg["plugins"].([]any)
		if !ok || len(plugins) != 1 {
			t.Fatalf("%s marketplace plugins not a 1-element array: %v", mp, cfg["plugins"])
		}
		entry, _ := plugins[0].(map[string]any)
		if entry["name"] != "forge" {
			t.Errorf("%s entry name = %v, want forge", mp, entry["name"])
		}
		if entry["source"] != "./plugins/forge" {
			t.Errorf("%s source = %v, want ./plugins/forge", mp, entry["source"])
		}
		// author 与 owner 同源（name 必有）。
		if _, has := entry["author"]; !has {
			t.Errorf("%s entry missing author field", mp)
		}
		// 省略 version：git SHA 驱动自动更新。
		if _, has := entry["version"]; has {
			t.Errorf("%s entry has version field (should omit for SHA-driven auto-update)", mp)
		}
		if _, has := cfg["version"]; has {
			t.Errorf("%s marketplace has version field", mp)
		}
	}
}

// TestPluginPack_OwnerIsRequired：OwnerName 空时 GeneratePluginPack 必须报错（claude marketplace
// schema 把 owner 标为 required，省略会让 `claude plugin validate` 拒载）。
func TestPluginPack_OwnerIsRequired(t *testing.T) {
	dir := t.TempDir()
	spec := DefaultPluginPack(dir)
	spec.OwnerName = ""
	err := GeneratePluginPack(spec)
	if err == nil {
		t.Fatal("GeneratePluginPack should error when OwnerName empty (claude marketplace schema required)")
	}
}

// TestPluginPack_CustomPluginName：非默认 PluginName 时，source 必须跟随（./plugins/<name>），
// plugin 树写到 plugins/<name>/。回归守卫 B1：pluginSource 曾硬编码 "./plugins/forge"，导致
// --plugin-name myforge 时 source 指向不存在的 ./plugins/forge，install 失败。
func TestPluginPack_CustomPluginName(t *testing.T) {
	dir := t.TempDir()
	spec := DefaultPluginPack(dir)
	spec.PluginName = "myforge"
	if err := GeneratePluginPack(spec); err != nil {
		t.Fatalf("GeneratePluginPack: %v", err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".claude-plugin", "marketplace.json"), &cfg)
	plugins, _ := cfg["plugins"].([]any)
	entry, _ := plugins[0].(map[string]any)
	if entry["source"] != "./plugins/myforge" {
		t.Errorf("source = %v, want ./plugins/myforge (B1: source must follow PluginName, was hardcoded)", entry["source"])
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "myforge", ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("plugin tree not written to plugins/myforge/: %v", err)
	}
	// plugins/forge/ 不应被创建（旧硬编码路径）
	if _, err := os.Stat(filepath.Join(dir, "plugins", "forge")); err == nil {
		t.Error("plugins/forge/ created despite PluginName=myforge (stale hardcoded path)")
	}
}

// TestPluginPack_OwnerWithEmail：OwnerEmail 非空时，owner/author 都带 email 字段（name 总在）。
func TestPluginPack_OwnerWithEmail(t *testing.T) {
	dir := t.TempDir()
	spec := DefaultPluginPack(dir) // OwnerName=MjxUpUp
	spec.OwnerEmail = "alice@example.com"
	if err := GeneratePluginPack(spec); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".claude-plugin", "marketplace.json"), &cfg)
	owner, _ := cfg["owner"].(map[string]any)
	if owner["email"] != "alice@example.com" {
		t.Errorf("owner email = %v, want alice@example.com", owner["email"])
	}
	plugins, _ := cfg["plugins"].([]any)
	entry, _ := plugins[0].(map[string]any)
	author, _ := entry["author"].(map[string]any)
	if author["email"] != "alice@example.com" {
		t.Errorf("author email = %v, want alice@example.com", author["email"])
	}
}

// TestPluginPack_Idempotent：反复生成不重复添加（plugin entry 不变成 2 个、文件仍合法）。
func TestPluginPack_Idempotent(t *testing.T) {
	dir := t.TempDir()
	spec := DefaultPluginPack(dir)
	for i := 0; i < 2; i++ {
		if err := GeneratePluginPack(spec); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".claude-plugin", "marketplace.json"), &cfg)
	plugins, _ := cfg["plugins"].([]any)
	if len(plugins) != 1 {
		t.Errorf("idempotent run duplicated plugin entries: %d (%v)", len(plugins), plugins)
	}
}

// TestPluginPack_NoCurlyQuotes：回归守卫 [[windows-input-quote-corruption]]——生成的所有文件
// 绝不能含弯引号 U+201C/U+201D。用 rune 构造目标串（绕过测试源码字面量是否被腐蚀）。
func TestPluginPack_NoCurlyQuotes(t *testing.T) {
	dir := generatePack(t)
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

// TestPluginPack_Readme：README 含三步首体验结构 + 每 host 安装命令 + Codex 路径未确认的诚实表述
// + npm 包名正确（@agent_forge/forge，与 npm/package.json 一致）+ 能力边界（每项目仍需 init）。
// 负向断言 @mjxupup/forge 抓历史回退：早期 pluginReadme 写过错用 GitHub owner slug 的包名。
func TestPluginPack_Readme(t *testing.T) {
	dir := generatePack(t)
	content := readOrFail(t, filepath.Join(dir, "plugins", "forge", "README.md"))
	for _, want := range []string{
		"Three-step setup",   // 三步首体验结构
		"@agent_forge/forge", // npm 包名（与 npm/package.json 一致）
		"once per machine",   // step 1：二进制是机器级硬前置
		"once per agent",     // step 2：plugin 是 agent 级
		"once per project",   // step 3：项目级资产每项目一次（能力边界）
		"/plugin install forge@forge",
		"MjxUpUp/Forge",
		"forge init --agents codex",
		"forge init --agents cursor",
		"forge init --agents copilot",
		"Claude Code",
		"not officially confirmed", // D3: Codex 路径诚实表述（OpenAI 未明确）
	} {
		if !strings.Contains(content, want) {
			t.Errorf("README missing %q", want)
		}
	}
	// 负向：旧错误包名不得重现（@mjxupup/forge 指向不存在的包）。
	if strings.Contains(content, "@mjxupup/forge") {
		t.Errorf("README references @mjxupup/forge (stale wrong package name; want @agent_forge/forge)")
	}
}

// TestPluginPack_CommittedManifestMatchesGenerator：committed 的 plugins/forge/.claude-plugin/
// plugin.json 的 hooks 字段必须等于 GeneratePluginPack 当前输出（ForgeHookSpec 派生）。
// TestPluginPack_HooksMirrorSettings 只守卫生成器内部一致（临时目录里 settings.local.json vs
// plugin.json，两者都从同一 ForgeHookSpec 派生），抓不住"改了 ForgeHookSpec 但忘记跑
// `forge plugin pack` 重新提交 plugin.json"的 drift——本测试直接读仓库里 committed 的
// plugin.json 对比生成器输出，确保提交的派生资产与代码同步。回归源：SessionStart 加了
// task-resume 到 ForgeHookSpec，但 committed plugin.json 漏重新生成（code-review P0-1）。
func TestPluginPack_CommittedManifestMatchesGenerator(t *testing.T) {
	committed := filepath.Join("..", "..", "plugins", "forge", ".claude-plugin", "plugin.json")
	if _, err := os.Stat(committed); err != nil {
		t.Skipf("committed plugin manifest not found at %s (non-forge repo layout): %v", committed, err)
	}
	generated := generatePack(t)
	var genManifest, committedManifest map[string]any
	loadJSON(t, filepath.Join(generated, "plugins", "forge", ".claude-plugin", "plugin.json"), &genManifest)
	loadJSON(t, committed, &committedManifest)
	a, _ := json.Marshal(genManifest["hooks"])
	b, _ := json.Marshal(committedManifest["hooks"])
	if string(a) != string(b) {
		t.Errorf("committed plugin.json hooks drifted from generator output (run `forge plugin pack` and commit the result):\n generated: %s\n committed: %s", a, b)
	}
}
