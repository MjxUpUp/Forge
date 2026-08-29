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

// TestExecuteTaskGate_CheatScan_RecordsAdvisory core contract: committed source
// containing dead-branch → task-verify records a CheckCheatScan entry
// (Passed=false, deterministic), and the gate still PASSES (advisory does not
// block.
//
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

// TestExecuteTaskGate_CheatScan_Clean clean code → CheckCheatScan Passed=true
// (still recorded, so trace shows scanned-and-clean).
//
// TestExecuteTaskGate_CheatScan_Clean 干净代码 → CheckCheatScan Passed=true（仍记录，
// trace 可见「扫过、干净」）。确认扫描器在 task-verify 总是跑（不只命中时才记）。
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

// TestExecuteTaskGate_CheatScan_NonSourceNotScanned when there is no source
// change, Detail reflects no lines to scan.
//
// TestExecuteTaskGate_CheatScan_NonSourceNotScanned 无源码变更时 Detail 反映「无可扫
// 新增行」——ScanCheatPatterns 对空 added 返回 nil，gate 仍记 Passed=true。
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

// TestExecuteTaskGate_CheatScan_PhantomImport end-to-end: a committed TS file
// importing a relative path that does not exist on disk → CheckCheatScan
// Passed=false with a phantom-import count in Detail.
//
// TestExecuteTaskGate_CheatScan_PhantomImport 端到端：committed 的 TS 文件 import 一个
// 磁盘上不存在的相对路径 → CheckCheatScan Passed=false 且 Detail 含 phantom-import
// 计数；gate 照常通过（advisory）。钉住新检测器确实接进了 gate 路径，而非只有单测。
func TestExecuteTaskGate_CheatScan_PhantomImport(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"app.ts": "import { ghost } from './ghost'\n\nexport const x = ghost\n",
	}, "add phantom import")

	state := newVerifyState(t, dir, "phantom-gate")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`task-verify 应 PASS（advisory 不阻塞）: %v`, err)
	}
	rec := findCheatScanEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckCheatScan 条目未记录`)
	}
	if rec.Passed {
		t.Errorf(`含幽灵相对 import，CheckCheatScan 应 Passed=false, Detail=%q`, rec.Detail)
	}
	if !strings.Contains(rec.Detail, "phantom-import=1") {
		t.Errorf(`Detail 应含 phantom-import=1: %q`, rec.Detail)
	}
}

// TestExecuteTaskGate_CheatScan_DedupSuffix pins the audit-side dedup annotation
// (2026-08 review-observability): re-running task-verify over an unchanged diff
// re-records the full scan result (audit truth, Passed=false) but the Detail now
// carries the fresh/suppressed breakdown.
//
// TestExecuteTaskGate_CheatScan_DedupSuffix 钉住审计侧的去重标注（2026-08 评审
// 可观测性）：对同一 diff 重跑 task-verify 仍记录全量扫描结果（审计真相，
// Passed=false），但 Detail 带上新发现/被抑制拆分——重扫时为
// 「new=0, suppressed=N」——使重复 FAIL 条目与真正的新命中可区分。
// 首次扫描（全部为新）不带后缀。
func TestExecuteTaskGate_CheatScan_DedupSuffix(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"cheat.go": "package main\n\nfunc Dead() { if false { panic(1) } }\n",
	}, "add cheat")

	state := newVerifyState(t, dir, "dedup-gate")

	captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf(`第 1 次 task-verify 应 PASS: %v`, err)
		}
	})
	captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf(`第 2 次 task-verify 应 PASS: %v`, err)
		}
	})

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	var scans []checklog.Entry
	for _, e := range entries {
		if e.Check == checklog.CheckCheatScan {
			scans = append(scans, e)
		}
	}
	if len(scans) != 2 {
		t.Fatalf(`两次 verify 应记 2 条 CheckCheatScan, got %d`, len(scans))
	}
	if scans[0].Passed || scans[1].Passed {
		t.Errorf(`两条条目仍应 Passed=false（全量审计真相）: %+v`, scans)
	}
	if strings.Contains(scans[0].Detail, "new=") {
		t.Errorf(`首次扫描全部为新，Detail 不应带去重后缀: %q`, scans[0].Detail)
	}
	if !strings.Contains(scans[1].Detail, "; new=0, suppressed=") {
		t.Errorf(`重扫同一 diff，Detail 应含「new=0, suppressed=N」拆分: %q`, scans[1].Detail)
	}
}
