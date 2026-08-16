package registry

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
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

// TestList_SkipsDeadPath: registered paths that no longer exist never appear in List
// (project fades out). After user-level-assets, liveness is judged by the project path
// itself — init writes no project-level .forge/ anymore.
//
// TestList_SkipsDeadPath 已登记但不复存在的路径不出现在 List（项目淡出）。
// user-level-assets 之后存活按项目路径本身判定——init 不再写项目级 .forge/。
func TestList_SkipsDeadPath(t *testing.T) {
	useTempHome(t)
	real := mkForgeProject(t)
	fake := filepath.Join(t.TempDir(), `gone`) // 不存在的路径

	if err := Add(real); err != nil {
		t.Fatal(err)
	}
	if err := Add(fake); err != nil {
		t.Fatal(err)
	}
	got := List()
	if len(got) != 1 || got[0] != filepath.Clean(real) {
		t.Errorf(`List 应仅含存活项目, got %v`, got)
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

// TestList_ProjectRemoved: a registered project whose directory is later removed stops
// appearing in List.
//
// TestList_ProjectRemoved 项目登记后目录被删，List 不再返回它。
func TestList_ProjectRemoved(t *testing.T) {
	useTempHome(t)
	a := mkForgeProject(t)
	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	// Simulate the project being moved away: delete the project dir.
	//
	// 模拟项目移走：删掉项目目录。
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	if got := List(); len(got) != 0 {
		t.Errorf(`项目目录删除后 List 应空, got %v`, got)
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
	fake := filepath.Join(t.TempDir(), `gone`) // 不存在的路径，登记后即死路径

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
	if len(f2.Projects) != 1 || filepath.Clean(f2.Projects[0].Path) != filepath.Clean(a) {
		t.Errorf("写回后 projects.json=%v want [%s]（死路径+重复应被精简）", f2.Projects, filepath.Clean(a))
	}
}

// TestPrune: explicit prune returns (pruned, remain) counts and writes back dead paths/duplicates. The dogfood registry cure entry point.
//
// TestPrune 显式精简返回 (pruned, remain) 计数 + 写回死路径/重复。dogfood registry 治本入口。
func TestPrune(t *testing.T) {
	home := useTempHome(t)
	a := mkForgeProject(t)
	fake := filepath.Join(t.TempDir(), `gone`) // 不存在的路径，死路径

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

// TestAdd_CorruptRegistryBackedUpAndRebuilds pins the silent-wipe fix: when projects.json is
// corrupt, Add must back it aside (projects.json.corrupt-<ts>) and warn on stderr before
// rebuilding from empty — the old code swallowed the Unmarshal error and atomically overwrote
// the registry with just the current project, silently dropping every other registration.
//
// TestAdd_CorruptRegistryBackedUpAndRebuilds 钉死静默清空修复：projects.json 损坏时，
// Add 必须先把它备份到一边（projects.json.corrupt-<ts>）并 stderr 告警，再从空表
// 重建——旧代码吞掉 Unmarshal 错误后把仅含当前项目的表原子覆盖回去，其他所有登记
// 被静默丢弃。
func TestAdd_CorruptRegistryBackedUpAndRebuilds(t *testing.T) {
	home := useTempHome(t)
	a := mkForgeProject(t)

	pj := filepath.Join(home, `projects.json`)
	corrupt := []byte(`{"projects": ["/old/project", broken`)
	if err := os.WriteFile(pj, corrupt, 0644); err != nil {
		t.Fatal(err)
	}

	if err := Add(a); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The corrupt file must be backed aside, not silently overwritten.
	//
	// 损坏文件必须被备份到一边，不是静默覆盖。
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), `projects.json.corrupt-`) {
			backup = e.Name()
			break
		}
	}
	if backup == "" {
		t.Fatal("损坏的 projects.json 应被备份为 projects.json.corrupt-<ts>，未找到")
	}
	got, err := os.ReadFile(filepath.Join(home, backup))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Errorf("备份内容 = %q, want 原损坏内容 %q", got, corrupt)
	}

	// The rebuilt registry holds the newly added project and stays valid JSON.
	//
	// 重建后的注册表含有新登记项目且仍是合法 JSON。
	data, err := os.ReadFile(pj)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("重建后的 projects.json 应为合法 JSON: %v", err)
	}
	if len(f.Projects) != 1 || filepath.Clean(f.Projects[0].Path) != filepath.Clean(a) {
		t.Errorf("重建注册表 = %v, want [%s]", f.Projects, filepath.Clean(a))
	}
}

