package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// findScopeDriftEntry 在 checklog 里找 CheckScopeDrift 条目（指针，便于读字段）。
func findScopeDriftEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == checklog.CheckScopeDrift {
			return &entries[i]
		}
	}
	return nil
}

// TestExecuteTaskGate_ScopeDrift_RecordsAdvisory 钉住核心契约：任务声明了 PlanScope，
// 实改了声明外的源码 → task-verify 记一条 CheckScopeDrift（Passed=false、deterministic），
// 且 gate 照常 PASS（advisory 不阻塞——变更影响分析召回率仅 ~44%，scope 是 prediction）。
func TestExecuteTaskGate_ScopeDrift_RecordsAdvisory(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		`foo.go`: `package main

func Foo() int { return 1 }
`,
	}, `add foo (out-of-scope)`)

	state := newVerifyState(t, dir, `drift-task`)
	state.PlanScope = []string{`bar.go`} // foo.go 不在声明内 → drift

	var stderr string
	var execErr error
	stderr = captureStderr(t, func() {
		_, execErr = ExecuteTaskGate(dir, `task-verify`, state)
	})
	if execErr != nil {
		t.Fatalf(`task-verify 应 PASS（advisory 不阻塞）, got err: %v`, execErr)
	}

	rec := findScopeDriftEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckScopeDrift 条目未记录——task-verify 未检测 scope 偏差`)
	}
	if rec.Passed {
		t.Errorf(`foo.go 超出 PlanScope 声明，CheckScopeDrift 应 Passed=false`)
	}
	if !rec.Checked {
		t.Errorf(`CheckScopeDrift 应 Checked=true`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`CheckScopeDrift 应 deterministic（gate 实算）, got %s`, rec.Source)
	}
	if !strings.Contains(rec.Detail, `foo.go`) {
		t.Errorf(`CheckScopeDrift Detail 应含偏离文件 foo.go: %q`, rec.Detail)
	}
	if !strings.Contains(stderr, `scope-drift`) {
		t.Errorf(`stderr 应含 scope-drift advisory 提示: %s`, stderr)
	}
}

// TestExecuteTaskGate_ScopeDrift_PassesWhenInScope 实改文件全在声明内（含测试随源码覆盖）
// → CheckScopeDrift Passed=true。foo_test.go 随 foo.go 声明自动覆盖（不误报 drift）。
func TestExecuteTaskGate_ScopeDrift_PassesWhenInScope(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		`foo.go`: `package main

func Foo() int { return 1 }
`,
		`foo_test.go`: `package main

import "testing"

func TestFoo(t *testing.T) {}
`,
	}, `add foo + test (in-scope)`)

	state := newVerifyState(t, dir, `in-scope-task`)
	state.PlanScope = []string{`foo.go`} // foo.go + foo_test.go 均覆盖

	if _, err := ExecuteTaskGate(dir, `task-verify`, state); err != nil {
		t.Fatalf(`task-verify 应 PASS: %v`, err)
	}
	rec := findScopeDriftEntry(t, dir)
	if rec == nil {
		t.Fatal(`声明了 PlanScope 就应记 CheckScopeDrift（即便 Passed=true）`)
	}
	if !rec.Passed {
		t.Errorf(`全在声明内应 Passed=true, Detail=%q`, rec.Detail)
	}
}

// TestExecuteTaskGate_ScopeDrift_SkippedWhenNoScope 未声明 PlanScope → 不记 CheckScopeDrift
// （无声明即无 drift 可检测，避免噪声）。这是 advisory 的前提：scope 是 opt-in 契约。
func TestExecuteTaskGate_ScopeDrift_SkippedWhenNoScope(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		`foo.go`: `package main

func Foo() int { return 1 }
`,
	}, `add foo`)

	state := newVerifyState(t, dir, `no-scope-task`)
	// PlanScope 留空

	if _, err := ExecuteTaskGate(dir, `task-verify`, state); err != nil {
		t.Fatalf(`task-verify 应 PASS: %v`, err)
	}
	if rec := findScopeDriftEntry(t, dir); rec != nil {
		t.Errorf(`未声明 PlanScope 时不应记 CheckScopeDrift, got %+v`, rec)
	}
}

// TestExecuteTaskGate_ScopeDrift_NonSourceIgnored 改了声明外的非源码文件（README.md）
// → 不算 drift（drift 只对源码；改 README 是探索，非 scope 违约）。
func TestExecuteTaskGate_ScopeDrift_NonSourceIgnored(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		`README.md`: `# project
`,
		`foo.go`: `package main

func Foo() int { return 1 }
`,
		`foo_test.go`: `package main

import "testing"

func TestFoo(t *testing.T) {}
`,
	}, `add readme + foo`)

	state := newVerifyState(t, dir, `nonsource-task`)
	state.PlanScope = []string{`foo.go`} // README.md 超出但不计（非源码）

	if _, err := ExecuteTaskGate(dir, `task-verify`, state); err != nil {
		t.Fatalf(`task-verify 应 PASS: %v`, err)
	}
	rec := findScopeDriftEntry(t, dir)
	if rec == nil || !rec.Passed {
		t.Errorf(`非源码文件超出声明不计 drift，应 Passed=true, got %+v`, rec)
	}
}
