package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// expectedPluginFiles is the set of relative paths that GeneratePluginPack(DefaultPluginPack) should generate (relative to
// RepoDir). Forgetting to list a new output file here would let TestPluginPack_WritesAllFiles miss it — intentionally hardcoded to force the generator
// and tests to stay in sync. Paths contain `forge` because DefaultPluginPack.PluginName=`forge`.
//
// expectedPluginFiles 是 GeneratePluginPack(DefaultPluginPack) 应生成的相对路径集（相对
// RepoDir）。加新输出文件忘加这里，TestPluginPack_WritesAllFiles 会漏检——故意列死，逼生成器
// 与测试同步。路径含 "forge" 因 DefaultPluginPack.PluginName="forge"。
var expectedPluginFiles = []string{
	".claude-plugin/marketplace.json",
	".cursor-plugin/marketplace.json",
	"plugins/forge/.claude-plugin/plugin.json",
	"plugins/forge/reasonix-plugin.json",
	"plugins/forge/README.md",
}

// generatePack generates a default pack into a temp directory and returns it. DefaultPluginPack prefills owner=MjxUpUp
// to satisfy the schema required fields.
//
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

// TestPluginPack_WritesAllFiles: all expected files are generated.
//
// TestPluginPack_WritesAllFiles：所有预期文件都生成。
func TestPluginPack_WritesAllFiles(t *testing.T) {
	dir := generatePack(t)
	for _, rel := range expectedPluginFiles {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected file missing: %s (%v)", rel, err)
		}
	}
}

// TestPluginPack_HooksMirrorSettings: the hooks field of plugin.json must equal the hooks field that GenerateSettings
// writes to settings.local.json — a single-source-of-truth guard. End-to-end comparison (reading two real files,
// not function return values). If someone changes ForgeHookSpec but pluginpack switches to a hardcoded copy, this test catches the drift.
//
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

// TestPluginPack_Marketplace: both marketplace.json files have correct structure — name=forge, owner present (schema
// required), a single plugin, source=./plugins/forge (follows PluginName), author field, and version omitted.
//
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
		// owner is a required field of the claude marketplace schema.
		//
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
		// author shares the same source as owner (name is always present).
		//
		// author 与 owner 同源（name 必有）。
		if _, has := entry["author"]; !has {
			t.Errorf("%s entry missing author field", mp)
		}
		// version omitted: git SHA drives automatic updates.
		//
		// 省略 version：git SHA 驱动自动更新。
		if _, has := entry["version"]; has {
			t.Errorf("%s entry has version field (should omit for SHA-driven auto-update)", mp)
		}
		if _, has := cfg["version"]; has {
			t.Errorf("%s marketplace has version field", mp)
		}
	}
}

// TestPluginPack_OwnerIsRequired: GeneratePluginPack must error when OwnerName is empty (the claude marketplace
// schema marks owner as required; omitting it would make `claude plugin validate` reject loading).
//
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

// TestPluginPack_CustomPluginName: with a non-default PluginName, source must follow (./plugins/<name>),
// and the plugin tree is written to plugins/<name>/. Regression guard B1: pluginSource was once hardcoded to `./plugins/forge`, causing
// source to point at the nonexistent ./plugins/forge when --plugin-name myforge, failing install.
//
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
	// plugins/forge/ should not be created (stale hardcoded path).
	//
	// plugins/forge/ 不应被创建（旧硬编码路径）
	if _, err := os.Stat(filepath.Join(dir, "plugins", "forge")); err == nil {
		t.Error("plugins/forge/ created despite PluginName=myforge (stale hardcoded path)")
	}
}

// TestPluginPack_OwnerWithEmail: when OwnerEmail is non-empty, both owner and author carry an email field (name is always present).
//
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

// TestPluginPack_Idempotent: repeated generation does not duplicate entries (plugin entry stays at 1, files remain valid).
//
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

// TestPluginPack_NoCurlyQuotes: regression guard for [[windows-input-quote-corruption]] — all generated files
// must never contain curly quotes U+201C/U+201D. Target strings are built from runes (bypassing whether the test source literal is corrupted).
//
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

