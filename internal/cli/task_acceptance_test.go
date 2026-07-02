package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupAcceptanceTask 建一个 session-scoped 活动任务并写入给定验收标准，返回 (dir, taskRef)。
// 复用 #2 review_status_test 的范式：SetActiveTaskRef + SaveTaskState，让 runTaskVerifyAcceptanceAt
// 经 ActiveTaskState(sessionID) 能找到任务（而非陈旧共享文件）。
func setupAcceptanceTask(t *testing.T, acceptRaw []string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	const sid = `test-session-accept`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)
	const taskRef = `feat/spec-persist`
	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	state := &taskpipeline.TaskState{
		TaskRef:    taskRef,
		SessionID:  sid,
		Branch:     `feat/spec-persist`,
		StartedAt:  time.Now(),
		Acceptance: taskpipeline.ParseAcceptance(acceptRaw),
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	return dir, taskRef
}

// findAcceptanceEntry 在 checklog 里找 CheckNameAcceptance 条目（指针，便于读字段）。
func findAcceptanceEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == taskpipeline.CheckNameAcceptance {
			return &entries[i]
		}
	}
	return nil
}

// TestRunTaskVerifyAcceptanceAt_RecordsDeterministic 是 #3 的核心守卫：绿验收标准实跑后，
// checklog 必须记 CheckNameAcceptance 条目、Passed=true、Source=deterministic（forge 自己跑
// 命令看结果，不可伪造），且 TaskState.Acceptance[].Passed 回填为 true。这是把 dev-workflow
// Plan 的 "Run+Expected" 从 plan 文本变成不可伪造证据的接入点——VerifyAcceptance / 记录接线 /
// Source 分桶任一断裂即被抓。
func TestRunTaskVerifyAcceptanceAt_RecordsDeterministic(t *testing.T) {
	dir, taskRef := setupAcceptanceTask(t, []string{`go version :: go version`})

	var runErr error
	out := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir) })
	if runErr != nil {
		t.Fatalf(`green acceptance should not error: %v`, runErr)
	}

	// TaskState 回填：Passed=true
	loaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if !loaded.Acceptance[0].Passed {
		t.Errorf(`criterion Passed 未回填为 true（实跑结果未落盘）`)
	}

	// checklog：CheckNameAcceptance / Passed=true / deterministic
	rec := findAcceptanceEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckNameAcceptance entry 未记录——verify-acceptance 未把实跑结果写入 checklog`)
	}
	if !rec.Passed {
		t.Errorf(`acceptance Passed = false on green, want true`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`acceptance Source = %s, want deterministic（forge 实跑应归 deterministic，非 agent-claim）`, rec.Source)
	}
	if !strings.Contains(out, `全部通过`) {
		t.Errorf(`输出缺"全部通过"摘要: %s`, out)
	}
}

// TestRunTaskVerifyAcceptanceAt_RecordsFailure 钉住 RED 路径：失败的验收标准也照常记一条
// CheckNameAcceptance（Passed=false、Checked=true、deterministic）、返回非 nil error，且失败
// criterion 回填 Output 供排查。没有本测试，未来若有人把 checklog.Record 挪进 `if allPassed`
// 分支，失败证据会被静默丢弃而 green-only 测试照过——正中 #3 要堵的"agent 自述满足验收"盲区。
func TestRunTaskVerifyAcceptanceAt_RecordsFailure(t *testing.T) {
	dir, taskRef := setupAcceptanceTask(t, []string{`go version :: NONEXISTENT_SUBSTRING`})

	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir) })
	if runErr == nil {
		t.Fatal(`failing acceptance should return a non-nil error`)
	}

	loaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if loaded.Acceptance[0].Passed {
		t.Errorf(`criterion Passed = true on red, want false（期望子串缺失应判失败）`)
	}
	if loaded.Acceptance[0].Output == `` {
		t.Errorf(`失败 criterion 应回填 Output 供排查（实跑输出未落盘）`)
	}

	rec := findAcceptanceEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckNameAcceptance entry 未记录 on failure——fail path 丢弃了记录`)
	}
	if rec.Passed {
		t.Errorf(`acceptance Passed = true on red, want false`)
	}
	if !rec.Checked {
		t.Errorf(`acceptance Checked = false, want true（失败也应标记已检查）`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`acceptance Source = %s, want deterministic（forge 实跑的失败也是 deterministic）`, rec.Source)
	}
}

