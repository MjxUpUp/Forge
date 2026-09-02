package registry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// policy_test.go — Project Policy Layer P1 的状态模型契约测试：SetStatus/State/
// IsMember 联动、Add 不复活 declined、Rekey 保留状态字段、ListManaged 过滤。
// 行为契约见 docs/design/project-policy-layer.md「行为契约」表。

// entryOf 从注册表文件读回指定路径的条目（测试断言落盘状态用）。
func entryOf(t *testing.T, path string) Entry {
	t.Helper()
	f, ok := readFile()
	if !ok {
		t.Fatal(`registry: readFile failed`)
	}
	for _, e := range f.Projects {
		if pathKey(filepath.Clean(e.Path)) == pathKey(filepath.Clean(path)) {
			return e
		}
	}
	t.Fatalf(`registry: entry %s not found`, path)
	return Entry{}
}

// TestSetStatus_DeclineResume 钉住核心生命周期：Add→managed，decline→IsMember
// false + State declined + 落盘审计字段，resume→managed。declined→managed 的
// 唯一合法路径是 SetStatus（forge on）；Add（dashboard 自登记等）不得复活。
func TestSetStatus_DeclineResume(t *testing.T) {
	useTempHome(t)
	p := mkForgeProject(t)

	if err := Add(p); err != nil {
		t.Fatal(err)
	}
	if _, ok := IsMember(p); !ok {
		t.Fatal(`IsMember false right after Add`)
	}

	if err := SetStatus(p, StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}
	if _, ok := IsMember(p); ok {
		t.Error(`IsMember true after decline`)
	}
	root, state := State(p)
	if state != StatusDeclined {
		t.Errorf(`State = %q, want %q`, state, StatusDeclined)
	}
	if root == `` {
		t.Error(`State returned empty root for declined entry`)
	}

	// 落盘字段：status + 决策审计（by/at）。
	e := entryOf(t, p)
	if !e.IsDeclined() {
		t.Errorf(`on-disk status = %q, want declined`, e.Status)
	}
	if e.DecisionBy != `forge off` {
		t.Errorf(`DecisionBy = %q, want "forge off"`, e.DecisionBy)
	}
	if e.DecisionAt.IsZero() {
		t.Error(`DecisionAt zero after SetStatus`)
	}

	// Add upsert（dashboard 自登记 / legacyFind 自愈）不得复活 declined。
	if err := Add(p); err != nil {
		t.Fatal(err)
	}
	if _, state := State(p); state != StatusDeclined {
		t.Errorf(`Add resurrected declined entry: state = %q`, state)
	}

	if err := SetStatus(p, StatusManaged, `forge on`); err != nil {
		t.Fatal(err)
	}
	if _, ok := IsMember(p); !ok {
		t.Error(`IsMember false after resume`)
	}
}

// TestSetStatus_DeclineUnregistered 从未登记的项目也能直接 decline（首次接触前
// 退出，forge off 的语义）：SetStatus 建立 declined 条目而非要求先 Add。
func TestSetStatus_DeclineUnregistered(t *testing.T) {
	useTempHome(t)
	p := mkForgeProject(t)

	if err := SetStatus(p, StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}
	if _, ok := IsMember(p); ok {
		t.Error(`IsMember true for declined-only entry`)
	}
	if _, state := State(p); state != StatusDeclined {
		t.Errorf(`State = %q, want declined`, state)
	}
}

// TestState_Unknown 无条目 → StatusUnknown、空 root。
func TestState_Unknown(t *testing.T) {
	useTempHome(t)
	p := mkForgeProject(t)

	root, state := State(p)
	if state != StatusUnknown {
		t.Errorf(`State = %q, want %q`, state, StatusUnknown)
	}
	if root != `` {
		t.Errorf(`State root = %q, want empty`, root)
	}
}

// TestIsMember_DeclinedPrefix 非 git 前缀匹配路径上 declined 不赋予成员资格，
// 但 State 仍报 declined（status 显示与 Find 判定都依赖它）。
func TestIsMember_DeclinedPrefix(t *testing.T) {
	useTempHome(t)
	parent := mkForgeProject(t)
	sub := filepath.Join(parent, `sub`)
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	if err := Add(parent); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(parent, StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}

	if _, ok := IsMember(sub); ok {
		t.Error(`IsMember(sub) true under declined parent`)
	}
	if _, state := State(sub); state != StatusDeclined {
		t.Errorf(`State(sub) = %q, want declined`, state)
	}
}

