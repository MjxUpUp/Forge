package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestCreateAddFindRemove covers the CRUD spine: create → add → find → workpaces-for → remove, including the duplicate-name refusal and the same-key upsert (add twice = one member, refreshed path).
//
// TestCreateAddFindRemove 覆盖 CRUD 主干：create → add → find →
// workspaces-for → remove，含重名拒绝与同 key upsert（add 两次 = 一个成员、
// 路径被刷新）。
func TestCreateAddFindRemove(t *testing.T) {
	f := &File{}

	if err := f.Create(``); err == nil {
		t.Error(`空名 Create 应报错`)
	}
	if err := f.Create(`fleet`); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := f.Create(`fleet`); err == nil {
		t.Error(`重名 Create 应报错`)
	}

	if err := f.AddRepo(`ghost`, RepoRef{Key: `k1`}); err == nil {
		t.Error(`向不存在的 workspace AddRepo 应报错`)
	}
	if err := f.AddRepo(`fleet`, RepoRef{Key: `k1`, Path: `/old`}); err != nil {
		t.Fatalf("AddRepo k1: %v", err)
	}
	if err := f.AddRepo(`fleet`, RepoRef{Key: `k2`, Path: `/two`}); err != nil {
		t.Fatalf("AddRepo k2: %v", err)
	}
	// Upsert：同 key 刷新展示路径，绝不重复。
	if err := f.AddRepo(`fleet`, RepoRef{Key: `k1`, Path: `/new`}); err != nil {
		t.Fatalf("AddRepo k1 upsert: %v", err)
	}
	w := f.Find(`fleet`)
	if w == nil {
		t.Fatal(`Find(fleet) 应命中`)
	}
	if len(w.Repos) != 2 {
		t.Fatalf("upsert 后成员数 = %d, want 2（不得重复）", len(w.Repos))
	}
	if w.Repos[0].Key != `k1` || w.Repos[0].Path != `/new` {
		t.Errorf("upsert 应刷新 k1 的展示路径, got %+v", w.Repos[0])
	}
	if w.CreatedAt.IsZero() {
		t.Error(`CreatedAt 应在 Create 时落章`)
	}

	ok, err := f.RemoveRepo(`fleet`, `k1`)
	if err != nil || !ok {
		t.Errorf("RemoveRepo k1 = (%v, %v), want (true, nil)", ok, err)
	}
	ok, err = f.RemoveRepo(`fleet`, `k1`)
	if err != nil || ok {
		t.Errorf("RemoveRepo 非成员 = (%v, %v), want (false, nil)", ok, err)
	}
	if _, err = f.RemoveRepo(`ghost`, `k1`); err == nil {
		t.Error(`从不存在的 workspace RemoveRepo 应报错`)
	}
	if got := f.Find(`fleet`); len(got.Repos) != 1 || got.Repos[0].Key != `k2` {
		t.Errorf("remove 后成员 = %+v, want 仅 k2", got.Repos)
	}
}

