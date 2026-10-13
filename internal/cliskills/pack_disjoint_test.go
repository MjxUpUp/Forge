package cliskills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCoreAndPackSkillNamesDisjoint 钉住分发单一真相源：一个 skill 名只能住在核心
// skills/ 或某个扩展包 plugins/<pack>/skills/ 之一（plugins/forge 是核心镜像，不算
// 扩展包）。两处同名 = 宿主同时加载两份、行为以先加载者为准——2026-10 把
// design-audit / design-artifact-standards 收回核心时即须同步从扩展包移除。
func TestCoreAndPackSkillNamesDisjoint(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile))) // internal/cliskills → repo root
	core := skillNamesIn(t, filepath.Join(root, "skills"))
	if len(core) == 0 {
		t.Fatal("核心 skills/ 未找到任何 skill——仓库布局变了？")
	}
	packs, err := filepath.Glob(filepath.Join(root, "plugins", "*", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range packs {
		pack := filepath.Base(filepath.Dir(dir))
		if pack == "forge" {
			continue // 核心镜像
		}
		checked++
		for name := range skillNamesIn(t, dir) {
			if core[name] {
				t.Errorf("skill %q 同时在核心 skills/ 与扩展包 plugins/%s/skills/ ——只能保留一处", name, pack)
			}
		}
	}
	if checked == 0 {
		t.Fatal("未找到任何扩展包——守卫空转")
	}
}

// skillNamesIn returns the names of skill dirs (containing SKILL.md) under dir.
func skillNamesIn(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 %s: %v", dir, err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			out[e.Name()] = true
		}
	}
	return out
}
