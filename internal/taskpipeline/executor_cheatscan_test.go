package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// findCheatScanEntry 在 checklog 里找 CheckCheatScan 条目（指针，便于读字段）。
func findCheatScanEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == checklog.CheckCheatScan {
			return &entries[i]
		}
	}
	return nil
}

// TestExecuteTaskGate_CheatScan_RecordsAdvisory 核心契约：committed 源码含
// dead-branch → task-verify 记一条 CheckCheatScan（Passed=false、deterministic），
// 且 gate 照常 PASS（advisory 不阻塞——这些是机械检测的疑似模式，留痕供 review
// 核查而非拦死）。
func TestExecuteTaskGate_CheatScan_RecordsAdvisory(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"cheat.go": "package main\n\n" +
			"// @ts-ignore 测试用\n" +
			"func Dead() { if false { panic(1) } }\n",
	}, "add cheat")

	state := newVerifyState(t, dir, "cheat-gate")

	var stderr string
	var execErr error
	stderr = captureStderr(t, func() {
		_, execErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if execErr != nil {
		t.Fatalf(`task-verify 应 PASS（advisory 不阻塞）, got err: %v`, execErr)
	}

	rec := findCheatScanEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckCheatScan 条目未记录——task-verify 未跑 cheat-scan`)
	}
	if rec.Passed {
		t.Errorf(`含 dead-branch/@ts-ignore，CheckCheatScan 应 Passed=false`)
	}
	if !rec.Checked {
		t.Errorf(`CheckCheatScan 应 Checked=true`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`CheckCheatScan 应 deterministic（gate 实算）, got %s`, rec.Source)
	}
	if !strings.Contains(rec.Detail, "dead-branch") {
		t.Errorf(`Detail 应含 dead-branch 计数: %q`, rec.Detail)
	}
	if !strings.Contains(stderr, "cheat-scan") {
		t.Errorf(`stderr 应含 cheat-scan advisory: %s`, stderr)
	}
}

// TestExecuteTaskGate_CheatScan_Clean 干净代码 → CheckCheatScan Passed=true（仍记录，
// trace 可见"扫过、干净"）。确认扫描器在 task-verify 总是跑（不只命中时才记）。
func TestExecuteTaskGate_CheatScan_Clean(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"clean.go": "package main\n\nfunc Add(a, b int) int { return a + b }\n",
	}, "add clean")

	state := newVerifyState(t, dir, "clean-gate")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`task-verify 应 PASS: %v`, err)
	}
	rec := findCheatScanEntry(t, dir)
	if rec == nil {
		t.Fatal(`即便干净也应记 CheckCheatScan（trace 可见"扫过"）`)
	}
	if !rec.Passed {
		t.Errorf(`干净代码应 Passed=true, Detail=%q`, rec.Detail)
	}
}

// TestExecuteTaskGate_CheatScan_NonSourceNotScanned 无源码变更时 Detail 反映"无可扫
// 新增行"——ScanCheatPatterns 对空 added 返回 nil，gate 仍记 Passed=true。
func TestExecuteTaskGate_CheatScan_NonSourceNotScanned(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"README.md": "# project\n",
	}, "doc only")

	state := newVerifyState(t, dir, "doc-gate")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`task-verify 应 PASS: %v`, err)
	}
	rec := findCheatScanEntry(t, dir)
	if rec == nil {
		t.Fatal(`无源码变更也应记 CheckCheatScan（Passed=true）`)
	}
	if !rec.Passed {
		t.Errorf(`无源码变更应 Passed=true, Detail=%q`, rec.Detail)
	}
}
