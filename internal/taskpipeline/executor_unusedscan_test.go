package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// findUnusedScanEntry finds the CheckUnusedScan entry in the checklog (pointer, so fields are readable).
//
// findUnusedScanEntry 在 checklog 里找 CheckUnusedScan 条目（指针，便于读字段）。
func findUnusedScanEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == checklog.CheckUnusedScan {
			return &entries[i]
		}
	}
	return nil
}

// TestExecuteTaskGate_UnusedScan_RecordsAdvisory core contract: committed source containing an
// unreferenced export (Lonely) → task-verify records a CheckUnusedScan entry (Passed=false,
// deterministic), and the gate still PASSES (advisory does not block — mechanically detected
// suspect wiring miss, logged for review scrutiny rather than hard-blocked).
//
// TestExecuteTaskGate_UnusedScan_RecordsAdvisory 核心契约：committed 源码含未引用导出（Lonely）
// → task-verify 记一条 CheckUnusedScan（Passed=false、deterministic），且 gate 照常 PASS
// （advisory 不阻塞——机械检测的疑似接线缺失，留痕供 review 核查而非拦死）。
func TestExecuteTaskGate_UnusedScan_RecordsAdvisory(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\n" +
			"func main() { Used() }\n" +
			"func Used() int { return 1 }\n" +
			"func Lonely() int { return 2 }\n",
	}, "add unused")

	state := newVerifyState(t, dir, "unused-gate")

	var stderr string
	var execErr error
	stderr = captureStderr(t, func() {
		_, execErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if execErr != nil {
		t.Fatalf(`task-verify 应 PASS（advisory 不阻塞）, got err: %v`, execErr)
	}

	rec := findUnusedScanEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckUnusedScan 条目未记录——task-verify 未跑 unused-scan`)
	}
	if rec.Passed {
		t.Errorf(`含未引用导出 Lonely，CheckUnusedScan 应 Passed=false`)
	}
	if !rec.Checked {
		t.Errorf(`CheckUnusedScan 应 Checked=true`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`CheckUnusedScan 应 deterministic（gate 实算）, got %s`, rec.Source)
	}
	if !strings.Contains(rec.Detail, "Lonely") {
		t.Errorf(`Detail 应含 Lonely: %q`, rec.Detail)
	}
	if !strings.Contains(stderr, "unused-scan") {
		t.Errorf(`stderr 应含 unused-scan advisory: %s`, stderr)
	}
}

// TestExecuteTaskGate_UnusedScan_Clean clean code → CheckUnusedScan Passed=true (still recorded,
// so trace shows scanned-and-clean). Confirms the scanner always runs under task-verify.
//
// TestExecuteTaskGate_UnusedScan_Clean 干净代码 → CheckUnusedScan Passed=true（仍记录，trace 可见
// 「扫过、干净」）。确认扫描器在 task-verify 总是跑。
func TestExecuteTaskGate_UnusedScan_Clean(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"clean.go": "package main\n\nfunc main() { Use() }\nfunc Use() int { return 1 }\n",
	}, "add clean")

	state := newVerifyState(t, dir, "clean-gate")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`task-verify 应 PASS: %v`, err)
	}
	rec := findUnusedScanEntry(t, dir)
	if rec == nil {
		t.Fatal(`即便干净也应记 CheckUnusedScan（trace 可见"扫过"）`)
	}
	if !rec.Passed {
		t.Errorf(`干净代码应 Passed=true, Detail=%q`, rec.Detail)
	}
}

// TestExecuteTaskGate_UnusedScan_NonSourceNotScanned when there is no source change, the scan has
// nothing to extract — ScanUnusedSymbols returns nil, the gate still records Passed=true.
//
// TestExecuteTaskGate_UnusedScan_NonSourceNotScanned 无源码变更时扫描无可提取——ScanUnusedSymbols
// 返回 nil，gate 仍记 Passed=true。
func TestExecuteTaskGate_UnusedScan_NonSourceNotScanned(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"README.md": "# project\n",
	}, "doc only")

	state := newVerifyState(t, dir, "doc-gate")
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`task-verify 应 PASS: %v`, err)
	}
	rec := findUnusedScanEntry(t, dir)
	if rec == nil {
		t.Fatal(`无源码变更也应记 CheckUnusedScan（Passed=true）`)
	}
	if !rec.Passed {
		t.Errorf(`无源码变更应 Passed=true, Detail=%q`, rec.Detail)
	}
}

// TestExecuteTaskGate_UnusedScan_SameFindingSuppressedOnRetry 钉住同 finding 抑制
// （2026-08 噪音审计：Translate(method) 同任务重发 8 次）：同一 finding 在 verify
// 重试中不再逐条重发（stderr 只剩「均已报告」一行）；checklog 审计条目保持全量。
// finding 修复消失后出现的新 finding 仍照常报告。
//
// TestExecuteTaskGate_UnusedScan_SameFindingSuppressedOnRetry pins same-finding
// suppression (2026-08 noise audit: Translate(method) re-emitted 8 times on one task):
// the same finding is not re-emitted line by line on a verify retry (stderr keeps only
// the "all already reported" line); the checklog audit entry stays complete. A new
// finding appearing after the old one was fixed still reports normally.
func TestExecuteTaskGate_UnusedScan_SameFindingSuppressedOnRetry(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // 隔离 SaveTaskState 的全局 home
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\n" +
			"func main() { Used() }\n" +
			"func Used() int { return 1 }\n" +
			"func Lonely() int { return 2 }\n",
	}, "add unused")

	state := newVerifyState(t, dir, "unused-dedup")
	stderr1 := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf(`首次 task-verify 应 PASS: %v`, err)
		}
	})
	if !strings.Contains(stderr1, "unused-scan 发现 1 个") || !strings.Contains(stderr1, "Lonely") {
		t.Fatalf(`首次应逐条报告 Lonely: %q`, stderr1)
	}

	// 重试（从磁盘重载 state，模拟新一轮 forge 调用）：同一 finding 不再逐条重发。
	reloaded, err := LoadTaskState(dir, "unused-dedup")
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	stderr2 := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", reloaded); err != nil {
			t.Fatalf(`重试 task-verify 应 PASS: %v`, err)
		}
	})
	if strings.Contains(stderr2, "  ⚠ [") {
		t.Errorf(`重试不应逐条重发已报告 finding: %q`, stderr2)
	}
	if !strings.Contains(stderr2, "均已在本任务此前 verify 报告过") {
		t.Errorf(`重试应留「均已报告」说明行: %q`, stderr2)
	}

	// 修复 Lonely（接线）+ 引入新未接线符号 Lonely2：新 finding 照常报，旧指纹不再出现。
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\n" +
			"func main() { Used(); Lonely() }\n" +
			"func Used() int { return 1 }\n" +
			"func Lonely() int { return 2 }\n" +
			"func Lonely2() int { return 3 }\n",
	}, "wire Lonely, add Lonely2")
	reloaded2, err := LoadTaskState(dir, "unused-dedup")
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	stderr3 := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", reloaded2); err != nil {
			t.Fatalf(`第三次 task-verify 应 PASS: %v`, err)
		}
	})
	if !strings.Contains(stderr3, "Lonely2") {
		t.Errorf(`新 finding Lonely2 应照常报告: %q`, stderr3)
	}
}
