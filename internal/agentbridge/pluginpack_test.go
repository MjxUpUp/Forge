package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// expectedPluginFiles 是 GeneratePluginPack 应生成的相对路径集（相对 RepoDir）。加新输出
// 文件忘加这里，TestPluginPack_WritesAllFiles 会漏检——故意列死，逼生成器与测试同步。
var expectedPluginFiles = []string{
	".claude-plugin/marketplace.json",
	".cursor-plugin/marketplace.json",
	"plugins/forge/.claude-plugin/plugin.json",
	"plugins/forge/.mcp.json",
	"plugins/forge/README.md",
}

// generatePack 生成一个默认 pack 到临时目录，返回该目录。
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
// 写到 settings.local.json 的 hooks 字段——单一真相源守卫。Generator 写的就是
// hooks.ForgeHookSpec()，settings 也是；若有人改 ForgeHookSpec 但 pluginpack 改用硬编码
// 副本，或反之，此测试抓住 drift。端到端比对（读两个真实文件，非函数返回值）。
func TestPluginPack_HooksMirrorSettings(t *testing.T) {
	// 写 settings.local.json（真实生成路径）
	sdir := t.TempDir()
	if err := hooks.GenerateSettings(sdir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	var settings map[string]any
	loadJSON(t, filepath.Join(sdir, ".claude", "settings.local.json"), &settings)

	// 生成 plugin pack，读 plugin.json
	pdir := generatePack(t)
	var manifest map[string]any
	loadJSON(t, filepath.Join(pdir, "plugins", "forge", ".claude-plugin", "plugin.json"), &manifest)

	// JSON 规范化比对（map 顺序无关）。
	a, _ := json.Marshal(settings["hooks"])
	b, _ := json.Marshal(manifest["hooks"])
	if string(a) != string(b) {
		t.Errorf("plugin.json hooks != settings.local.json hooks (single-source-of-truth drift):\n settings: %s\n plugin:   %s", a, b)
	}
}

// TestPluginPack_MCP：共享 .mcp.json 含 forge server（command=forge, args=[mcp,serve]），
// 与 writeClaudeMCP 同形——plugin 安装后 claude/codex 自动发现。
func TestPluginPack_MCP(t *testing.T) {
	dir := generatePack(t)
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, "plugins", "forge", ".mcp.json"), &cfg)
	srv := forgeServerAt(t, cfg, "mcpServers", "forge")
	if srv["command"] != "forge" {
		t.Errorf("mcp command = %v, want forge", srv["command"])
	}
	assertStringArgs(t, srv["args"], "mcp", "serve")
}

// TestPluginPack_Marketplace：两份 marketplace.json 结构正确——name=forge、唯一 plugin、
// source=./plugins/forge、省略 version（git SHA 驱动自动更新）。claude 与 cursor 两份格式
// 相同（仅目录不同），表驱动各测。
func TestPluginPack_Marketplace(t *testing.T) {
	dir := generatePack(t)
	for _, mp := range []string{".claude-plugin", ".cursor-plugin"} {
		var cfg map[string]any
		loadJSON(t, filepath.Join(dir, mp, "marketplace.json"), &cfg)
		if cfg["name"] != "forge" {
			t.Errorf("%s marketplace name = %v, want forge", mp, cfg["name"])
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
		// 省略 version：claude marketplace 用 commit SHA 驱动每次 commit 自动更新。
		// 出现 version 字段说明策略回退（forge v1.0 迭代期应用 SHA 自动更新，非显式 release）。
		if _, has := entry["version"]; has {
			t.Errorf("%s entry has version field (should omit for SHA-driven auto-update)", mp)
		}
		if _, has := cfg["version"]; has {
			t.Errorf("%s marketplace has version field", mp)
		}
	}
}

// TestPluginPack_OmitsOwnerWhenEmpty：DefaultPluginPack 的 OwnerName/Email 空，marketplace
// 与 plugin entry 应整体省略 owner/author 字段（而非写空对象）。
func TestPluginPack_OmitsOwnerWhenEmpty(t *testing.T) {
	dir := generatePack(t)
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".claude-plugin", "marketplace.json"), &cfg)
	if _, has := cfg["owner"]; has {
		t.Error("marketplace has owner field when OwnerName/Email empty (should omit)")
	}
	plugins, _ := cfg["plugins"].([]any)
	entry, _ := plugins[0].(map[string]any)
	if _, has := entry["author"]; has {
		t.Error("plugin entry has author field when OwnerName/Email empty (should omit)")
	}
}

// TestPluginPack_IncludesOwnerWhenSet：OwnerName/Email 非空时，marketplace owner 与
// plugin author 都带上（品牌化分发）。
func TestPluginPack_IncludesOwnerWhenSet(t *testing.T) {
	dir := t.TempDir()
	spec := DefaultPluginPack(dir)
	spec.OwnerName = "Alice"
	spec.OwnerEmail = "alice@example.com"
	if err := GeneratePluginPack(spec); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".claude-plugin", "marketplace.json"), &cfg)
	owner, _ := cfg["owner"].(map[string]any)
	if owner["name"] != "Alice" {
		t.Errorf("owner name = %v, want Alice", owner["name"])
	}
	plugins, _ := cfg["plugins"].([]any)
	entry, _ := plugins[0].(map[string]any)
	author, _ := entry["author"].(map[string]any)
	if author["email"] != "alice@example.com" {
		t.Errorf("author email = %v, want alice@example.com", author["email"])
	}
}

// TestPluginPack_Idempotent：反复生成不重复添加（plugin entry 不变成 2 个、文件仍合法）。
// forge plugin pack 反复跑（如 CI 每次 commit）必须安全。
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
// 绝不能含弯引号 U+201C/U+201D。用 \u 转义构造目标串（绕过测试源码双引号是否被腐蚀，与
// mcpconfig_test 的 rune 构造同理）。
func TestPluginPack_NoCurlyQuotes(t *testing.T) {
	dir := generatePack(t)
	curly := "“”"
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

// TestPluginPack_Readme：README 含每 host 的安装命令片段——仿 superpowers README 的"每 host
// 安装命令"表，是"什么都不懂的用户"照着装的关键。少了任一片段说明 README 漏了某个 host。
func TestPluginPack_Readme(t *testing.T) {
	dir := generatePack(t)
	content := readOrFail(t, filepath.Join(dir, "plugins", "forge", "README.md"))
	for _, want := range []string{
		"/plugin install forge@forge",
		"MjxUpUp/Forge",
		"forge init --agents codex",
		"forge init --agents cursor",
		"forge init --agents copilot",
		"Claude Code",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("README missing %q", want)
		}
	}
}