// TestList_KeepsEntryWhenStatErrorIsNotNotExist pins the prune-condition fix: List may only
// drop an entry when os.Stat reports IsNotExist (project truly gone). Any other stat error
// (permission, invalid path, I/O) means "unreadable right now", not "disappeared" — the entry
// must be kept. A NUL byte in the path triggers a portable invalid-argument error on every OS.
//
// TestList_KeepsEntryWhenStatErrorIsNotNotExist 钉死 prune 条件修复：List 只允许在
// os.Stat 报 IsNotExist（项目真没了）时丢条目。其他 stat 错误（权限、非法路径、
// I/O）是「此刻不可读」而非「已消失」——条目必须保留。路径里的 NUL 字节在所有
// 平台都能可移植地触发 invalid-argument 错误。
func TestList_KeepsEntryWhenStatErrorIsNotNotExist(t *testing.T) {
	home := useTempHome(t)
	a := mkForgeProject(t)

	bogus := "bad\x00path" // os.Stat → invalid argument, NOT IsNotExist
	f := File{Projects: []Entry{{Path: a}, {Path: bogus}}}
	data, err := json.MarshalIndent(f, ``, `  `)
	if err != nil {
		t.Fatal(err)
	}
	pj := filepath.Join(home, `projects.json`)
	if err := os.WriteFile(pj, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	got := List()
	if len(got) != 2 {
		t.Fatalf("List = %v, want 2 条（stat 非 IsNotExist 错误的条目必须保留）", got)
	}
	foundBogus := false
	for _, p := range got {
		if p == filepath.Clean(bogus) {
			foundBogus = true
		}
	}
	if !foundBogus {
		t.Errorf("不可读但未消失的条目应保留在 List: %v", got)
	}
}

// TestDedupe_CaseInsensitive_Windows pins Windows path-equality: the filesystem is
// case-insensitive, so C:\Proj and c:\proj are the same project — both Add idempotence and
// List dedupe must treat them as one entry.
//
// TestDedupe_CaseInsensitive_Windows 钉死 Windows 路径相等：文件系统大小写不敏感，
// C:\Proj 与 c:\proj 是同一个项目——Add 幂等和 List 去重都必须视为一条。
func TestDedupe_CaseInsensitive_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("路径大小写不敏感去重仅适用于 Windows 文件系统")
	}
	home := useTempHome(t)
	a := mkForgeProject(t)

	// Swap the drive-letter case to build a case variant of the same path.
	//
	// 交换盘符大小写，构造同一路径的大小写变体。
	vol := filepath.VolumeName(a)
	variantVol := strings.ToLower(vol)
	if variantVol == vol {
		variantVol = strings.ToUpper(vol)
	}
	variant := variantVol + a[len(vol):]
	if variant == a {
		t.Skip("无法构造大小写变体（盘符异常）")
	}

	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	if err := Add(variant); err != nil {
		t.Fatal(err)
	}
	if got := List(); len(got) != 1 {
		t.Fatalf("Add 大小写变体应幂等: List = %v, want 1 条", got)
	}

	// List dedupe must also collapse both casings already inside the JSON.
	//
	// List 去重也必须合并 JSON 里已有的两种大小写。
	f := File{Projects: []Entry{{Path: a}, {Path: variant}}}
	data, err := json.MarshalIndent(f, ``, `  `)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, `projects.json`), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	if got := List(); len(got) != 1 {
		t.Fatalf("List 应按大小写不敏感去重: List = %v, want 1 条", got)
	}
}

// TestAdd_AtomicWriteLeavesNoResidue pins the writeFile → util.AtomicWrite
// switch: Add persists the registry with no temp-file residue behind.
//
// TestAdd_AtomicWriteLeavesNoResidue 钉住 writeFile → util.AtomicWrite 切换：
// Add 落盘注册表后无临时文件残留。
func TestAdd_AtomicWriteLeavesNoResidue(t *testing.T) {
	useTempHome(t)
	proj := mkForgeProject(t)
	if err := Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}
	gp, err := globalPath()
	if err != nil {
		t.Fatalf("globalPath: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(gp))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("atomic write residue left behind: %s", e.Name())
		}
	}
}

