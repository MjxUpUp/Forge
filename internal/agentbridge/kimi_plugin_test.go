package agentbridge

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// kimiPluginVersion is the display version committed into .kimi-plugin/plugin.json. It is
// release metadata (shown in /plugins and used for update badges), intentionally NOT
// auto-bumped by scripts/release.js — bump it here when the plugin surface (hooks roster
// excluded, that one is guarded) meaningfully changes.
//
// kimiPluginVersion 是提交进 .kimi-plugin/plugin.json 的展示版本。它是发布元数据
// （显示在 /plugins 并用于更新角标），刻意不由 scripts/release.js 自动 bump——
// 当插件面（hooks 名册除外，那个有守卫）发生实质变化时在此手动升。
const kimiPluginVersion = "1.18.0"

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
// TestKimiPluginManifestMirrorsSpec 把已提交的 .kimi-plugin/plugin.json 钉在由
// hooks.ForgeHookSpec（单一真相源）派生的生成器输出上。kimi 的 GitHub 安装从仓库
// 根读 manifest，故它必须提交进库——任何 hooks 名册变更而不同步 manifest 都会在此
// 失败。
func TestKimiPluginManifestMirrorsSpec(t *testing.T) {
	manifest := BuildKimiPluginManifest(kimiPluginVersion, kimiPluginDescription)
	want, err := MarshalKimiPluginManifest(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	// Roster parity: one manifest hook per spec command.
	total := 0
	for _, matchers := range hooks.ForgeHookSpec() {
		for _, m := range matchers {
			total += len(m.Hooks)
		}
	}
	if len(manifest.Hooks) != total {
		t.Fatalf("manifest has %d hooks, spec has %d commands", len(manifest.Hooks), total)
	}
	for _, h := range manifest.Hooks {
		if h.Timeout <= 0 || h.Timeout > 600 {
			t.Errorf("hook %s/%s timeout %d outside kimi's 1-600 range", h.Event, h.Command, h.Timeout)
		}
	}

	path := filepath.Join("..", "..", ".kimi-plugin", "plugin.json")
	if *updateKimiPlugin {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0644); err != nil {
			t.Fatalf("update manifest: %v", err)
		}
		t.Logf("rewrote %s", path)
		return
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed manifest: %v (run with -update-kimi-plugin to create)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("committed .kimi-plugin/plugin.json drifted from ForgeHookSpec — regenerate: go test ./internal/agentbridge -run TestKimiPluginManifestMirrorsSpec -update-kimi-plugin")
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