// TestPluginPack_Readme: README contains the three-step first-experience structure + per-host install commands + honest statement about unconfirmed Codex paths
// + correct npm package name (@agent_forge/forge, matching npm/package.json) + capability boundary (init still required per project).
// Negative assertion on @mjxupup/forge catches historical regression: early pluginReadme wrote the wrong package name using the GitHub owner slug.
//
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
		"Kimi Code",
		"/plugins install https://github.com/", // kimi plugin install（repo-root .kimi-plugin/plugin.json）
		"forge init --agents kimi",             // kimi 的 config.toml 回退路径
		"Claude Code",
		"Reasonix",
		"reasonix plugin install",      // reasonix native plugin（plugins/forge/reasonix-plugin.json）
		"forge init --agents reasonix", // reasonix 的 settings.json flat hooks 回退路径
		"not officially confirmed",     // D3: Codex 路径诚实表述（OpenAI 未明确）
	} {
		if !strings.Contains(content, want) {
			t.Errorf("README missing %q", want)
		}
	}
	// Negative: the old wrong package name must not reappear (@mjxupup/forge points to a nonexistent package).
	//
	// 负向：旧错误包名不得重现（@mjxupup/forge 指向不存在的包）。
	if strings.Contains(content, "@mjxupup/forge") {
		t.Errorf("README references @mjxupup/forge (stale wrong package name; want @agent_forge/forge)")
	}
	// Mojibake guard: the embedded template runs through fmt.Sprintf (pluginReadme interpolates the
	// repo slug), so a literal percent in the template (e.g. the Windows path %APPDATA%) is parsed as
	// a format verb and renders as "%!A(MISSING)...". The template escapes such literals as a
	// double-percent (rendering a single percent). Assert the rendered README carries the correct
	// Windows path and never the (MISSING) fmt-mojibake signature. Regression source: the reasonix
	// section's Windows path was eaten by fmt.Sprintf and shipped in v1.28.0.
	//
	// Mojibake 守卫：embed 模板经 fmt.Sprintf 渲染（pluginReadme 插值 repo slug），故模板里的字面量
	// 百分号（如 Windows 路径 %APPDATA%）会被当成格式动词渲染成 "%!A(MISSING)..."。模板把这类字面量
	// 转义成双百分号（渲染出单个百分号）。断言渲染后的 README 带正确的 Windows 路径，且永不出现
	// (MISSING) 这类 fmt 乱码签名。回归源：reasonix 段的 Windows 路径被 fmt.Sprintf 吃掉，随 v1.28.0 发布。
	if !strings.Contains(content, `%APPDATA%\reasonix`) {
		t.Errorf("README missing literal Windows path APPDATA (template must double-escape percent signs so fmt.Sprintf renders them): see plugin_readme.md reasonix section")
	}
	if strings.Contains(content, "(MISSING)") {
		t.Errorf("README contains fmt.Sprintf mojibake (MISSING) — a literal percent in the embedded template is being eaten as a format verb; escape it as a double-percent")
	}
}

// TestPluginPack_CommittedManifestMatchesGenerator: the hooks field of the committed plugins/forge/.claude-plugin/
// plugin.json must equal the current output of GeneratePluginPack (derived from ForgeHookSpec).
// TestPluginPack_HooksMirrorSettings only guards internal generator consistency (settings.local.json vs
// plugin.json in the temp dir, both derived from the same ForgeHookSpec); it cannot catch the drift of changing ForgeHookSpec but forgetting to run
// `forge plugin pack` to re-commit plugin.json — this test directly reads the repo's committed
// plugin.json and compares it against generator output, ensuring committed derived assets stay in sync with code. Regression source: SessionStart added
// task-resume to ForgeHookSpec, but the committed plugin.json was not regenerated (code-review P0-1).
//
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

// TestPluginPack_Readme_UserLevelContract pins the v1.22 user-level wording in the
// plugin README: zero project writes since v1.22, and the uninstall --restore
// rollback path (guards the plugin_readme.go capability-boundary comment contract).
//
// TestPluginPack_Readme_UserLevelContract 钉死 plugin README 的 v1.22 用户级表述：
// v1.22 起零项目写入 + uninstall --restore 回滚路径（守卫 plugin_readme.go
// 能力边界注释契约）。
func TestPluginPack_Readme_UserLevelContract(t *testing.T) {
	dir := generatePack(t)
	content := readOrFail(t, filepath.Join(dir, "plugins", "forge", "README.md"))
	for _, want := range []string{
		"Since v1.22 `forge init` writes nothing into the project",
		"--restore",
		"zero project writes",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("README missing user-level wording %q", want)
		}
	}
}