// mkGitRepo creates a fake git main worktree: a temp dir with a .git directory.
// forgedata.Key only stats .git, so no real git binary is needed.
//
// mkGitRepo 建一个假 git 主 worktree：临时目录 + .git 目录。forgedata.Key 只
// stat .git，无需真实 git 二进制。
func mkGitRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, `.git`), 0755); err != nil {
		t.Fatal(err)
	}
	return d
}

// mkWorktree creates a fake linked worktree of mainDir: a temp dir whose .git file
// points at mainDir/.git, so both dirs share one forge key.
//
// mkWorktree 建 mainDir 的假 linked worktree：临时目录的 .git 文件指向
// mainDir/.git，两个目录共享同一 forge key。
func mkWorktree(t *testing.T, mainDir string) string {
	t.Helper()
	d := t.TempDir()
	content := `gitdir: ` + filepath.Join(mainDir, `.git`) + "\n"
	if err := os.WriteFile(filepath.Join(d, `.git`), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return d
}

// readEntries reads the registry JSON directly (no List filtering) so tests can
// inspect stored Path/Key exactly as persisted.
//
// readEntries 直接读注册表 JSON（不经 List 过滤），让测试能按落盘原样检查
// 存储的 Path/Key。
func readEntries(t *testing.T, home string) []Entry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, `projects.json`))
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f.Projects
}

// TestAdd_SameKeyKeepsLivePath pins the worktree fix: registering from a worktree of
// an already-registered repo must NOT rewrite the stored path to the worktree path —
// otherwise deleting the worktree lets List prune the whole entry (key included) and
// the main project silently loses membership. The live main path is kept; only the
// key is refreshed.
//
// TestAdd_SameKeyKeepsLivePath 钉死 worktree 修复：已登记 repo 的 worktree 里触发
// 登记不得把存储路径改写成 worktree 路径——否则 worktree 删除后 List 会把整条
// （含 key）prune 掉，主项目静默丢成员资格。旧路径仍活则保留，只刷新 key。
func TestAdd_SameKeyKeepsLivePath(t *testing.T) {
	home := useTempHome(t)
	main := mkGitRepo(t)
	wt := mkWorktree(t, main)

	if err := Add(main); err != nil {
		t.Fatal(err)
	}
	if err := Add(wt); err != nil {
		t.Fatal(err)
	}

	entries := readEntries(t, home)
	if len(entries) != 1 {
		t.Fatalf("同 key 登记应为 1 条（upsert），实得 %d: %v", len(entries), entries)
	}
	if filepath.Clean(entries[0].Path) != filepath.Clean(main) {
		t.Errorf("旧路径仍活时不得换成 worktree 路径: Path=%s, want %s", entries[0].Path, main)
	}
}

// TestAdd_SameKeyUpdatesPathWhenOldGone: two worktrees share one common .git dir
// (one forge key); when the registered worktree's path no longer exists, re-adding
// from the sibling worktree updates the stored path (the project "moved").
//
// TestAdd_SameKeyUpdatesPathWhenOldGone：两个 worktree 共享同一 common .git dir
// （同一 forge key）；已登记 worktree 路径不复存在时，从兄弟 worktree 再登记
// 会更新存储路径（项目「移动」语义）。
func TestAdd_SameKeyUpdatesPathWhenOldGone(t *testing.T) {
	home := useTempHome(t)
	// Common dir named .git so resolveGitFile's .git-ancestor walk resolves both
	// worktrees to the same key.
	//
	// common dir 命名为 .git，让 resolveGitFile 的 .git 祖先查找把两个 worktree
	// 解析到同一 key。
	common := filepath.Join(t.TempDir(), `.git`)
	if err := os.MkdirAll(common, 0755); err != nil {
		t.Fatal(err)
	}
	mkWT := func() string {
		d := t.TempDir()
		content := `gitdir: ` + common + "\n"
		if err := os.WriteFile(filepath.Join(d, `.git`), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return d
	}
	wt1 := mkWT()
	wt2 := mkWT()

	if err := Add(wt1); err != nil {
		t.Fatal(err)
	}
	// The registered worktree disappears (deleted); the sibling worktree lives on.
	//
	// 已登记 worktree 消失（被删）；兄弟 worktree 仍在。
	if err := os.RemoveAll(wt1); err != nil {
		t.Fatal(err)
	}
	if err := Add(wt2); err != nil {
		t.Fatal(err)
	}

	entries := readEntries(t, home)
	if len(entries) != 1 {
		t.Fatalf("同 key 登记应为 1 条，实得 %d: %v", len(entries), entries)
	}
	if filepath.Clean(entries[0].Path) != filepath.Clean(wt2) {
		t.Errorf("旧路径已死时应更新为新路径: Path=%s, want %s", entries[0].Path, wt2)
	}
}

// TestUnmarshal_SkipsEmptyPathEntries: null and {} entries in projects.json carry no
// path and are skipped defensively instead of becoming ghost entries.
//
// TestUnmarshal_SkipsEmptyPathEntries：projects.json 里的 null 与 {} 条目无 path，
// 防御性跳过，不变成幽灵条目。
func TestUnmarshal_SkipsEmptyPathEntries(t *testing.T) {
	var f File
	data := []byte(`{"projects": [null, {}, {"path": "/x", "key": "k1"}, "legacy"]}`)
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Projects) != 2 {
		t.Fatalf("null/{} 条目应被跳过: 实得 %d 条 %v, want 2", len(f.Projects), f.Projects)
	}
	if f.Projects[0].Path != `/x` || f.Projects[0].Key != `k1` {
		t.Errorf("entry[0] = %+v, want {Path:/x Key:k1}", f.Projects[0])
	}
	if f.Projects[1].Path != `legacy` {
		t.Errorf("entry[1] = %+v, want legacy string entry {Path:legacy}", f.Projects[1])
	}
}

