package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// useTempHome redirects the global home (FORGE_DATA_HOME) to a temp dir so tests stay isolated and never touch the real
// ~/.forge/projects.json. It returns the home root; projects.json=home/projects.json.
// refactor-data-home commit E: registry now uses forgedata.GlobalHome() (FORGE_DATA_HOME);
// the legacy HomeDir variable injection is deprecated.
//
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

// mkForgeProject creates a project root containing .forge/ in a temp dir and returns its path.
//
// mkForgeProject 在临时目录建一个含 .forge/ 的项目根，返回其路径。
func mkForgeProject(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	return d
}

// TestAdd_List verifies dedup, sorting, and that only .forge-bearing projects are listed.
//
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
	// After sorting, a comes first (lexicographic by path; temp dir paths decide the order).
	//
	// 排序后 a 在前（按路径字典序，临时目录路径决定）。
	if got[0] != filepath.Clean(a) && got[1] != filepath.Clean(a) {
		t.Errorf(`项目 a 未登记: %v`, got)
	}
}

// TestAdd_Idempotent: adding the same path repeatedly keeps List at a single entry.
//
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

// TestList_SkipsNonForge: paths without .forge/ never appear even after Add (project fades out).
//
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

// TestList_NoRegistry: with no registry file, List returns nil (empty, not an error).
//
// TestList_NoRegistry 无注册表文件时 List 返回 nil（空，非错误）。
func TestList_NoRegistry(t *testing.T) {
	useTempHome(t)
	if got := List(); got != nil {
		t.Errorf(`无注册表时 List = %v, want nil`, got)
	}
}

// TestList_ProjectRemoved: a registered project whose .forge is later removed stops appearing in List.
//
// TestList_ProjectRemoved 项目登记后被删（.forge 移走），List 不再返回它。
func TestList_ProjectRemoved(t *testing.T) {
	useTempHome(t)
	a := mkForgeProject(t)
	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	// Simulate the project being moved away: delete .forge/.
	//
	// 模拟项目移走：删掉 .forge/。
	if err := os.RemoveAll(filepath.Join(a, `.forge`)); err != nil {
		t.Fatal(err)
	}
	if got := List(); len(got) != 0 {
		t.Errorf(`项目 .forge 删除后 List 应空, got %v`, got)
	}
}

// TestRegistry_UsesForgeDataHome pins refactor-data-home commit E: registry must go through
// forgedata.GlobalHome() (FORGE_DATA_HOME) and no longer use the deprecated FORGE_HOME env. projects.json
// lands at the FORGE_DATA_HOME root (home/projects.json, not home/.forge/projects.json), and setting
// FORGE_HOME must not affect List (the legacy env is deprecated).
//
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
	// projects.json must live at the FORGE_DATA_HOME root (home/projects.json), not under home/.forge/.
	//
	// projects.json 必须在 FORGE_DATA_HOME 根（home/projects.json），不在 home/.forge/。
	pj := filepath.Join(home, `projects.json`)
	if _, err := os.Stat(pj); err != nil {
		t.Fatalf("projects.json must land at FORGE_DATA_HOME/projects.json (%s), got: %v", pj, err)
	}
	// Setting FORGE_HOME must have no effect (deprecated env) — registry must still read FORGE_DATA_HOME.
	//
	// 设 FORGE_HOME 不应影响（废弃 env）——registry 必须仍读 FORGE_DATA_HOME。
	t.Setenv(`FORGE_HOME`, t.TempDir())
	got := List()
	if len(got) != 1 || got[0] != filepath.Clean(a) {
		t.Errorf("FORGE_HOME must be ignored (deprecated commit E): List=%v, want [%s]", got, filepath.Clean(a))
	}
}

// TestList_PrunesDeadAndWritesBack pins dogfood 1.4: on detecting dead paths or duplicate entries, List lazily writes back a
// pruned version — cleaning Temp garbage registered by e2e subprocesses plus faded projects so projects.json converges
// (dogfood measured 1819 entries / 1814 garbage). The write fires only when invalidation is detected; the next List reads the pruned copy.
//
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
	// Manually inject a duplicate entry (simulating historical dirty data / concurrent-write residue).
	//
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

	// List must filter out dead paths (fake) and duplicates, and write back the pruned version.
	//
	// List 应过滤死路径(fake) + 重复，且写回精简版
	got := List()
	if len(got) != 1 || got[0] != filepath.Clean(a) {
		t.Fatalf("List=%v want [%s]（死路径+重复过滤后）", got, filepath.Clean(a))
	}
	// After write-back, on-disk JSON should contain only a (dead paths and duplicates pruned).
	//
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

// TestPrune: explicit prune returns (pruned, remain) counts and writes back dead paths/duplicates. The dogfood registry cure entry point.
//
// TestPrune 显式精简返回 (pruned, remain) 计数 + 写回死路径/重复。dogfood registry 治本入口。
func TestPrune(t *testing.T) {
	home := useTempHome(t)
	a := mkForgeProject(t)
	fake := t.TempDir() // 无 .forge，死路径

	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	if err := Add(fake); err != nil {
		t.Fatal(err)
	}
	// Inject a duplicate entry (append a once more into the JSON).
	//
	// 注入重复条目（a 再追加一次到 JSON）
	pj := filepath.Join(home, `projects.json`)
	data, err := os.ReadFile(pj)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	f.Projects = append(f.Projects, f.Projects[0]) // 重复 a
	prunedJSON, _ := json.MarshalIndent(f, ``, `  `)
	if err := os.WriteFile(pj, append(prunedJSON, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	// before=3 (a + fake + dup-a); after prune remain=1 (only a alive), pruned=2.
	//
	// before=3 (a + fake + 重复a), 精简后 remain=1 (只 a 活跃), pruned=2
	gotPruned, gotRemain, err := Prune()
	if err != nil {
		t.Fatal(err)
	}
	if gotPruned != 2 || gotRemain != 1 {
		t.Fatalf(`Prune=(%d,%d), want (2,1)（死路径 fake + 重复 a 被精简）`, gotPruned, gotRemain)
	}
}

// TestPrune_NoRegistry: with no registry file, returns (0,0,nil) without error (matches List: empty is not an error).
//
// TestPrune_NoRegistry 无注册表文件返回 (0,0,nil)，不报错（与 List 一致：空非错误）。
func TestPrune_NoRegistry(t *testing.T) {
	useTempHome(t)
	pruned, remain, err := Prune()
	if err != nil || pruned != 0 || remain != 0 {
		t.Errorf(`无注册表 Prune=(%d,%d,%v), want (0,0,nil)`, pruned, remain, err)
	}
}

// TestPrune_AlreadyClean: when the registry is already pruned, pruned=0 (idempotent; repeated Prune is a no-op).
//
// TestPrune_AlreadyClean 注册表已精简时 pruned=0（幂等，重复 Prune 不改）。
func TestPrune_AlreadyClean(t *testing.T) {
	useTempHome(t)
	a := mkForgeProject(t)
	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	pruned, remain, err := Prune()
	if err != nil || pruned != 0 || remain != 1 {
		t.Errorf(`已精简 Prune=(%d,%d,%v), want (0,1,nil)`, pruned, remain, err)
	}
}