// TestListManaged_FiltersDeclined ListManaged 只含 managed 存活条目；
// List 保持全量（declined 项目仍是"已登记"，workspace doctor 等需要看到）。
func TestListManaged_FiltersDeclined(t *testing.T) {
	useTempHome(t)
	a := mkForgeProject(t)
	b := mkForgeProject(t)

	if err := Add(a); err != nil {
		t.Fatal(err)
	}
	if err := Add(b); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(a, StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}

	managed := ListManaged()
	if len(managed) != 1 || managed[0] != filepath.Clean(b) {
		t.Errorf(`ListManaged = %v, want [%s]`, managed, b)
	}
	all := List()
	if len(all) != 2 {
		t.Errorf(`List = %v, want both entries`, all)
	}
}

// TestRekey_PreservesStatus Rekey 改 key 时整条目迁移——原实现重建
// Entry{Path,Key} 会丢 Status/决策字段（对抗复查 M7），declined 条目 rekey 后
// 必须仍是 declined。
func TestRekey_PreservesStatus(t *testing.T) {
	useTempHome(t)
	p := mkForgeProject(t)

	if err := SetStatus(p, StatusDeclined, `forge off`); err != nil {
		t.Fatal(err)
	}
	oldKey := entryKey(p) // 与 Add/SetStatus 同一 key 推导（非 git 目录回落 PathKey）

	if _, err := Rekey(oldKey, `zzrekeytarget01`); err != nil {
		t.Fatal(err)
	}
	e := entryOf(t, p)
	if !e.IsDeclined() {
		t.Errorf(`Rekey dropped status: %+v`, e)
	}
	if e.DecisionBy != `forge off` {
		t.Errorf(`Rekey dropped DecisionBy: %+v`, e)
	}
}

// TestLegacyJSON_NoStatusIsManaged 存量 projects.json（无 status 字段）全部视为
// managed（零值兼容），且 Add upsert 不无谓注入 status 键——升级 forge 不改写
// 既有条目形态、不改变成员资格。
func TestLegacyJSON_NoStatusIsManaged(t *testing.T) {
	home := useTempHome(t)
	p := mkForgeProject(t)
	if err := os.WriteFile(filepath.Join(home, `projects.json`),
		[]byte("{\n  \"projects\": [\n    {\"path\": "+strconv.Quote(filepath.Clean(p))+", \"key\": \"abc123\"}\n  ]\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, ok := IsMember(p); !ok {
		t.Error(`legacy entry without status must be managed member`)
	}
	if err := Add(p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, `projects.json`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"status"`)) {
		t.Errorf("Add injected status key into legacy entry:\n%s", data)
	}
	if _, ok := IsMember(p); !ok {
		t.Error(`membership lost after Add on legacy entry`)
	}
}

// TestSetStatus_ErrDeclinedProject sentinel 可被 errors.Is 判定——包括被 %w 包装
// 后（Find/CLI 若包裹该错误，分支判定必须仍成立；裸 == 比较在包装后即失效）。
func TestSetStatus_ErrDeclinedProject(t *testing.T) {
	wrapped := fmt.Errorf(`projectroot: %w`, ErrDeclinedProject)
	if !errors.Is(wrapped, ErrDeclinedProject) {
		t.Fatal(`errors.Is must see through %w wrapping`)
	}
}

// TestConcurrentAddsNoLostEntries 写锁守卫：并发 Add 不丢条目（P2 起 projects.json
// 是多宿主热写目标；无锁时 read-modify-write 后写覆盖先写）。同时并发 Add +
// SetStatus 同一项目：终态一致（managed 或 declined 之一），文件不损坏。
func TestConcurrentAddsNoLostEntries(t *testing.T) {
	useTempHome(t)
	const n = 24
	projects := make([]string, n)
	for i := range projects {
		projects[i] = mkForgeProject(t)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, n*2)
	for _, p := range projects {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			errCh <- Add(p)
		}(p)
	}
	// 混入对第一个项目的并发状态翻转（真实场景：hook 自登记与 forge off 并发）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- SetStatus(projects[0], StatusDeclined, `race-off`)
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf(`并发写失败: %v`, err)
		}
	}

	// 终态断言：全部条目在场（无丢失）+ 文件可解析。
	got := List()
	if len(got) != n {
		t.Fatalf(`并发 Add 丢条目：want %d, got %d (%v)`, n, len(got), got)
	}
	if _, state := State(projects[0]); state != StatusDeclined {
		t.Errorf(`SetStatus 与 Add 竞态后状态 = %q, want declined（锁应保 SetStatus 后写胜出）`, state)
	}
}