// TestWorkspacesFor_MultiMembership pins the design decision that one key may belong to several workspaces: WorkspacesFor must return ALL of them (the gate and doctor both depend on seeing the full overlap set).
//
// TestWorkspacesFor_MultiMembership 钉住「一个 key 可属于多个 workspace」的
// 设计决策：WorkspacesFor 必须全量返回（门禁与 doctor 都依赖看到完整重叠集）。
func TestWorkspacesFor_MultiMembership(t *testing.T) {
	f := &File{}
	for _, name := range []string{`a`, `b`, `c`} {
		if err := f.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	mustAdd := func(ws, key string) {
		if err := f.AddRepo(ws, RepoRef{Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	mustAdd(`a`, `shared`)
	mustAdd(`a`, `only-a`)
	mustAdd(`b`, `shared`)
	mustAdd(`c`, `only-c`)

	got := f.WorkspacesFor(`shared`)
	if len(got) != 2 {
		t.Fatalf("WorkspacesFor(shared) = %d 个, want 2（a+b）", len(got))
	}
	names := map[string]bool{}
	for _, w := range got {
		names[w.Name] = true
	}
	if !names[`a`] || !names[`b`] {
		t.Errorf("WorkspacesFor(shared) 应含 a 与 b, got %v", names)
	}
	if got := f.WorkspacesFor(`only-c`); len(got) != 1 || got[0].Name != `c` {
		t.Errorf("WorkspacesFor(only-c) = %+v, want 仅 c", got)
	}
	if got := f.WorkspacesFor(`nobody`); len(got) != 0 {
		t.Errorf("WorkspacesFor(nobody) 应空, got %+v", got)
	}
}

// TestSaveLoadRoundTrip: Save → Load returns an equivalent manifest, and the file lands at <FORGE_DATA_HOME>/workspaces.json (the projects.json sibling rule).
//
// TestSaveLoadRoundTrip：Save → Load 读回等价清单，且落点在
// <FORGE_DATA_HOME>/workspaces.json（与 projects.json 平级规则）。
func TestSaveLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)

	f := &File{}
	if err := f.Create(`fleet`); err != nil {
		t.Fatal(err)
	}
	if err := f.AddRepo(`fleet`, RepoRef{Key: `k1`, Path: `/repo/one`}); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, `workspaces.json`)); err != nil {
		t.Fatalf("workspaces.json 应落在 FORGE_DATA_HOME 下: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	w := got.Find(`fleet`)
	if w == nil || len(w.Repos) != 1 || w.Repos[0].Key != `k1` || w.Repos[0].Path != `/repo/one` {
		t.Errorf("Load 回读 = %+v, want fleet[k1@/repo/one]", got)
	}
}

// TestLoad_MissingIsEmpty: no file → empty File, no error (read path contract, same as registry: empty = no workspaces, not an error).
//
// TestLoad_MissingIsEmpty：无文件 → 空 File、无错误（读路径契约，与 registry
// 一致：空 = 没有 workspace，非错误）。
func TestLoad_MissingIsEmpty(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	f, err := Load()
	if err != nil {
		t.Fatalf("缺失文件 Load 不应报错: %v", err)
	}
	if len(f.Workspaces) != 0 {
		t.Errorf("缺失文件应为空清单, got %+v", f)
	}
}

// TestCorruptBackupAndRebuild pins the registry-style corruption contract.
//
// TestCorruptBackupAndRebuild 钉住 registry 同款损坏契约：读路径（Load）返回
// 错误（门禁 fail-open 依赖它）；写路径（LoadForWrite）把文件备份为
// workspaces.json.corrupt-*（保留原字节）后从空重建。
func TestCorruptBackupAndRebuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	bad := []byte(`{"workspaces": [broken`)
	p := filepath.Join(home, `workspaces.json`)
	if err := os.WriteFile(p, bad, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Fatal(`损坏 JSON 时 Load 必须报错（只读调用方据此 fail-open）`)
	}

	f, err := LoadForWrite()
	if err != nil {
		t.Fatalf("LoadForWrite 应备份重建而非报错: %v", err)
	}
	if len(f.Workspaces) != 0 {
		t.Errorf("重建后应为空清单, got %+v", f)
	}

	// 损坏字节必须保留在备份里。
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), `workspaces.json.corrupt-`) {
			backup = e.Name()
		}
	}
	if backup == `` {
		t.Fatal(`应生成 workspaces.json.corrupt-<ts> 备份`)
	}
	got, err := os.ReadFile(filepath.Join(home, backup))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Errorf("备份内容 = %q, want 原损坏内容 %q", got, bad)
	}

	// 重建后的文件可正常写、读。
	if err := f.Create(`fleet`); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save after rebuild: %v", err)
	}
	if _, err := Load(); err != nil {
		t.Fatalf("重建后 Load 应正常: %v", err)
	}
}

// TestSave_AtomicNoTempLeftover: AtomicWrite renames its temp file over the target — a stray .tmp-* next to workspaces.json would mean the write path is no longer atomic.
//
// TestSave_AtomicNoTempLeftover：AtomicWrite 把临时文件 rename 覆盖目标——
// workspaces.json 旁残留 .tmp-* 说明写路径不再原子。
func TestSave_AtomicNoTempLeftover(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	f := &File{}
	if err := f.Create(`fleet`); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != `workspaces.json` {
			t.Errorf("意外残留文件: %s", e.Name())
		}
	}
}

// TestJSONShape pins the wire format (the manifest is user-inspectable JSON; an accidental key rename would strand existing files).
//
// TestJSONShape 钉住线上格式（清单是用户可检视的 JSON；字段名被误改会让
// 存量文件读不出）。
func TestJSONShape(t *testing.T) {
	data := []byte(`{"workspaces":[{"name":"fleet","repos":[{"key":"k1","path":"/p"}],"created_at":"2026-08-26T00:00:00Z"}]}`)
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	w := f.Find(`fleet`)
	if w == nil || len(w.Repos) != 1 || w.Repos[0].Key != `k1` {
		t.Fatalf("磁盘形态应能读回, got %+v", f)
	}
}

