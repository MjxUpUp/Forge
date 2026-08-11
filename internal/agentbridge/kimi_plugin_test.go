package agentbridge

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// readReleaseVersion reads the canonical release version from npm/package.json — the
// single source of truth that scripts/release.js bumps and that .kimi-plugin/plugin.json's
// version field now tracks (see TestKimiPluginManifestVersionTracksRelease). Centralizing
// the read here keeps the Go guard and the -update-kimi-plugin regenerator on the same
// source the release script writes.
//
// readReleaseVersion 从 npm/package.json 读权威发布版本——scripts/release.js bump 的
// 单一真相源，.kimi-plugin/plugin.json 的 version 字段现在跟随它（见
// TestKimiPluginManifestVersionTracksRelease）。集中读取使 Go 守卫与 -update-kimi-plugin
// 重生成器盯上同一个 release 脚本写入的源。
func readReleaseVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "npm", "package.json"))
	if err != nil {
		t.Fatalf("read npm/package.json: %v", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("unmarshal npm/package.json: %v", err)
	}
	if pkg.Version == "" {
		t.Fatal("npm/package.json has empty version")
	}
	return pkg.Version
}

const kimiPluginDescription = "Forge loop-engineering quality gates: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion for AI coding agents."

// updateKimiPlugin rewrites the committed manifest instead of comparing it
// (`go test ./internal/agentbridge -run TestKimiPluginManifestMirrorsSpec -update-kimi-plugin`).
//
// updateKimiPlugin 让守卫测试改写已提交的 manifest 而非比对
// （`go test ./internal/agentbridge -run TestKimiPluginManifestMirrorsSpec -update-kimi-plugin`）。
var updateKimiPlugin = flag.Bool("update-kimi-plugin", false, "rewrite .kimi-plugin/plugin.json from ForgeHookSpec")

// TestKimiPluginManifestMirrorsSpec pins the committed .kimi-plugin/plugin.json to the
// generator output derived from hooks.ForgeHookSpec (single source of truth). kimi's
// GitHub install reads the manifest from the repo root, so it must be committed — and
// any hook roster change without a manifest refresh fails here.
//
// The version field is deliberately excluded from drift sensitivity: it now tracks the
// forge release (scripts/release.js syncs it), so want is generated with the version read
// back from the committed file — only hooks/name/description are byte-compared. The
// version==release binding itself is guarded by TestKimiPluginManifestVersionTracksRelease.
//
// TestKimiPluginManifestMirrorsSpec 把已提交的 .kimi-plugin/plugin.json 钉在由
// hooks.ForgeHookSpec（单一真相源）派生的生成器输出上。kimi 的 GitHub 安装从仓库
// 根读 manifest，故它必须提交进库——任何 hooks 名册变更而不同步 manifest 都会在此
// 失败。
//
// version 字段刻意排除在 drift 敏感范围外：它现在跟随 forge release（scripts/release.js
// 同步），故 want 用从已提交文件读回的 version 生成——只对 hooks/name/description 做
// 字节比对。version==release 的绑定本身由 TestKimiPluginManifestVersionTracksRelease 守卫。
func TestKimiPluginManifestMirrorsSpec(t *testing.T) {
	path := filepath.Join("..", "..", ".kimi-plugin", "plugin.json")

	// Roster parity: one manifest hook per spec command. Asserted before the byte compare
	// so a hooks/spec mismatch surfaces with a precise count rather than a diffuse diff.
	total := 0
	for _, matchers := range hooks.ForgeHookSpec() {
		for _, m := range matchers {
			total += len(m.Hooks)
		}
	}
	manifestHooks := BuildKimiPluginHooks()
	if len(manifestHooks) != total {
		t.Fatalf("manifest has %d hooks, spec has %d commands", len(manifestHooks), total)
	}
	for _, h := range manifestHooks {
		if h.Timeout <= 0 || h.Timeout > 600 {
			t.Errorf("hook %s/%s timeout %d outside kimi's 1-600 range", h.Event, h.Command, h.Timeout)
		}
	}

	if *updateKimiPlugin {
		// Regenerate from the release version (single source of truth) so a hooks roster
		// refresh also resyncs the version field to the current release.
		version := readReleaseVersion(t)
		want, err := MarshalKimiPluginManifest(BuildKimiPluginManifest(version, kimiPluginDescription))
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0644); err != nil {
			t.Fatalf("update manifest: %v", err)
		}
		t.Logf("rewrote %s (version=%s from npm/package.json)", path, version)
		return
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed manifest: %v (run with -update-kimi-plugin to create)", err)
	}

	// Generate want with the version read back from the committed file: the version field
	// now tracks the release and changes every release, so it must not be part of the
	// hooks-mirror comparison. Only hooks/name/description are byte-compared.
	var committed struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(got, &committed); err != nil {
		t.Fatalf("unmarshal committed manifest for version: %v", err)
	}
	want, err := MarshalKimiPluginManifest(BuildKimiPluginManifest(committed.Version, kimiPluginDescription))
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("committed .kimi-plugin/plugin.json drifted from ForgeHookSpec — regenerate: go test ./internal/agentbridge -run TestKimiPluginManifestMirrorsSpec -update-kimi-plugin")
	}
}