// TestIsMember_ExactMatchCaseInsensitive_Windows pins pathKey normalization on the
// exact-match branch: C:\Proj registered, c:\proj queried — same project on a
// case-insensitive filesystem.
//
// TestIsMember_ExactMatchCaseInsensitive_Windows 钉死精确匹配分支的 pathKey 归一：
// 登记 C:\Proj、查询 c:\proj——大小写不敏感文件系统下是同一项目。
func TestIsMember_ExactMatchCaseInsensitive_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("路径大小写不敏感匹配仅适用于 Windows 文件系统")
	}
	useTempHome(t)
	a := mkForgeProject(t)
	if err := Add(a); err != nil {
		t.Fatal(err)
	}

	vol := filepath.VolumeName(a)
	variantVol := strings.ToLower(vol)
	if variantVol == vol {
		variantVol = strings.ToUpper(vol)
	}
	variant := variantVol + a[len(vol):]
	if variant == a {
		t.Skip("无法构造大小写变体（盘符异常）")
	}

	root, ok := IsMember(variant)
	if !ok {
		t.Fatalf("大小写变体精确路径应命中成员: IsMember(%q) = (%q, false)", variant, root)
	}
	if filepath.Clean(root) != filepath.Clean(a) {
		t.Errorf("root = %q, want %q", root, a)
	}
}

// TestIsMember_SymlinkResolved: a cwd reached through a symlink must match the
// registered physical path (EvalSymlinks normalization, same semantics as PathKey).
// Skipped when the platform/user cannot create symlinks (Windows needs privilege).
//
// TestIsMember_SymlinkResolved：经 symlink 进入的 cwd 必须匹配已登记的物理路径
// （EvalSymlinks 归一，与 PathKey 同语义）。平台/用户无法创建 symlink 时跳过
// （Windows 需要权限）。
func TestIsMember_SymlinkResolved(t *testing.T) {
	useTempHome(t)
	real := mkForgeProject(t)
	if err := Add(real); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(t.TempDir(), `link`)
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("无法创建 symlink（权限不足或平台限制）: %v", err)
	}
	root, ok := IsMember(link)
	if !ok {
		t.Fatalf("symlink 路径应命中成员: IsMember(%q) = (%q, false)", link, root)
	}
	// Compare resolved physical paths: IsMember legitimately returns the lexical
	// git root (link form), and on macOS t.TempDir() itself is /var→/private/var
	// symlink form — lexical equality is the wrong assertion; physical identity
	// is the contract.
	//
	// 比较解析后的物理路径：IsMember 返回字面 git root（link 形态）是合法的，
	// 且 macOS 上 t.TempDir() 本身就是 /var→/private/var 的 symlink 形态——
	// 字面相等是错误的断言，物理同一路径才是契约。
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(root): %v", err)
	}
	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks(real): %v", err)
	}
	if resolvedRoot != resolvedReal {
		t.Errorf("resolved root = %q, want %q（同一物理目录）", resolvedRoot, resolvedReal)
	}
}