// TestDoctor exercises every drift class against a fake live registry (non-git temp dirs → PathKey identity, matching registryKeys' fallback).
//
// TestDoctor 用假注册表（非 git 临时目录 → PathKey 身份，对应 registryKeys
// 的回落）覆盖全部 drift 类别。
func TestDoctor(t *testing.T) {
	liveA := t.TempDir() // 注册表里的活路径
	liveB := t.TempDir()
	staleCache := t.TempDir() // 仍存在但已非 keyB 现路径的旧缓存
	keyA := forgedata.PathKey(liveA)
	keyB := forgedata.PathKey(liveB)

	f := &File{}
	for _, name := range []string{`fleet`, `overlap`, `empty`} {
		if err := f.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	add := func(ws string, r RepoRef) {
		if err := f.AddRepo(ws, r); err != nil {
			t.Fatal(err)
		}
	}
	add(`fleet`, RepoRef{Key: keyA, Path: liveA})          // 健康成员
	add(`fleet`, RepoRef{Key: `ghost-key`, Path: `/gone`}) // 不在注册表
	add(`overlap`, RepoRef{Key: keyA, Path: liveA})        // 同 key 第二 workspace
	add(`overlap`, RepoRef{Key: keyB, Path: staleCache})   // 缓存路径与现路径不符（旧路径仍存活）

	findings := f.Doctor([]string{liveA, liveB})
	byKind := map[string][]Finding{}
	for _, fd := range findings {
		byKind[fd.Kind] = append(byKind[fd.Kind], fd)
	}

	if got := byKind[DriftNotRegistered]; len(got) != 1 || got[0].Key != `ghost-key` {
		t.Errorf("not-registered = %+v, want 1 条 ghost-key", got)
	}
	if got := byKind[DriftMultiWorkspace]; len(got) != 1 || got[0].Key != keyA {
		t.Errorf("multi-workspace = %+v, want 1 条 keyA", got)
	}
	if got := byKind[DriftEmpty]; len(got) != 1 || got[0].Workspace != `empty` {
		t.Errorf("empty = %+v, want 1 条 empty workspace", got)
	}
	// /gone 挂在未注册成员上：not-registered 已报，不应再叠加 path-missing
	// （key 解析不出时路径比对无意义）。
	if got := byKind[DriftPathMissing]; len(got) != 0 {
		t.Errorf("path-missing = %+v, want 0（未注册成员不再叠加路径检查）", got)
	}
	if got := byKind[DriftPathMismatch]; len(got) != 1 || got[0].Key != keyB || got[0].RegistryPath != liveB {
		t.Errorf("path-mismatch = %+v, want 1 条 keyB（registry 现路径 liveB）", got)
	}
}

// TestDoctor_MissingCachedPath: the cached display path died on disk while the key still resolves in the registry → path-missing with the current path attached for the fix hint.
//
// TestDoctor_MissingCachedPath：缓存展示路径在磁盘上消失但 key 仍能在注册表
// 解析 → path-missing，并附上现路径供修复提示。
func TestDoctor_MissingCachedPath(t *testing.T) {
	live := t.TempDir()
	dead := filepath.Join(t.TempDir(), `deleted`)
	key := forgedata.PathKey(live)

	f := &File{}
	if err := f.Create(`fleet`); err != nil {
		t.Fatal(err)
	}
	if err := f.AddRepo(`fleet`, RepoRef{Key: key, Path: dead}); err != nil {
		t.Fatal(err)
	}
	findings := f.Doctor([]string{live})
	if len(findings) != 1 || findings[0].Kind != DriftPathMissing {
		t.Fatalf("应只报 1 条 path-missing, got %+v", findings)
	}
	if findings[0].RegistryPath != live {
		t.Errorf("path-missing 应带 registry 现路径 %q, got %q", live, findings[0].RegistryPath)
	}
}

// TestDoctor_Healthy: a manifest whose members all resolve cleanly reports nothing (doctor must stay silent on health — noise trains users to ignore it).
//
// TestDoctor_Healthy：成员全部健康解析的清单零报告（doctor 必须在健康时
// 静默——噪音会训练用户无视它）。
func TestDoctor_Healthy(t *testing.T) {
	live := t.TempDir()
	key := forgedata.PathKey(live)
	f := &File{}
	if err := f.Create(`fleet`); err != nil {
		t.Fatal(err)
	}
	if err := f.AddRepo(`fleet`, RepoRef{Key: key, Path: live}); err != nil {
		t.Fatal(err)
	}
	if got := f.Doctor([]string{live}); len(got) != 0 {
		t.Errorf("健康清单应零 finding, got %+v", got)
	}
}
