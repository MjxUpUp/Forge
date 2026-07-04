package registry

import (
	"os"
	"path/filepath"
	"testing"
)

// useTempHome 把全局 home（FORGE_DATA_HOME）重定向到临时目录，测试间隔离（不污染真实
// ~/.forge/projects.json）。返回 home 根，projects.json=home/projects.json。
// refactor-data-home commit E：registry 改用 forgedata.GlobalHome()（FORGE_DATA_HOME），
// 废弃旧的 HomeDir 变量注入。
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	return home
}

// mkForgeProject 在临时目录建一个含 .forge/ 的项目根，返回其路径。
func mkForgeProject(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	return d
}

// TestAdd_List 去重 + 排序 + 仅含 .forge 的项目。
func TestAdd_List(t *testing.T) {
	useTempHome(t)
	a := mkForgeProject(t)
	b := mkForgeProject(t)

	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	if err := Add(b); err != nil {
		t.Fatal(err)
	}

	got := List()
	if len(got) != 2 {
		t.Fatalf(`List len = %d, want 2 (%v)`, len(got), got)
	}
	// 排序后 a 在前（按路径字典序，临时目录路径决定）。
	if got[0] != filepath.Clean(a) && got[1] != filepath.Clean(a) {
		t.Errorf(`项目 a 未登记: %v`, got)
	}
}

// TestAdd_Idempotent 同路径重复 Add，List 只一条。
func TestAdd_Idempotent(t *testing.T) {
	useTempHome(t)
	a := mkForgeProject(t)
	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	if err := Add(filepath.Join(a, `sub`, `..`)); err != nil { // 不同写法同路径
		t.Fatal(err)
	}
	if got := List(); len(got) != 1 {
		t.Errorf(`幂等失败: List = %v, want 1 条`, got)
	}
}

// TestList_SkipsNonForge 无 .forge/ 的路径登记了也不出现（项目淡出）。
func TestList_SkipsNonForge(t *testing.T) {
	useTempHome(t)
	real := mkForgeProject(t)
	fake := t.TempDir() // 无 .forge

	if err := Add(real); err != nil {
		t.Fatal(err)
	}
	if err := Add(fake); err != nil {
		t.Fatal(err)
	}
	got := List()
	if len(got) != 1 || got[0] != filepath.Clean(real) {
		t.Errorf(`List 应仅含真实 forge 项目, got %v`, got)
	}
}

// TestList_NoRegistry 无注册表文件时 List 返回 nil（空，非错误）。
func TestList_NoRegistry(t *testing.T) {
	useTempHome(t)
	if got := List(); got != nil {
		t.Errorf(`无注册表时 List = %v, want nil`, got)
	}
}

// TestList_ProjectRemoved 项目登记后被删（.forge 移走），List 不再返回它。
func TestList_ProjectRemoved(t *testing.T) {
	useTempHome(t)
	a := mkForgeProject(t)
	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	// 模拟项目移走：删掉 .forge/。
	if err := os.RemoveAll(filepath.Join(a, `.forge`)); err != nil {
		t.Fatal(err)
	}
	if got := List(); len(got) != 0 {
		t.Errorf(`项目 .forge 删除后 List 应空, got %v`, got)
	}
}

// TestRegistry_UsesForgeDataHome 钉死 refactor-data-home commit E：registry 必须走
// forgedata.GlobalHome()（FORGE_DATA_HOME），不再用废弃的 FORGE_HOME env。projects.json
// 落 FORGE_DATA_HOME 根（home/projects.json，不是 home/.forge/projects.json），且设
// FORGE_HOME 不应影响 List（旧 env 已废弃）。
func TestRegistry_UsesForgeDataHome(t *testing.T) {
	home := useTempHome(t)
	a := mkForgeProject(t)
	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	// projects.json 必须在 FORGE_DATA_HOME 根（home/projects.json），不在 home/.forge/。
	pj := filepath.Join(home, `projects.json`)
	if _, err := os.Stat(pj); err != nil {
		t.Fatalf("projects.json must land at FORGE_DATA_HOME/projects.json (%s), got: %v", pj, err)
	}
	// 设 FORGE_HOME 不应影响（废弃 env）——registry 必须仍读 FORGE_DATA_HOME。
	t.Setenv(`FORGE_HOME`, t.TempDir())
	got := List()
	if len(got) != 1 || got[0] != filepath.Clean(a) {
		t.Errorf("FORGE_HOME must be ignored (deprecated commit E): List=%v, want [%s]", got, filepath.Clean(a))
	}
}