// TestKimiPluginManifestVersionTracksRelease pins the committed .kimi-plugin/plugin.json
// version field to npm/package.json's version. scripts/release.js syncs the two on every
// release; this guard fails if a release ships without the sync (the version field would
// lag and misreport which release the committed manifest corresponds to). kimi's staleness
// detection reads installed.json's github.ref tag, NOT this field, so a lagging field has
// no behavioral impact — but correct display metadata is worth guarding.
//
// TestKimiPluginManifestVersionTracksRelease 把已提交 .kimi-plugin/plugin.json 的 version
// 字段钉在 npm/package.json 的 version 上。scripts/release.js 每次发版同步两者；若某次
// 发版漏同步（version 字段滞后、对不上 release），此守卫失败。kimi 的 staleness 检测读
// installed.json 的 github.ref tag，不读此字段，故滞后无行为影响——但展示元数据正确值得守卫。
func TestKimiPluginManifestVersionTracksRelease(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".kimi-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("read committed manifest: %v", err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	want := readReleaseVersion(t)
	if manifest.Version != want {
		t.Errorf("committed .kimi-plugin/plugin.json version=%q, npm/package.json version=%q — resync: go test ./internal/agentbridge -run TestKimiPluginManifestMirrorsSpec -update-kimi-plugin", manifest.Version, want)
	}
}

func TestIsKimiPluginInstalled(t *testing.T) {
	writeReg := func(t *testing.T, home, content string) {
		t.Helper()
		dir := filepath.Join(home, "plugins")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no file", func(t *testing.T) {
		t.Setenv("KIMI_CODE_HOME", t.TempDir())
		if IsKimiPluginInstalled() {
			t.Error("no installed.json must be false")
		}
	})
	t.Run("garbage", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeReg(t, home, "not json")
		if IsKimiPluginInstalled() {
			t.Error("garbage installed.json must be false")
		}
	})
	t.Run("enabled forge", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeReg(t, home, `{"plugins":[{"id":"forge","source":"https://github.com/MjxUpUp/Forge","enabled":true}]}`)
		if !IsKimiPluginInstalled() {
			t.Error("enabled forge record must be true")
		}
	})
	t.Run("name key fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeReg(t, home, `{"plugins":[{"name":"forge","source":"x"}]}`)
		if !IsKimiPluginInstalled() {
			t.Error("name=forge without explicit disable must be true")
		}
	})
	t.Run("disabled forge", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeReg(t, home, `{"plugins":[{"id":"forge","enabled":false}]}`)
		if IsKimiPluginInstalled() {
			t.Error("enabled=false must be false")
		}
	})
	t.Run("other plugin only", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeReg(t, home, `{"plugins":[{"id":"do-it","enabled":true}]}`)
		if IsKimiPluginInstalled() {
			t.Error("unrelated plugin must not count")
		}
	})
}

