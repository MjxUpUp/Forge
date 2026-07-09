package registry

import (
	"encoding/json"
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

// TestList_PrunesDeadAndWritesBack 钉死 dogfood 1.4：List 检测到死路径/重复条目时惰性写回
// 精简版——清理 e2e subprocess 注册的 Temp 垃圾 + 已淡出项目，让 projects.json 收敛
// （dogfood 实测 1819 条/1814 垃圾）。写仅在检测到失效时发生；下次 List 读到已精简的。
func TestList_PrunesDeadAndWritesBack(t *testing.T) {
	home := useTempHome(t)
	a := mkForgeProject(t)
	fake := t.TempDir() // 无 .forge，登记后即死路径

	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	if err := Add(fake); err != nil {
		t.Fatal(err)
	}
	// 手动注入重复条目（模拟历史脏数据 / 并发写残留）
	pj := filepath.Join(home, `projects.json`)
	data, err := os.ReadFile(pj)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	f.Projects = append(f.Projects, f.Projects[0]) // 复制 a 造成重复
	pruned, _ := json.MarshalIndent(f, ``, `  `)
	if err := os.WriteFile(pj, append(pruned, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	// List 应过滤死路径(fake) + 重复，且写回精简版
	got := List()
	if len(got) != 1 || got[0] != filepath.Clean(a) {
		t.Fatalf("List=%v want [%s]（死路径+重复过滤后）", got, filepath.Clean(a))
	}
	// 写回后磁盘 JSON 应只剩 a（死路径 + 重复被精简）
	data2, err := os.ReadFile(pj)
	if err != nil {
		t.Fatal(err)
	}
	var f2 File
	if err := json.Unmarshal(data2, &f2); err != nil {
		t.Fatal(err)
	}
	if len(f2.Projects) != 1 || filepath.Clean(f2.Projects[0]) != filepath.Clean(a) {
		t.Errorf("写回后 projects.json=%v want [%s]（死路径+重复应被精简）", f2.Projects, filepath.Clean(a))
	}
}
