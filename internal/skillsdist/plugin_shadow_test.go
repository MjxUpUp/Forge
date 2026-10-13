package skillsdist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestPluginShadows 钉住插件同名遮蔽检测：只报非 forge 插件里与核心同名且带 SKILL.md
// 的 skill；同一插件多 scope 去重；注册表缺失返回空；格式损坏报错。
func TestPluginShadows(t *testing.T) {
	home := t.TempDir()
	mkSkill := func(installPath, name string, withSkillMD bool) {
		t.Helper()
		dir := filepath.Join(installPath, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if withSkillMD {
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	core := filepath.Join(home, "plugins", "cache", "forge", "forge", "abc")
	design := filepath.Join(home, "plugins", "cache", "forge", "forge-design", "old")
	other := filepath.Join(home, "plugins", "cache", "x", "other", "1.0")
	mkSkill(core, "design-audit", true)   // 核心插件自身：不算遮蔽
	mkSkill(design, "design-audit", true) // 旧扩展包：遮蔽
	mkSkill(design, "design-artifact-standards", true)
	mkSkill(design, "frontend-code-review", true) // 非核心名：不报
	mkSkill(other, "design-audit", false)         // 无 SKILL.md：不报
	// 第三方插件里链接到目录的同名 skill：须跟随符号链接报出（审查轮 Minor 4）
	linked := filepath.Join(home, "elsewhere", "doc-review")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linked, filepath.Join(other, "skills", "doc-review")); err != nil {
		t.Fatal(err)
	}

	if got, err := PluginShadows(home, []string{"design-audit"}); err != nil || got != nil {
		t.Fatalf("注册表缺失应返回空, got %v %v", got, err)
	}

	reg := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"forge@forge":        []map[string]string{{"scope": "user", "installPath": core}},
			"forge-design@forge": []map[string]string{{"scope": "user", "installPath": design}, {"scope": "project", "installPath": design}},
			"other@x":            []map[string]string{{"scope": "user", "installPath": other}},
		},
	}
	data, _ := json.Marshal(reg)
	if err := os.WriteFile(filepath.Join(home, "plugins", "installed_plugins.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := PluginShadows(home, []string{"design-audit", "design-artifact-standards", "doc-review"})
	if err != nil {
		t.Fatal(err)
	}
	want := []PluginShadow{
		{Skill: "design-artifact-standards", Plugin: "forge-design@forge", Path: filepath.Join(design, "skills", "design-artifact-standards"), FromForgeMarketplace: true},
		{Skill: "design-audit", Plugin: "forge-design@forge", Path: filepath.Join(design, "skills", "design-audit"), FromForgeMarketplace: true},
		{Skill: "doc-review", Plugin: "other@x", Path: filepath.Join(other, "skills", "doc-review")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shadows = %+v\nwant %+v", got, want)
	}

	if err := os.WriteFile(filepath.Join(home, "plugins", "installed_plugins.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PluginShadows(home, []string{"design-audit"}); err == nil {
		t.Fatal("注册表损坏应报错")
	}
}
