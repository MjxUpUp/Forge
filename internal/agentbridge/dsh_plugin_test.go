package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// dsh_plugin_test.go —— DeepSeek Harness 插件（plugins/forge-dsh）的漂移守卫。
// 包装层的 hook 名册住在 lib/spec.json，必须与 hooks.ForgeHookSpec 完全一致：
// Cordis 插件按 spec.json 执行，任何漂移都意味着 dsh 执行的门禁集合与其他
// 宿主不同——正是 TestKimiPluginManifestMirrorsSpec 为 kimi manifest 防的那类
// 故障。
const dshPluginDir = "../../plugins/forge-dsh"

// TestDshPluginSpecMirrorsSpec pins plugins/forge-dsh/lib/spec.json to ForgeHookSpec byte-semantically (same events, matchers, commands, order).
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

// TestDshPluginPackageContract pins the distribution contract the dsh plugin loader relies on: the dsh.bundle.patch pointer, the entry file, and exactly one runtime dependency — the forge CLI itself, pinned to an exact version (a binary carrier: the plugin code never imports it, see TestDshPluginLibImportsStayDependencyFree).
//
// TestDshPluginPackageContract 钉住 dsh plugin loader 依赖的分发契约：
// dsh.bundle.patch 指针、入口文件、且运行时依赖恰好一项——forge 本体（精确版本钉）。
// 它只是二进制载体，插件代码不 import 它（见 TestDshPluginLibImportsStayDependencyFree）。
func TestDshPluginPackageContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(dshPluginDir, "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	var pkg struct {
		Name         string            `json:"name"`
		Main         string            `json:"main"`
		Dependencies map[string]string `json:"dependencies"`
		Optional     map[string]string `json:"optionalDependencies"`
		Peer         map[string]string `json:"peerDependencies"`
		Bundled      []string          `json:"bundledDependencies"`
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
	// 内置 forge 本体：安装插件即装本体、卸载即移除（PATH 上没有 forge 时 hook 全部静默
	// fail-open 的根治）。除它之外不得再有运行时依赖；钉必须是精确版本，不是范围——
	// 与 release-please extra-files lockstep bump 配套（TestReleasePleaseManifest_DshPluginTracksTrain）。
	for name, ver := range pkg.Dependencies {
		if name != "@agent_forge/forge" {
			t.Errorf("runtime dependency %q crept in — the only allowed one is the bundled @agent_forge/forge (devDependencies for tests are fine)", name)
			continue
		}
		if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(ver) {
			t.Errorf("@agent_forge/forge pin %q must be an exact version (lockstep with the plugin), not a range", ver)
		}
	}
	if len(pkg.Optional) > 0 || len(pkg.Peer) > 0 || len(pkg.Bundled) > 0 {
		t.Errorf("optional/peer/bundled dependencies crept in (%v %v %v) — the bundled forge in dependencies is the only runtime package", pkg.Optional, pkg.Peer, pkg.Bundled)
	}
	if _, ok := pkg.Dependencies["@agent_forge/forge"]; !ok {
		t.Error("@agent_forge/forge dependency missing — without it a plugin-only install gates nothing (every hook fails open)")
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

// TestDshPluginLibImportsStayDependencyFree: the bundled forge dependency is a binary carrier only. Plugin code must still load anywhere dsh runs without a module graph, so shipped lib files import only node: builtins and sibling files.
//
// TestDshPluginLibImportsStayDependencyFree：内置 forge 依赖只是二进制载体。插件代码仍须
// 在 dsh 运行的任何布局下无需模块图即可加载——发布的 lib 文件只许 import node: 内建与同包相对路径。
func TestDshPluginLibImportsStayDependencyFree(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(dshPluginDir, "lib", "*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no lib/*.js found: %v", err)
	}
	// 引号不限单双、import 可无 from（副作用导入）、可不在行首、from 前后空白可省。
	staticRe := regexp.MustCompile(`\b(?:import|export)\s*(?:[^;'"]*?\bfrom\s*)?['"]([^'"]+)['"]`)
	dynamicRe := regexp.MustCompile(`\bimport\s*\(\s*(['"]?)([^'")]*)`)
	requireRe := regexp.MustCompile(`\brequire\s*\(`)
	for _, f := range files {
		if strings.HasSuffix(f, ".test.js") {
			continue
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		name := filepath.Base(f)
		okSpec := func(spec string) bool {
			return strings.HasPrefix(spec, "node:") || (strings.HasPrefix(spec, "./") && !strings.Contains(spec, ".."))
		}
		for _, m := range staticRe.FindAllStringSubmatch(src, -1) {
			if !okSpec(m[1]) {
				t.Errorf("%s imports %q — shipped plugin code may import only node: builtins and ./ siblings", name, m[1])
			}
		}
		for _, m := range dynamicRe.FindAllStringSubmatch(src, -1) {
			if m[1] == "" || !okSpec(m[2]) {
				t.Errorf("%s has dynamic import(%s%s) — only literal node:/./ specifiers are allowed", name, m[1], m[2])
			}
		}
		if requireRe.MatchString(src) {
			t.Errorf("%s calls require(...) — shipped plugin code is ESM; use a ./ or node: import", name)
		}
		// createRequire is the escape hatch for resolving the bundled forge binary by path;
		// it may live only in bundled.js, which never loads what it resolves.
		if name != "bundled.js" && strings.Contains(src, "createRequire") {
			t.Errorf("%s uses createRequire — only bundled.js may (it resolves the forge binary path, never imports it)", name)
		}
	}
}

// TestDshPluginShipsEveryImportedFile: every `./x.js` a shipped lib file imports must be listed in package.json "files". A new module that is imported but not listed passes every in-repo test (the checkout has the file) and then breaks the published package at load time — the host fails to import the plugin and no gate runs.
//
// TestDshPluginShipsEveryImportedFile：发布的 lib 文件 import 的每个 `./x.js` 都必须在
// package.json 的 "files" 里。新模块被 import 却没列入，仓库内测试全绿（checkout 里有该
// 文件），发布后插件在加载期就 import 失败——宿主加载不起插件，所有门禁都不跑。
func TestDshPluginShipsEveryImportedFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(dshPluginDir, "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	var pkg struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("unmarshal package.json: %v", err)
	}
	shipped := map[string]bool{}
	for _, f := range pkg.Files {
		shipped[f] = true
	}
	files, err := filepath.Glob(filepath.Join(dshPluginDir, "lib", "*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no lib/*.js found: %v", err)
	}
	importRe := regexp.MustCompile(`\b(?:import|export)\s*(?:[^;'"]*?\bfrom\s*)?['"](\./[^'"]+)['"]`)
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasSuffix(name, ".test.js") {
			continue
		}
		if !shipped["lib/"+name] {
			t.Errorf("lib/%s is not in package.json files — it would be missing from the published package", name)
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range importRe.FindAllStringSubmatch(string(body), -1) {
			rel := "lib/" + strings.TrimPrefix(m[1], "./")
			if !shipped[rel] {
				t.Errorf("%s imports %s, which package.json files does not ship — the published plugin fails to load", name, m[1])
			}
		}
	}
}

// TestDshTranslator_Translate pins the deliberate no-op contract (the plugin is the only wiring path): Translate succeeds and writes NOTHING.
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