// TestPluginPack_ReasonixManifestHooksMirror: the hooks field of reasonix-plugin.json must equal the
// flat hooks shape buildReasonixHooks produces (the same one reasonix's Translate writes into
// settings.json). reasonix is the 5th host: its Claude compatibility does NOT resolve
// .claude-plugin/plugin.json's nested hooks (empirically rejected), so a NATIVE flat manifest is
// required — and it must mirror the settings.json path's single source of truth or `reasonix
// plugin install` and `forge init --agents reasonix` would wire different gates. Also pins the
// native manifest identity fields (apiVersion/name) so a future rename drift is caught.
//
// TestPluginPack_ReasonixManifestHooksMirror：reasonix-plugin.json 的 hooks 字段必须等于
// buildReasonixHooks 产出的扁平 hooks 形态（与 reasonix 的 Translate 写进 settings.json 的相同）。
// reasonix 是第 5 host：其 Claude 兼容不解析 .claude-plugin/plugin.json 的嵌套 hooks（实测被拒），
// 故需 NATIVE 扁平 manifest——且它必须镜像 settings.json 路径的单一真相源，否则
// `reasonix plugin install` 与 `forge init --agents reasonix` 会接不同的 gate。同时钉住 native
// manifest 标识字段（apiVersion/name），以抓将来的改名 drift。
func TestPluginPack_ReasonixManifestHooksMirror(t *testing.T) {
	pdir := generatePack(t)
	var manifest map[string]any
	loadJSON(t, filepath.Join(pdir, "plugins", "forge", "reasonix-plugin.json"), &manifest)
	// Native manifest identity fields.
	if manifest["apiVersion"] != "reasonix.io/plugin/v1" {
		t.Errorf("reasonix apiVersion = %v, want reasonix.io/plugin/v1 (native reasonix plugin manifest)", manifest["apiVersion"])
	}
	if manifest["name"] != "forge" {
		t.Errorf("reasonix manifest name = %v, want forge", manifest["name"])
	}
	// hooks field == buildReasonixHooks flat shape (single source of truth shared with the
	// settings.json path). End-to-end comparison: read the generated file, marshal the function
	// output writeReasonixPluginManifest and reasonix settings.json both consume. Both sides are
	// round-tripped through map[string]any so struct-declaration-order (match,command) vs
	// alphabetical-map-key-order (command,match) differences don't masquerade as drift —
	// loadJSON yields map[string]any (alphabetical keys), a direct struct marshal yields
	// declaration-order keys; same data, different string.
	a, _ := json.Marshal(manifest["hooks"])
	builtRaw, _ := json.Marshal(buildReasonixHooks()["hooks"])
	var builtNorm any
	if err := json.Unmarshal(builtRaw, &builtNorm); err != nil {
		t.Fatalf("normalize built hooks: %v", err)
	}
	b, _ := json.Marshal(builtNorm)
	if string(a) != string(b) {
		t.Errorf("reasonix-plugin.json hooks != buildReasonixHooks output (single-source-of-truth drift between plugin manifest and settings.json path):\n manifest: %s\n built:    %s", a, b)
	}
	// Sanity: the flat reasonix entry shape must be present (match, not matcher; bare command,
	// no type wrapper) — guards against accidentally reusing the claude nested shape.
	if strings.Contains(string(a), `"matcher"`) || strings.Contains(string(a), `"type"`) {
		t.Errorf("reasonix manifest must use the flat {match, command} shape, not claude's nested form:\n %s", a)
	}
}

// TestPluginPack_CommittedReasonixManifestMatchesGenerator: the hooks field of the committed
// plugins/forge/reasonix-plugin.json must equal the current output of GeneratePluginPack. Mirrors
// TestPluginPack_CommittedManifestMatchesGenerator for the reasonix native manifest — catches the
// drift of changing ForgeHookSpec (or reasonixEventName) but forgetting to run `forge plugin pack`
// to re-commit reasonix-plugin.json. The committed reasonix manifest is the 5th-host distribution
// artifact; a stale one ships wrong gates to reasonix plugin installs.
//
// TestPluginPack_CommittedReasonixManifestMatchesGenerator：committed 的
// plugins/forge/reasonix-plugin.json 的 hooks 字段必须等于 GeneratePluginPack 当前输出。镜像
// TestPluginPack_CommittedManifestMatchesGenerator 用于 reasonix native manifest——抓"改了
// ForgeHookSpec（或 reasonixEventName）但忘记跑 `forge plugin pack` 重新提交 reasonix-plugin.json"
// 的 drift。committed 的 reasonix manifest 是第 5 host 的分发产物；陈旧的会给 reasonix plugin
// 安装发错的 gate。
func TestPluginPack_CommittedReasonixManifestMatchesGenerator(t *testing.T) {
	committed := filepath.Join("..", "..", "plugins", "forge", "reasonix-plugin.json")
	if _, err := os.Stat(committed); err != nil {
		t.Skipf("committed reasonix manifest not found at %s (non-forge repo layout): %v", committed, err)
	}
	generated := generatePack(t)
	var genManifest, committedManifest map[string]any
	loadJSON(t, filepath.Join(generated, "plugins", "forge", "reasonix-plugin.json"), &genManifest)
	loadJSON(t, committed, &committedManifest)
	a, _ := json.Marshal(genManifest["hooks"])
	b, _ := json.Marshal(committedManifest["hooks"])
	if string(a) != string(b) {
		t.Errorf("committed reasonix-plugin.json hooks drifted from generator output (run `forge plugin pack` and commit the result):\n generated: %s\n committed: %s", a, b)
	}
	// The committed file's identity fields must also match (apiVersion/name) — a stale committed
	// manifest could carry a renamed field the current generator no longer emits. version is checked
	// too: writeReasonixPluginManifest hardcodes it, so changing that constant without re-running
	// `forge plugin pack` would leave the committed artifact stale on version.
	if committedManifest["apiVersion"] != genManifest["apiVersion"] {
		t.Errorf("committed reasonix apiVersion = %v, want %v", committedManifest["apiVersion"], genManifest["apiVersion"])
	}
	if committedManifest["version"] != genManifest["version"] {
		t.Errorf("committed reasonix version = %v, want %v (writeReasonixPluginManifest hardcodes it — re-run forge plugin pack)", committedManifest["version"], genManifest["version"])
	}
}