// TestRunTaskVerifyAcceptanceAt_NoAcceptanceSilent 验证未登记验收标准的任务静默退出、
// 不写 checklog（不留噪声条目），且不报错。
func TestRunTaskVerifyAcceptanceAt_NoAcceptanceSilent(t *testing.T) {
	dir, _ := setupAcceptanceTask(t, nil) // 无验收标准

	var runErr error
	out := captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir) })
	if runErr != nil {
		t.Fatalf(`no-acceptance path should not error: %v`, runErr)
	}
	if !strings.Contains(out, `未登记`) {
		t.Errorf(`无验收标准时应提示"未登记": %s`, out)
	}
	entries, _ := checklog.LoadAll(dir)
	if len(entries) != 0 {
		t.Errorf(`无验收标准 → 不应写 checklog 条目，got %d`, len(entries))
	}
}

// TestTaskAcceptance_E2E_FlagToStatusToVerify 端到端钉住用户路径：task start --accept（多条，
// 验证 StringArray 不被逗号切分）→ task status 展示 ⏳ 未验证 → task verify-acceptance 实跑
// 全绿、记 deterministic 证据。覆盖 cobra flag 绑定 + 状态渲染 + 实跑记录的完整链路。
// 用真实 `go` 子命令（非 echo——Windows 无 echo.exe，strings.Fields+exec 路径会失败）。
func TestTaskAcceptance_E2E_FlagToStatusToVerify(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-accept`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}

	// 两条 --accept（StringArray：整条不切，含空格/:: 不被拆）。第二条 `go version ::` 是
	// 尾部裸 ::（无 expected），顺带验证 ParseAcceptance 的尾部 :: 兼容。
	startOut, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/spec-e2e`,
		`--accept`, `go version :: go version`,
		`--accept`, `go version ::`)
	if code != 0 {
		t.Fatalf(`task start --accept failed: %s`, startOut)
	}
	if !strings.Contains(startOut, `验收标准`) {
		t.Errorf(`task start 输出缺验收标准块: %s`, startOut)
	}

	// status：验收标准列为 ⏳ 未验证（实跑前）
	statusOut, _, code := runForge(t, dir, `task`, `status`)
	if code != 0 {
		t.Fatalf(`task status failed: %s`, statusOut)
	}
	for _, want := range []string{`验收标准`, `go version`, `未验证`} {
		if !strings.Contains(statusOut, want) {
			t.Errorf(`status 缺 %q: %s`, want, statusOut)
		}
	}

	// verify-acceptance：实跑全绿、exit 0、记 deterministic
	verifyOut, _, code := runForge(t, dir, `task`, `verify-acceptance`)
	if code != 0 {
		t.Fatalf(`verify-acceptance 应 exit 0（全绿）, got %d: %s`, code, verifyOut)
	}
	for _, want := range []string{`全部通过`, `deterministic`, `checklog: acceptance`} {
		if !strings.Contains(verifyOut, want) {
			t.Errorf(`verify-acceptance 输出缺 %q: %s`, want, verifyOut)
		}
	}

	// verify 后 status 应显示 ✅ 通过（不再 ⏳）
	statusOut2, _, _ := runForge(t, dir, `task`, `status`)
	if strings.Contains(statusOut2, `未验证`) {
		t.Errorf(`verify 后不应再有"未验证"项: %s`, statusOut2)
	}
	if !strings.Contains(statusOut2, `通过`) {
		t.Errorf(`verify 后 status 应显示"通过": %s`, statusOut2)
	}
}
