package agentbridge

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/skills"
)

// expectedPluginFiles is the set of relative paths that GeneratePluginPack(DefaultPluginPack) should generate (relative to
// RepoDir). Forgetting to list a new output file here would let TestPluginPack_WritesAllFiles miss it — intentionally hardcoded to force the generator
// and tests to stay in sync. Paths contain `forge` because DefaultPluginPack.PluginName=`forge`.
// The skills/ output class is NOT listed here (its count tracks the canonical library and
// cannot be hardcoded) — TestPluginPack_SkillsShipped / _SkillsConvergeOnRegen /
// _CommittedSkillsMatchGenerator guard the same sync contract via the dynamic embeddedSkillDirs set.
//
// expectedPluginFiles 是 GeneratePluginPack(DefaultPluginPack) 应生成的相对路径集（相对
// RepoDir）。加新输出文件忘加这里，TestPluginPack_WritesAllFiles 会漏检——故意列死，逼生成器
// 与测试同步。路径含 "forge" 因 DefaultPluginPack.PluginName="forge"。
var expectedPluginFiles = []string{
	".claude-plugin/marketplace.json",
	".cursor-plugin/marketplace.json",
	"plugins/forge/.claude-plugin/plugin.json",
	"plugins/forge/reasonix-plugin.json",
	"plugins/forge/hooks.json",
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

// TestPluginPack_HooksMirrorSettings: the hooks field of plugin.json must equal the hooks field that the ForgeHookSpec fixture
// writes to settings.local.json — a single-source-of-truth guard. End-to-end comparison (reading two real files,
// not function return values). If someone changes ForgeHookSpec but pluginpack switches to a hardcoded copy, this test catches the drift.
//
// TestPluginPack_HooksMirrorSettings：plugin.json 的 hooks 字段必须等于 ForgeHookSpec fixture
// 写到 settings.local.json 的 hooks 字段——单一真相源守卫。端到端比对（读两个真实文件，
// 非函数返回值）。若有人改 ForgeHookSpec 但 pluginpack 改用硬编码副本，此测试抓住 drift。
func TestPluginPack_HooksMirrorSettings(t *testing.T) {
	sdir := t.TempDir()
	writeClaudeSettingsFixture(t, sdir)
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

// TestPluginPack_NoCurlyQuotes: regression guard for [[windows-input-quote-corruption]] — all
// GENERATOR-RENDERED files must never contain curly quotes U+201C/U+201D (the corruption
// signature of Windows input eating Go source literals). The skills/ subtree is EXEMPT: it is
// a byte-verbatim copy of the authored canonical library, where curly quotes are legitimate
// Chinese prose punctuation (the corruption this guard catches is in files the generator
// composes from Go string literals, not in authored content copied verbatim).
// Target strings are built from runes (bypassing whether the test source literal is corrupted).
//
// TestPluginPack_NoCurlyQuotes：回归守卫 [[windows-input-quote-corruption]]——所有**生成器渲染**
// 的文件绝不能含弯引号 U+201C/U+201D（Windows 输入吃掉 Go 源码字面量的腐蚀签名）。skills/
// 子树豁免：它是 authored canonical 库的逐字节复制，弯引号在那里是合法的中文正文标点（本守卫
// 抓的是生成器从 Go 字符串字面量拼出的文件里的腐蚀，不是逐字复制的 authored 内容）。用 rune
// 构造目标串（绕过测试源码字面量是否被腐蚀）。
func TestPluginPack_NoCurlyQuotes(t *testing.T) {
	dir := generatePack(t)
	curly := string([]rune{0x201c, 0x201d})
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// Authored skill content ships verbatim — curly quotes there are legitimate Chinese
		// punctuation, not corruption. Only generator-composed files are guarded.
		//
		// authored skill 内容逐字分发——其中的弯引号是合法中文标点，非腐蚀。只守卫生成器
		// 组合产出的文件。
		if strings.Contains(filepath.ToSlash(path), "plugins/forge/skills/") {
			return nil
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
// + correct npm package name (@agent_forge/forge, matching npm/package.json) + capability boundary (Phase 3: project registration is AUTOMATIC for plugin
// users via init-suggest auto-takeover; manual init demoted to repair/non-plugin/team-mode).
// Negative assertion on @mjxupup/forge catches historical regression: early pluginReadme wrote the wrong package name using the GitHub owner slug.
//
// TestPluginPack_Readme：README 含三步首体验结构 + 每 host 安装命令 + Codex 路径未确认的诚实表述
// + npm 包名正确（@agent_forge/forge，与 npm/package.json 一致）+ 能力边界（Phase 3：plugin 用户
// 项目登记经 init-suggest 自动接管；手动 init 降级为修复/非 plugin/团队模式）。
// 负向断言 @mjxupup/forge 抓历史回退：早期 pluginReadme 写过错用 GitHub owner slug 的包名。
func TestPluginPack_Readme(t *testing.T) {
	dir := generatePack(t)
	content := readOrFail(t, filepath.Join(dir, "plugins", "forge", "README.md"))
	for _, want := range []string{
		"Three-step setup",   // 三步首体验结构
		"@agent_forge/forge", // npm 包名（与 npm/package.json 一致）
		"once per machine",   // step 1：二进制是机器级硬前置
		"once per agent",     // step 2：plugin 是 agent 级
		// step 3（Phase 3 新契约）：plugin 用户登记自动化 + 手动 init 的三个残留场景 + 退出/清理指引
		"automatic for plugin users", // init-suggest 自动接管（装即 opt-in）
		"forge registry prune",       // 项目目录移动后的死路径清理指引
		"forge suggest decline",      // 每项目退出指引（退出权高于默认开启）
		"forge init --project",       // 团队模式仍走手动 init
		// skills 段（P3-1）：宣传数量运行时从 embed 现数（%[2]d 插值），渲染结果必须
		// 携带真实数量且无 fmt 动词误用残渣（"%!d"）——钉住插值接线，防数量硬编码回潮。
		//
		// skills paragraph (P3-1): the advertised count is rendered from the embed at
		// runtime (%[2]d); the render must carry the real count and no fmt-verb mojibake
		// ("%!d") — pins the interpolation wiring against a hardcoded number returning.
		fmt.Sprintf("%d skills", embeddedSkillCount()),
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
		"forge init --agents cline",    // cline wrapper 脚本路径（Wave 3b：~/Documents/Cline/Rules/Hooks/）
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
	// Count-verb mojibake: the skill-count interpolation (%[2]d) must never leak a raw
	// verb or bad-format signature into the render.
	//
	// 数量动词乱码：skill 数量插值（%[2]d）不得把裸动词或坏格式签名漏进渲染结果。
	if strings.Contains(content, "%!d") || strings.Contains(content, "%[2]d") {
		t.Errorf("README contains unrendered/mojibake skill-count verb (%%!d or %%[2]d) — the count interpolation is broken")
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

// TestPluginPack_CommittedReadmeMatchesGenerator: the committed plugins/forge/README.md
// must equal the current generator output byte-for-byte. The manifest guard above cannot
// catch README drift (it compares only the hooks field) — hand-editing the rendered README
// without editing assets/plugin_readme.md silently reverts on the next `forge plugin
// pack` run (zcode row added 2026-08-24 was almost lost this way; code-review finding).
//
// TestPluginPack_CommittedReadmeMatchesGenerator：已提交的 plugins/forge/README.md 必须
// 与生成器当前输出逐字节相等。上面的 manifest 守卫抓不住 README 漂移（它只比 hooks
// 字段）——只手改渲染产物、不改 assets/plugin_readme.md 模板，下次任何人跑
// `forge plugin pack` 都会静默回滚该改动（2026-08-24 的 zcode 行差点因此丢失；
// code-review 发现）。
func TestPluginPack_CommittedReadmeMatchesGenerator(t *testing.T) {
	committed := filepath.Join("..", "..", "plugins", "forge", "README.md")
	if _, err := os.Stat(committed); err != nil {
		t.Skipf("committed plugin README not found at %s (non-forge repo layout — nothing to regenerate against): %v", committed, err)
	}
	generated := generatePack(t)
	want := readOrFail(t, filepath.Join(generated, "plugins", "forge", "README.md"))
	got := readOrFail(t, committed)
	if got != want {
		t.Errorf("committed plugins/forge/README.md drifted from the generator (edit internal/agentbridge/assets/plugin_readme.md, then run `forge plugin pack` and commit the result)")
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

// TestPluginPack_Readme_CopilotVSCodeCaveat pins the Wave 2c code-review finding,
// doc-confirmed against code.visualstudio.com: VS Code detects a plugin's format by
// its manifest marker (.claude-plugin/plugin.json = Claude format → hooks read ONLY
// from hooks/hooks.json; root hooks.json is the Copilot-format location). The pack
// ships the Claude marker, so the root hooks.json may be inert on VS Code; the README
// must say so honestly instead of implying VS Code is wired (same pattern as the
// codex "not officially confirmed" caveat). Guards against the caveat being dropped
// in a README rewrite while the structural fix (hooks/hooks.json + double-fire
// verification on Claude Code) remains an open follow-up.
//
// TestPluginPack_Readme_CopilotVSCodeCaveat 钉死 Wave 2c 代码审查发现（已对照
// code.visualstudio.com 文档核实）：VS Code 按 manifest 标记检测 plugin 格式
// （.claude-plugin/plugin.json = Claude 格式 → hooks 只从 hooks/hooks.json 读；
// 根 hooks.json 是 Copilot 格式位置）。pack 带 Claude 标记，故根 hooks.json 在
// VS Code 上可能无效；README 必须诚实说明，而非暗示 VS Code 已接线（与 codex
// 「未官方确认」caveat 同款模式）。防止结构性修复（hooks/hooks.json + Claude
// Code 双跑验证）仍是开放 follow-up 期间，caveat 在 README 重写中被丢掉。
func TestPluginPack_Readme_CopilotVSCodeCaveat(t *testing.T) {
	dir := generatePack(t)
	content := readOrFail(t, filepath.Join(dir, "plugins", "forge", "README.md"))
	for _, want := range []string{
		"VS Code caveat",
		"hooks/hooks.json",
		"VS Code unverified",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("README missing copilot VS Code caveat wording %q — the root hooks.json is inert on VS Code (Claude-format detection) and the README must say so honestly", want)
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

// TestPluginPack_CopilotHooksManifest: the copilot hooks.json (Wave 2c) must carry
// copilot's config format — {"version":1,"hooks":{PascalCase events}} with flat
// {type,command,matcher,timeoutSec} entries — at the plugin ROOT (NOT hooks/hooks.json:
// Claude Code also loads that location and would double-fire every hook alongside
// .claude-plugin/plugin.json's hooks field). Every forge command carries
// ` --agent copilot` (output-protocol selection — copilot parses no decision:"approve"
// and agentStop blocks only via stdout decision JSON), matchers pass through verbatim
// (copilot matches Claude tool names), PostCompact stays absent (no copilot analogue —
// only the observe-only preCompact, whose output is not processed), and timeoutSec is
// set (copilot's 30s default risks killing heavier gates like task-verify).
//
// TestPluginPack_CopilotHooksManifest：copilot hooks.json（Wave 2c）必须是 copilot 的
// 配置格式——{"version":1,"hooks":{PascalCase event}} 加扁平 {type,command,matcher,
// timeoutSec} 条目——且位于 plugin 根（非 hooks/hooks.json：Claude Code 也加载该位置，
// 会与 .claude-plugin/plugin.json 的 hooks 字段双跑每个 hook）。每条 forge 命令带
// ` --agent copilot`（输出协议选择——copilot 不解析 decision:"approve"，agentStop 只能
// 经 stdout decision JSON 阻断），matcher 原样透传（copilot 匹配 Claude 工具名），
// PostCompact 保持缺席（无 copilot 对应物——只有 observe-only 的 preCompact，其输出
// 不被处理），timeoutSec 必须设置（copilot 默认 30s 有杀掉 task-verify 等重型门禁的风险）。
func TestPluginPack_CopilotHooksManifest(t *testing.T) {
	dir := generatePack(t)
	// Root location, not hooks/hooks.json (the Claude double-fire trap).
	//
	// 根位置，非 hooks/hooks.json（Claude 双跑陷阱）。
	path := filepath.Join(dir, "plugins", "forge", "hooks.json")
	var manifest map[string]any
	loadJSON(t, path, &manifest)
	if manifest["version"] != float64(1) {
		t.Errorf("version = %v, want 1", manifest["version"])
	}
	hooksMap, ok := manifest["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks field not an object: %T", manifest["hooks"])
	}
	// Event whitelist: everything copilot supports must be wired; PostCompact (the one
	// spec event with no copilot analogue) must stay absent.
	//
	// event 白名单：copilot 支持的必须全接；PostCompact（唯一无 copilot 对应物的
	// spec event）必须缺席。
	for _, required := range []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit"} {
		if _, present := hooksMap[required]; !present {
			t.Errorf("copilot hooks.json must wire %s: missing", required)
		}
	}
	if _, present := hooksMap["PostCompact"]; present {
		t.Error("copilot hooks.json must not wire PostCompact (no copilot analogue — unknown event keys risk item drops at load)")
	}
	sawForge := false
	for event, entries := range hooksMap {
		list, ok := entries.([]any)
		if !ok {
			t.Fatalf("%s entries not a list: %T", event, entries)
		}
		for _, raw := range list {
			e, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("%s entry not an object: %T", event, raw)
			}
			if e["type"] != "command" {
				t.Errorf("%s entry type = %v, want command", event, e["type"])
			}
			cmd, _ := e["command"].(string)
			if !strings.HasPrefix(cmd, "forge hook ") {
				continue
			}
			sawForge = true
			if !strings.HasSuffix(cmd, " --agent copilot") {
				t.Errorf("%s: forge command missing --agent copilot suffix: %s", event, cmd)
			}
			// Matchers pass through VERBATIM (copilot matches Claude tool names —
			// bash→Bash, edit/str_replace_editor/apply_patch→Edit). Any translated
			// token here would silently disarm the gate. Session-level events carry
			// no matcher (omitempty → absent).
			//
			// matcher 原样透传（copilot 匹配 Claude 工具名——bash→Bash、
			// edit/str_replace_editor/apply_patch→Edit）。这里出现任何被翻译的
			// token 都等于静默解除门禁。会话级 event 无 matcher（omitempty → 缺席）。
			if matcher, ok := e["matcher"].(string); ok && matcher != "" {
				for _, tok := range strings.Split(matcher, "|") {
					switch tok {
					case "Shell", "Task", "Write|Edit":
						t.Errorf("%s: matcher token %q looks translated (copilot matches Claude tool names verbatim): %v", event, tok, matcher)
					}
				}
			}
			if timeout, ok := e["timeoutSec"].(float64); !ok || timeout < 60 {
				t.Errorf("%s: timeoutSec = %v, want >= 60 (copilot's 30s default risks killing heavier gates)", event, e["timeoutSec"])
			}
		}
	}
	if !sawForge {
		t.Fatal("no forge commands generated in copilot hooks.json")
	}
}

// TestPluginPack_CopilotHooksMirrorSpec: the per-event forge command sets in the
// copilot manifest must equal ForgeHookSpec's (suffix-stripped) — a single-source-of-
// truth guard parallel to TestPluginPack_HooksMirrorSettings. A hardcoded or drifted
// copilot table would wire different gates than every other host.
//
// TestPluginPack_CopilotHooksMirrorSpec：copilot manifest 里每 event 的 forge 命令集
// 必须等于 ForgeHookSpec 的（剥后缀比对）——与 TestPluginPack_HooksMirrorSettings 平行
// 的单一真相源守卫。硬编码或漂移的 copilot 表会接出与其他 host 不同的 gate。
func TestPluginPack_CopilotHooksMirrorSpec(t *testing.T) {
	dir := generatePack(t)
	var manifest map[string]any
	loadJSON(t, filepath.Join(dir, "plugins", "forge", "hooks.json"), &manifest)
	hooksMap := manifest["hooks"].(map[string]any)

	for event, entries := range hooksMap {
		list, _ := entries.([]any)
		got := map[string]bool{}
		for _, raw := range list {
			e := raw.(map[string]any)
			cmd, _ := e["command"].(string)
			if cmd != "" {
				got[strings.TrimSuffix(cmd, " --agent copilot")] = true
			}
		}
		want := map[string]bool{}
		for _, m := range hooks.ForgeHookSpec()[event] {
			for _, h := range m.Hooks {
				want[h.Command] = true
			}
		}
		if !stringSetEqual(want, got) {
			t.Errorf("copilot %s commands drifted from ForgeHookSpec:\n  spec:    %s\n  copilot: %s", event, sortedSet(want), sortedSet(got))
		}
	}
}

// TestPluginPack_CommittedCopilotManifestMatchesGenerator: the committed
// plugins/forge/hooks.json must equal the current generator output. Mirrors
// TestPluginPack_CommittedReasonixManifestMatchesGenerator for the copilot manifest —
// catches the drift of changing ForgeHookSpec (or copilotEventName) but forgetting to
// run `forge plugin pack` to re-commit. A stale committed copilot manifest ships wrong
// gates to every copilot plugin install (copilot is the 6th host served by the pack).
//
// TestPluginPack_CommittedCopilotManifestMatchesGenerator：committed 的
// plugins/forge/hooks.json 必须等于生成器当前输出。镜像
// TestPluginPack_CommittedReasonixManifestMatchesGenerator 用于 copilot manifest——抓
// "改了 ForgeHookSpec（或 copilotEventName）但忘记跑 `forge plugin pack` 重新提交"的
// drift。陈旧的 committed copilot manifest 会给每次 copilot plugin 安装发错的 gate
// （copilot 是 pack 服务的第 6 个 host）。
func TestPluginPack_CommittedCopilotManifestMatchesGenerator(t *testing.T) {
	committed := filepath.Join("..", "..", "plugins", "forge", "hooks.json")
	if _, err := os.Stat(committed); err != nil {
		// Same hard-failure-if-layout-exists contract as the skills tree: the copilot
		// manifest is a generator output listed in expectedPluginFiles — once the
		// committed plugin layout exists, its absence means `forge plugin pack` output
		// was not committed. Skipping here would false-green on fresh checkouts.
		//
		// 与 skills 树同款的"布局在即硬失败"契约：copilot manifest 是列在
		// expectedPluginFiles 里的生成器输出——committed plugin 布局存在后，它的
		// 缺席意味着 `forge plugin pack` 的产物没提交。这里 skip 会在 fresh
		// checkout 上假绿。
		if _, perr := os.Stat(filepath.Join("..", "..", "plugins", "forge", ".claude-plugin", "plugin.json")); perr == nil {
			t.Fatalf("committed plugin layout exists but plugins/forge/hooks.json is missing — the copilot manifest is a required distribution asset (run `forge plugin pack` and commit it): %v", err)
		}
		t.Skipf("committed copilot manifest not found at %s (non-forge repo layout): %v", committed, err)
	}
	generated := generatePack(t)
	var genManifest, committedManifest map[string]any
	loadJSON(t, filepath.Join(generated, "plugins", "forge", "hooks.json"), &genManifest)
	loadJSON(t, committed, &committedManifest)
	a, _ := json.Marshal(genManifest)
	b, _ := json.Marshal(committedManifest)
	if string(a) != string(b) {
		t.Errorf("committed hooks.json drifted from generator output (run `forge plugin pack` and commit the result):\n generated: %s\n committed: %s", a, b)
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

// embeddedSkillDirs returns the skill dir names in the embedded canonical library (top-level
// dirs carrying SKILL.md) — the expected set the plugin pack must ship. Shared by the skills
// tests below so "what is a skill" has one definition.
//
// embeddedSkillDirs 返回内嵌 canonical 库里的 skill 目录名（带 SKILL.md 的顶层目录）——
// plugin pack 必须分发的期望集。下方 skills 测试共用，让"什么算一个 skill"只有一种定义。
func embeddedSkillDirs(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(skills.FS, ".")
	if err != nil {
		t.Fatalf("read embedded skills: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, serr := fs.Stat(skills.FS, path.Join(e.Name(), "SKILL.md")); serr == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestPluginPack_SkillsShipped: the pack must ship the full embedded skill library — one dir
// per skill under plugins/<name>/skills/, each with SKILL.md, content byte-equal to the embed.
// Regression source: GeneratePluginPack shipped hooks-only for the plugin's whole life, so
// plugin users saw zero skills and still needed the manual `forge skills install --global`.
//
// TestPluginPack_SkillsShipped：pack 必须分发完整内嵌 skill 库——plugins/<name>/skills/ 下
// 每 skill 一目录、各有 SKILL.md、内容与 embed 字节一致。回归源：GeneratePluginPack 有史以来
// 只带 hooks，plugin 用户看不到任何 skill，仍需手动 `forge skills install --global`。
func TestPluginPack_SkillsShipped(t *testing.T) {
	dir := generatePack(t)
	want := embeddedSkillDirs(t)
	if len(want) == 0 {
		t.Fatal("embedded canonical library resolved 0 skills — test precondition broken")
	}
	skillsDir := filepath.Join(dir, "plugins", "forge", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read plugin skills dir: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("unexpected non-dir entry in plugin skills/: %s", e.Name())
			continue
		}
		if _, serr := os.Stat(filepath.Join(skillsDir, e.Name(), "SKILL.md")); serr != nil {
			t.Errorf("skill dir %s has no SKILL.md (not loadable)", e.Name())
		}
		got[e.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("plugin skills/ missing embedded skill %q (%d of %d shipped)", name, len(got), len(want))
		}
	}
	for name := range got {
		found := false
		for _, w := range want {
			if w == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("plugin skills/ has extra dir %q not in embedded library (stale entry)", name)
		}
	}
	// Spot-check content byte-equality: a shipped skill must be the embed verbatim (single
	// source of truth), not a re-rendered variant. Checks every file of the first skill that
	// carries extra assets beyond SKILL.md (exercises the recursive WalkDir copy).
	//
	// 内容字节相等抽查：分发的 skill 必须与 embed 逐字一致（单一真相源），不是重渲染变体。
	// 检查第一个带额外资产（超出 SKILL.md）的 skill 的全部文件（覆盖递归 WalkDir 复制）。
	var probe string
	for _, name := range want {
		sub, _ := fs.ReadDir(skills.FS, name)
		if len(sub) > 1 {
			probe = name
			break
		}
	}
	if probe == "" {
		probe = want[0]
	}
	werr := fs.WalkDir(skills.FS, probe, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || filepath.Ext(p) == ".go" {
			return werr
		}
		emb, rerr := skills.FS.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		disk, rerr := os.ReadFile(filepath.Join(skillsDir, filepath.FromSlash(p)))
		if rerr != nil {
			t.Errorf("skill file %s not shipped: %v", p, rerr)
			return nil
		}
		if string(emb) != string(disk) {
			t.Errorf("skill file %s drifted from embed (single-source-of-truth violation)", p)
		}
		return nil
	})
	if werr != nil {
		t.Fatalf("walk probe skill %s: %v", probe, werr)
	}
}

// TestPluginPack_SkillsConvergeOnRegen: regeneration must converge, not accumulate — a skill
// deleted from the canonical library (simulated by planting a stale dir) must disappear from
// the pack on the next run. Without the RemoveAll-first design, stale dirs linger in the
// committed pack forever.
//
// TestPluginPack_SkillsConvergeOnRegen：regen 必须收敛而非只增不减——canonical 库里删掉的
// skill（用植入陈旧目录模拟）须在下一次生成时从 pack 消失。若无先 RemoveAll 的设计，
// 陈旧目录会永远残留在 committed pack 里。
func TestPluginPack_SkillsConvergeOnRegen(t *testing.T) {
	dir := t.TempDir()
	spec := DefaultPluginPack(dir)
	if err := GeneratePluginPack(spec); err != nil {
		t.Fatalf("GeneratePluginPack: %v", err)
	}
	stale := filepath.Join(dir, "plugins", "forge", "skills", "zz-stale-skill")
	if err := os.MkdirAll(stale, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "SKILL.md"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := GeneratePluginPack(spec); err != nil {
		t.Fatalf("regen: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("stale skill dir survived regeneration (pack must converge, not accumulate)")
	}
	// The real skills must all still be there after the stale wipe.
	//
	// 清掉陈旧条目后，真实 skills 必须全部仍在。
	for _, name := range embeddedSkillDirs(t) {
		if _, serr := os.Stat(filepath.Join(dir, "plugins", "forge", "skills", name, "SKILL.md")); serr != nil {
			t.Errorf("skill %s lost after regen (RemoveAll wiped more than the stale entry): %v", name, serr)
		}
	}
}

// TestPluginPack_CommittedSkillsMatchGenerator: the committed plugins/forge/skills/ tree must
// match the embedded library (file set + bytes). Mirrors TestPluginPack_CommittedManifestMatchesGenerator:
// the committed pack is the marketplace source users install from, so forgetting to re-run
// `forge plugin pack` after editing a skill ships stale skills to every plugin install.
//
// TestPluginPack_CommittedSkillsMatchGenerator：committed 的 plugins/forge/skills/ 树必须与
// 内嵌库一致（文件集 + 字节）。镜像 TestPluginPack_CommittedManifestMatchesGenerator：committed
// pack 是用户安装的 marketplace 源，改了 skill 忘跑 `forge plugin pack` 会给每次 plugin 安装
// 发陈旧 skills。
func TestPluginPack_CommittedSkillsMatchGenerator(t *testing.T) {
	committed := filepath.Join("..", "..", "plugins", "forge", "skills")
	if _, err := os.Stat(committed); err != nil {
		// Absence is a hard FAILURE when the repo carries the plugin layout at all (committed
		// plugin.json present): the skills tree is a required committed distribution asset —
		// forgotten at `git add` time would otherwise skip green on fresh checkouts (the exact
		// false-green pattern TestPluginPack_ReasonixLaunchersCommitted's comment rejects).
		// Only skip when the whole plugins/forge layout is absent (non-Forge repo layout).
		//
		// 仓库带着 plugin 布局（committed plugin.json 在）时缺失即硬失败：skills 树是必需的
		// committed 分发资产——漏 git add 会在 fresh checkout 上静默 skip 变绿（正是
		// TestPluginPack_ReasonixLaunchersCommitted 注释否决的假绿模式）。仅当整个
		// plugins/forge 布局缺失（非 Forge 仓库布局）才 skip。
		if _, perr := os.Stat(filepath.Join("..", "..", "plugins", "forge", ".claude-plugin", "plugin.json")); perr == nil {
			t.Fatalf("committed plugin layout exists but plugins/forge/skills/ is missing — the skills tree is a required distribution asset (run `forge plugin pack` and git add it): %v", err)
		}
		t.Skipf("committed plugin skills not found at %s (non-forge repo layout): %v", committed, err)
	}
	// 1. Every embedded skill file must be committed verbatim.
	//
	// 1. 每个内嵌 skill 文件必须逐字 committed。
	for _, name := range embeddedSkillDirs(t) {
		werr := fs.WalkDir(skills.FS, name, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() || filepath.Ext(p) == ".go" {
				return werr
			}
			emb, rerr := skills.FS.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			disk, rerr := os.ReadFile(filepath.Join(committed, filepath.FromSlash(p)))
			if rerr != nil {
				if os.IsNotExist(rerr) {
					t.Errorf("committed skills missing file %s (run `forge plugin pack` and commit)", p)
				} else {
					t.Errorf("read committed %s: %v", p, rerr)
				}
				return nil
			}
			if string(emb) != string(disk) {
				t.Errorf("committed skill file %s drifted from embed (run `forge plugin pack` and commit)", p)
			}
			return nil
		})
		if werr != nil {
			t.Fatalf("walk %s: %v", name, werr)
		}
	}
	// 2. No stale committed dirs beyond the embedded set (deleted skills must be re-packed out).
	//
	// 2. committed 不得有内嵌集之外的陈旧目录（删掉的 skill 须随 re-pack 移出）。
	entries, err := os.ReadDir(committed)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, name := range embeddedSkillDirs(t) {
		want[name] = true
	}
	for _, e := range entries {
		if e.IsDir() && !want[e.Name()] {
			t.Errorf("committed skills has stale dir %q (deleted from canonical library; run `forge plugin pack` and commit)", e.Name())
		}
	}
}