// TestPluginPack_ReasonixLaunchersCommitted: reasonix anchors the hook command's first token
// ("forge") to the plugin directory (and prepends the plugin dir to PATH), so a launcher shim
// — forge.cmd on Windows, forge on Unix — MUST ship inside plugins/forge/ or every hook fails
// with "command not found" and nothing enforces. These are static plugin assets (like install.sh
// / install.ps1), NOT generator output, so GeneratePluginPack does not write them; a
// committed-presence + content guard is the only thing catching accidental deletion or a
// recursion-guard regression. Regression source: the original reasonix wiring shipped no
// launcher, so even with hooks registered the commands could not resolve.
//
// TestPluginPack_ReasonixLaunchersCommitted：reasonix 把 hook 命令的首 token（"forge"）锚定到
// plugin 目录（并把 plugin 目录前置到 PATH），故必须在 plugins/forge/ 内附 launcher shim——Windows
// 上 forge.cmd、Unix 上 forge——否则每个 hook 都 "command not found"、啥也不 enforce。它们是静态
// plugin 资产（如 install.sh / install.ps1），非生成器输出，故 GeneratePluginPack 不写它们；
// committed-presence + 内容守卫是唯一能抓误删或递归防线回退的东西。回归源：原始 reasonix 接线
// 未附 launcher，故即便 hook 注册了命令也解析不了。
func TestPluginPack_ReasonixLaunchersCommitted(t *testing.T) {
	// Absence is a hard FAILURE here, not a skip. Unlike the sibling manifest tests above (whose
	// files are generator outputs bound to expectedPluginFiles / TestPluginPack_WritesAllFiles),
	// these launchers are STATIC untracked assets — exactly what gets forgotten at `git add` time.
	// A skip would let CI go green on a committed tree that silently dropped them: the launchers
	// are untracked until explicitly added, so "forgotten at commit" → fresh-checkout CI run hits
	// os.Stat failure → skip → false green → reasonix ships with no launcher → every hook fails
	// "command not found". That reachable false-confidence case is what this guard exists to catch.
	for _, rel := range []string{
		filepath.Join("..", "..", "plugins", "forge", "forge.cmd"),
		filepath.Join("..", "..", "plugins", "forge", "forge"),
	} {
		if _, err := os.Stat(rel); err != nil {
			t.Fatalf("committed launcher missing at %s — these are hand-committed static assets (forge plugin pack does NOT generate them); absence means the whole reasonix hook stack fails to resolve. %v", rel, err)
		}
	}
	// Windows shim resolves forge via `where forge`, skipping its own dir (%~dp0) to avoid
	// re-invoking itself (the plugin dir is prepended to PATH, so where lists this shim first).
	cmdBody, err := os.ReadFile(filepath.Join("..", "..", "plugins", "forge", "forge.cmd"))
	if err != nil {
		t.Fatalf("read forge.cmd: %v", err)
	}
	if !strings.Contains(string(cmdBody), "where forge") {
		t.Errorf("forge.cmd must resolve forge via `where forge`, got:\n%s", cmdBody)
	}
	if !strings.Contains(string(cmdBody), "%~dp0") {
		t.Errorf("forge.cmd must recursion-guard its own dir (%%~dp0), got:\n%s", cmdBody)
	}
	// Unix shim exec's the first forge on PATH outside its own dir (self_dir guard).
	unixBody, err := os.ReadFile(filepath.Join("..", "..", "plugins", "forge", "forge"))
	if err != nil {
		t.Fatalf("read forge: %v", err)
	}
	if !strings.Contains(string(unixBody), "exec") {
		t.Errorf("forge launcher must exec the resolved binary, got:\n%s", unixBody)
	}
	if !strings.Contains(string(unixBody), "self_dir") {
		t.Errorf("forge launcher must recursion-guard its own dir (self_dir), got:\n%s", unixBody)
	}
}