// gitInit turns dir into a real git repo, skipping the test when git is unavailable. Used by
// the key-drift test which needs the project to be git AFTER a non-git registration.
//
// gitInit 把 dir 变成真实 git 仓库，git 不可用时跳过测试。key 漂移测试需要项目在非 git
// 登记之后才变 git。
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git 不在 PATH，跳过 git 相关测试: %v", err)
	}
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, out)
	}
}

// TestIsMember_GitKeyDriftFromPathKey pins the path↔git key-drift fallback: a project
// forge-init'd while NON-git (registry entry stores a PathKey) that later ran `git init`
// must still resolve as a member. The git branch's key match misses the stale path-key
// (keyOf trusts the stored non-empty key), so a path-based fallback against the git
// working-tree root must catch it. This is the AgentOffice bug: forge init at 16:11
// (non-git → path-key pc3f3d8507069) → git init at 16:35 → IsMember returned false →
// forge forgot the project → all enforcement hooks degraded to allow-and-exit →
// reasonix "不走协议". Read-only (no self-heal write): IsMember is a hot path invoked
// from concurrent hook processes; the stale key is re-keyed by the next `forge init`
// (Add upsert).
//
// TestIsMember_GitKeyDriftFromPathKey 钉死 path↔git key 漂移的路径回退：项目在非 git
// 状态下 forge init（注册表存 PathKey），后来跑了 `git init`，仍须解析为成员。git 分支
// 的 key 匹配命中不了陈旧 path-key（keyOf 信任已存的非空 key），故按 git working-tree
// 根做路径回退兜住。这正是 AgentOffice bug：16:11 非 git init（→ path-key
// pc3f3d8507069）→ 16:35 git init → IsMember 返 false → forge 遗忘项目 → 所有强制
// hook 降级放行 → reasonix「不走协议」。只读（不自愈写回）：IsMember 是被并发 hook
// 进程调用的热路径，陈旧 key 由下次 `forge init`（Add upsert）刷新。
func TestIsMember_GitKeyDriftFromPathKey(t *testing.T) {
	useTempHome(t)
	d := t.TempDir()
	gitInit(t, d) // make it a real git project (the post-git-init state)

	// Simulate the stale registry state: registered as non-git (PathKey) BEFORE git init.
	// Writing the entry directly (not via Add) is deterministic — Add now would compute the
	// correct git-key and never reproduce the drift.
	//
	// 直接写陈旧注册表状态：git init 之前以非 git（PathKey）登记。直接写条目（而非 Add）
	// 是确定性的——现在跑 Add 会算出正确的 git-key，复现不了漂移。
	staleKey := forgedata.PathKey(d)
	if err := writeEntries([]Entry{{Path: d, Key: staleKey}}); err != nil {
		t.Fatal(err)
	}
	// Sanity: the live git-key differs from the stale path-key (precondition for the bug).
	// 健全性：实时 git-key 与陈旧 path-key 不同（bug 的前置条件）。
	liveKey, err := forgedata.Key(d)
	if err != nil {
		t.Fatalf("forgedata.Key: %v", err)
	}
	if liveKey == staleKey {
		t.Fatalf("前置条件不成立：git-key == path-key（%q），漂移场景未触发", liveKey)
	}

	// The project root itself must resolve.
	// 项目根本身须解析。
	root, ok := IsMember(d)
	if !ok {
		t.Fatalf("git-key 漂移场景应命中成员（路径回退）: IsMember(%q) = (%q, false)", d, root)
	}
	if filepath.Clean(root) != filepath.Clean(d) {
		t.Errorf("root = %q, want %q", root, d)
	}

	// A cwd DEEP inside the drifted project must also resolve (git root == project root) —
	// mirrors reasonix editing a file deep in AgentOffice.
	// 漂移项目深处的 cwd 也须解析（git 根 == 项目根）——对应 reasonix 在 AgentOffice
	// 深处改文件的场景。
	sub := filepath.Join(d, "src", "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	root2, ok2 := IsMember(sub)
	if !ok2 {
		t.Fatalf("漂移项目子目录应命中成员: IsMember(%q) = (%q, false)", sub, root2)
	}
	if filepath.Clean(root2) != filepath.Clean(d) {
		t.Errorf("子目录 root = %q, want %q", root2, d)
	}
}
