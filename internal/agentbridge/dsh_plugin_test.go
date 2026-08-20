package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// dsh_plugin_test.go — drift guards for the DeepSeek Harness plugin
// (plugins/forge-dsh). The wrapper's hook roster lives in lib/spec.json and
// must mirror hooks.ForgeHookSpec exactly: the Cordis plugin runs whatever
// spec.json says, so any drift means dsh enforces a different gate set than
// every other host — the exact failure mode TestKimiPluginManifestMirrorsSpec
// guards for the kimi manifest.
//
// dsh_plugin_test.go —— DeepSeek Harness 插件（plugins/forge-dsh）的漂移守卫。
// 包装层的 hook 名册住在 lib/spec.json，必须与 hooks.ForgeHookSpec 完全一致：
// Cordis 插件按 spec.json 执行，任何漂移都意味着 dsh 执行的门禁集合与其他
// 宿主不同——正是 TestKimiPluginManifestMirrorsSpec 为 kimi manifest 防的那类
// 故障。
const dshPluginDir = "../../plugins/forge-dsh"

// TestDshPluginSpecMirrorsSpec pins plugins/forge-dsh/lib/spec.json to
// ForgeHookSpec byte-semantically (same events, matchers, commands, order).
//
// TestDshPluginSpecMirrorsSpec 把 plugins/forge-dsh/lib/spec.json 钉在
// ForgeHookSpec 上（同事件、matcher、命令、顺序）。
func TestDshPluginSpecMirrorsSpec(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(dshPluginDir, "lib", "spec.json"))
	if err != nil {
		t.Fatalf("read dsh plugin spec.json: %v", err)
	}
	var got map[string][]hooks.HookMatcher
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("spec.json is not a ForgeHookSpec-shaped document: %v", err)
	}
	want := hooks.ForgeHookSpec()
	if !reflect.DeepEqual(got, want) {
		for event, wantMatchers := range want {
			if !reflect.DeepEqual(got[event], wantMatchers) {
				t.Errorf("event %s drifted:\n got: %+v\nwant: %+v", event, got[event], wantMatchers)
			}
		}
		for event := range got {
			if _, ok := want[event]; !ok {
				t.Errorf("event %s exists in spec.json but not in ForgeHookSpec", event)
			}
		}
		t.Fatal("plugins/forge-dsh/lib/spec.json drifted from ForgeHookSpec — resync the mirror (it is data, edit it directly)")
	}
}

// TestDshPluginPackageContract pins the distribution contract the dsh plugin
// loader relies on: the dsh.bundle.patch pointer, the entry file, and zero
// runtime dependencies (the wrapper must load anywhere dsh runs without
// pulling a module graph).
//
// TestDshPluginPackageContract 钉住 dsh plugin loader 依赖的分发契约：
// dsh.bundle.patch 指针、入口文件、零运行时依赖（包装层必须在 dsh 运行的任何
// 布局下无需模块图即可加载）。
func TestDshPluginPackageContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(dshPluginDir, "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	var pkg struct {
		Name         string            `json:"name"`
		Main         string            `json:"main"`
		Dependencies map[string]string `json:"dependencies"`
		Dsh          struct {
			Bundle struct {
				Patch string `json:"patch"`
			} `json:"bundle"`
		} `json:"dsh"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("unmarshal package.json: %v", err)
	}
	if pkg.Dsh.Bundle.Patch != "./cordis.patch.yml" {
		t.Errorf("dsh.bundle.patch = %q, want ./cordis.patch.yml — without it the package installs but stays inert", pkg.Dsh.Bundle.Patch)
	}
	if pkg.Main != "lib/index.js" {
		t.Errorf("main = %q, want lib/index.js", pkg.Main)
	}
	if len(pkg.Dependencies) > 0 {
		t.Errorf("runtime dependencies crept in (%v) — the wrapper must stay zero-dependency (devDependencies for tests are fine)", pkg.Dependencies)
	}
	patch, err := os.ReadFile(filepath.Join(dshPluginDir, "cordis.patch.yml"))
	if err != nil {
		t.Fatalf("read cordis.patch.yml: %v", err)
	}
	if !strings.Contains(string(patch), pkg.Name) {
		t.Errorf("cordis.patch.yml does not reference the package name %q — the insert would mount nothing", pkg.Name)
	}
	for _, entry := range []string{filepath.Join("lib", "index.js"), filepath.Join("lib", "spec.json"), "README.md"} {
		if _, err := os.Stat(filepath.Join(dshPluginDir, entry)); err != nil {
			t.Errorf("shipped file %s missing: %v", entry, err)
		}
	}
}

// TestDshTranslator_Registered guards AllTranslators membership — `forge init
// --agents dsh` resolves through it, and TestParseAgentFlag_CoversAllTranslators
// derives its coverage set from it.
//
// TestDshTranslator_Registered 守卫 AllTranslators 成员资格——`forge init
// --agents dsh` 经它解析，TestParseAgentFlag_CoversAllTranslators 的覆盖集也从
// 它派生。
func TestDshTranslator_Registered(t *testing.T) {
	for _, tr := range AllTranslators() {
		if tr.AgentType() == AgentDsh {
			return
		}
	}
	t.Fatal("DshTranslator not registered in AllTranslators")
}

// TestDshTranslator_Translate pins the deliberate no-op contract (the plugin
// is the only wiring path): Translate succeeds and writes NOTHING.
//
// TestDshTranslator_Translate 钉住刻意的 no-op 契约（plugin 是唯一接线路径）：
// Translate 成功且不写任何东西。
func TestDshTranslator_Translate(t *testing.T) {
	dir := t.TempDir()
	tr := &DshTranslator{}
	if err := tr.Translate(dir, &TranslationInput{}); err != nil {
		t.Fatalf("dsh Translate must succeed as a no-op, got %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dsh Translate must write nothing (plugin is the wiring path), found %d entries", len(entries))
	}
}