// TestKimiPluginStaleInfo pins the staleness signal: only an enabled forge entry with a
// tag ref yields a trimmed bare version; everything else (non-tag ref, missing github/ref,
// disabled, no entry, garbage, no file) is ok=false so the advisory never fires on noise.
//
// TestKimiPluginStaleInfo 钉住 staleness 信号：只有已启用 forge 条目带 tag ref 才产出
// trim 后的裸版本；其余（非 tag ref、缺 github/ref、禁用、无条目、垃圾、无文件）一律
// ok=false，确保 advisory 不会被噪声触发。
func TestKimiPluginStaleInfo(t *testing.T) {
	writeReg := func(t *testing.T, home, content string) {
		t.Helper()
		dir := filepath.Join(home, "plugins")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		json    string
		wantVer string
		wantOk  bool
	}{
		{
			name:    "tag ref v1.19.0",
			json:    `{"plugins":[{"id":"forge","enabled":true,"github":{"owner":"MjxUpUp","repo":"Forge","ref":{"kind":"tag","value":"v1.19.0"}}}]}`,
			wantVer: "1.19.0",
			wantOk:  true,
		},
		{
			name:    "tag ref without v prefix",
			json:    `{"plugins":[{"id":"forge","github":{"ref":{"kind":"tag","value":"1.20.0"}}}]}`,
			wantVer: "1.20.0",
			wantOk:  true,
		},
		{
			name:   "commit ref not comparable",
			json:   `{"plugins":[{"id":"forge","enabled":true,"github":{"ref":{"kind":"commit","value":"abc123"}}}]}`,
			wantOk: false,
		},
		{
			name:   "missing github field",
			json:   `{"plugins":[{"id":"forge","enabled":true}]}`,
			wantOk: false,
		},
		{
			name:   "missing ref",
			json:   `{"plugins":[{"id":"forge","enabled":true,"github":{"owner":"MjxUpUp"}}]}`,
			wantOk: false,
		},
		{
			name:   "empty ref value",
			json:   `{"plugins":[{"id":"forge","enabled":true,"github":{"ref":{"kind":"tag","value":""}}}]}`,
			wantOk: false,
		},
		{
			name:   "disabled forge",
			json:   `{"plugins":[{"id":"forge","enabled":false,"github":{"ref":{"kind":"tag","value":"v1.19.0"}}}]}`,
			wantOk: false,
		},
		{
			name:   "disabled flag true",
			json:   `{"plugins":[{"id":"forge","disabled":true,"github":{"ref":{"kind":"tag","value":"v1.19.0"}}}]}`,
			wantOk: false,
		},
		{
			name:   "no forge entry",
			json:   `{"plugins":[{"id":"other","enabled":true,"github":{"ref":{"kind":"tag","value":"v1.0.0"}}}]}`,
			wantOk: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("KIMI_CODE_HOME", home)
			writeReg(t, home, tc.json)
			ver, ok := KimiPluginStaleInfo()
			if ok != tc.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOk)
			}
			if ok && ver != tc.wantVer {
				t.Errorf("version = %q, want %q", ver, tc.wantVer)
			}
		})
	}

	t.Run("no file", func(t *testing.T) {
		t.Setenv("KIMI_CODE_HOME", t.TempDir())
		if _, ok := KimiPluginStaleInfo(); ok {
			t.Error("missing installed.json must be ok=false")
		}
	})

	t.Run("garbage", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		writeReg(t, home, "not json")
		if _, ok := KimiPluginStaleInfo(); ok {
			t.Error("garbage installed.json must be ok=false")
		}
	})
}

// TestKimiTranslator_PluginWins verifies the dedupe: with the kimi plugin installed,
// Translate strips the config.toml marker section (no double-run) and preserves user
// config — mirroring claude-code's plugin-vs-settings dedupe.
func TestKimiTranslator_PluginWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)

	// Pre-existing config with a forge marker section (installed via --agents kimi earlier).
	userConfig := "default_model = \"kimi-code/k3\"\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(userConfig), 0644); err != nil {
		t.Fatal(err)
	}
	tr := &KimiTranslator{}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte(kimiMarkStart)) {
		t.Fatal("precondition: marker section should exist before plugin install")
	}

	// Install the plugin (record appears) → Translate must strip the section.
	if err := os.MkdirAll(filepath.Join(home, "plugins"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "plugins", "installed.json"),
		[]byte(`{"plugins":[{"id":"forge","enabled":true}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if bytes.Contains(data, []byte(kimiMarkStart)) {
		t.Errorf("plugin installed but config.toml marker section not stripped:\n%s", data)
	}
	if string(data) != userConfig {
		t.Errorf("user config not preserved after strip:\n%q", string(data))
	}
}

// TestKimiTranslator_PluginWins_Boundary pins the two boundary paths of the plugin-wins
// branch: no config.toml at all (clean no-op, no error) and a corrupt marker section
// (StripKimiHooks' corruption error must propagate through Translate, not be swallowed).
func TestKimiTranslator_PluginWins_Boundary(t *testing.T) {
	installPlugin := func(t *testing.T, home string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(home, "plugins"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "plugins", "installed.json"),
			[]byte(`{"plugins":[{"id":"forge","enabled":true}]}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no config.toml", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		installPlugin(t, home)
		if err := (&KimiTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
			t.Errorf("plugin installed + no config.toml must be a clean no-op, got %v", err)
		}
	})

	t.Run("corrupt markers", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KIMI_CODE_HOME", home)
		installPlugin(t, home)
		corrupt := "default_model = \"x\"\n" + kimiMarkStart + "\ntelemetry = false\n"
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(corrupt), 0644); err != nil {
			t.Fatal(err)
		}
		if err := (&KimiTranslator{}).Translate(t.TempDir(), testInput()); err == nil {
			t.Error("corrupt marker section must surface StripKimiHooks' error through Translate")
		}
	})
}
