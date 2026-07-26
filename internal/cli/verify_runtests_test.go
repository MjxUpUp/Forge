package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestRunProjectTestsModeAt_RecordsDeterministic is the end-to-end guard for #1: on a
// real, runnable minimal go module it exercises the core of `forge verify --run-tests`,
// asserting checklog records a CheckNameTestRun entry with Source=deterministic (run by
// forge itself, unforgeable) and Passed reflects the real exit code. This is the hookup
// that promotes an agent self-claim of passing into deterministic evidence — if
// DetectTestCommand / RunTestCommand / recording wiring breaks anywhere, this test pins
// the regression.
//
// TestRunProjectTestsModeAt_RecordsDeterministic 是 #1 的端到端守卫：在一个真实可跑的
// 最小 go 模块上跑 forge verify --run-tests 的核心，断言 checklog 记录了 CheckNameTestRun
// 条目且 Source=deterministic（forge 自己跑的，不可伪造），Passed 反映真实退出码。
// 这是把「agent 自述测过」升级为「deterministic 证据」的接入点——若 DetectTestCommand /
// RunTestCommand / 记录接线任一断裂，本测试钉住回归。
func TestRunProjectTestsModeAt_RecordsDeterministic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module testproj\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A passing test (stdlib only, compiles offline).
	//
	// 一个通过的测试（仅 stdlib，离线可编译）
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"),
		[]byte("package testproj\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runProjectTestsModeAt(dir); err != nil {
		t.Fatalf("runProjectTestsModeAt on a green module should not error: %v", err)
	}

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var rec *checklog.Entry
	for i := range entries {
		if entries[i].Check == taskpipeline.CheckNameTestRun {
			rec = &entries[i]
			break
		}
	}
	if rec == nil {
		t.Fatal(`CheckNameTestRun entry not recorded — verify --run-tests 未把测试结果写入 checklog`)
	}
	if !rec.Passed {
		t.Errorf(`test-run Passed = false on a green suite, want true（真实退出码未正确反映）`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`test-run Source = %s, want deterministic（forge 实跑应归 deterministic，非 agent-claim）`, rec.Source)
	}
}

// TestRunProjectTestsModeAt_NoCommandSilent verifies that when no recognizable manifest
// is present it exits silently and writes nothing to checklog (no empty command spawned,
// no noise entries left behind).
//
// TestRunProjectTestsModeAt_NoCommandSilent 验证无可识别 manifest 时静默退出、不写
// checklog（不发空命令、不留噪声条目）。
func TestRunProjectTestsModeAt_NoCommandSilent(t *testing.T) {
	dir := t.TempDir() // 无 go.mod / Cargo.toml / package.json / pytest
	if err := runProjectTestsModeAt(dir); err != nil {
		t.Fatalf("no-command path should not error: %v", err)
	}
	entries, _ := checklog.LoadAll(dir)
	if len(entries) != 0 {
		t.Errorf(`no test command → 不应写 checklog 条目，got %d`, len(entries))
	}
}

// TestRunProjectTestsModeAt_AttributesToSessionScopedTask pins task attribution: when
// CLAUDE_CODE_SESSION_ID is set, test-run evidence must be recorded under the task
// pointed at by the session-scoped active-task-ref, not the stale shared
// .forge/active-task-ref. Regression scenario: runProjectTestsModeAt previously called
// ReadActiveTaskRef with an empty sessionID and read the old task from the shared file
// (e.g. fix/concurrent-session-race residue), attributing evidence to the wrong task
// where it is invisible to trace — this test plants a stale shared file to prove it is
// ignored.
//
// TestRunProjectTestsModeAt_AttributesToSessionScopedTask 钉住任务归属：当
// CLAUDE_CODE_SESSION_ID 已设置时，test-run 证据必须记到 session-scoped
// active-task-ref 指向的任务下，而非陈旧的共享 .forge/active-task-ref。
// 回归场景：此前 runProjectTestsModeAt 用空 sessionID 调 ReadActiveTaskRef，读到
// 共享文件里的旧任务（如 fix/concurrent-session-race 残留），证据记错任务、对该任务
// trace 不可见——本测试埋一个陈旧共享文件证明它被忽略。
func TestRunProjectTestsModeAt_AttributesToSessionScopedTask(t *testing.T) {
	dir := t.TempDir()

	goMod := `module testproj
go 1.21
`
	if err := os.WriteFile(filepath.Join(dir, `go.mod`), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}
	fooTest := `package testproj

import "testing"

func TestFoo(t *testing.T) {}
`
	if err := os.WriteFile(filepath.Join(dir, `foo_test.go`), []byte(fooTest), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate an agent running `forge verify --run-tests` inside a Claude Code session.
	//
	// 模拟 agent 在 Claude Code 会话内跑 forge verify --run-tests
	const sid = `test-session-abc`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)

	// The real active task (session-scoped file).
	//
	// 真实活动任务（session-scoped 文件）
	const realTask = `feat/session-scoped-task`
	if err := taskpipeline.SetActiveTaskRef(dir, sid, realTask); err != nil {
		t.Fatal(err)
	}

	// Plant a stale shared active-task-ref (legacy session residue) — must be ignored.
	//
	// 埋一个陈旧的共享 active-task-ref（旧会话残留）——必须被忽略
	if err := os.MkdirAll(filepath.Join(dir, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, `.forge`, `active-task-ref`),
		[]byte(`stale/legacy-task`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runProjectTestsModeAt(dir); err != nil {
		t.Fatalf(`green module should not error: %v`, err)
	}

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	var rec *checklog.Entry
	for i := range entries {
		if entries[i].Check == taskpipeline.CheckNameTestRun {
			rec = &entries[i]
			break
		}
	}
	if rec == nil {
		t.Fatal(`CheckNameTestRun entry not recorded`)
	}
	if rec.TaskRef != realTask {
		t.Errorf(`test-run task_ref = %q, want %q（应归属 session-scoped 活动任务，非陈旧共享文件）`, rec.TaskRef, realTask)
	}
}

// TestRunProjectTestsModeAt_RecordsFailure pins the RED path: a failing test suite must
// still record a test-run entry (Passed=false, Checked=true, source=deterministic) and
// return a non-nil error. The core value of #1 is recording failure too as unforgeable
// evidence — without this test, if someone later moves checklog.Record into an `if passed`
// branch, failure evidence would be silently dropped while green-only tests still pass,
// hitting exactly the agent-self-claim blind spot this feature aims to close.
//
// TestRunProjectTestsModeAt_RecordsFailure 钉住 RED 路径：失败的测试套件必须照常
// 记一条 test-run（Passed=false、Checked=true、source=deterministic）并返回非 nil error。
// #1 的核心价值就是把「失败」也记为不可伪造的证据——没有本测试，未来若有人把
// checklog.Record 挪进 `if passed` 分支，失败证据会被静默丢弃而 green-only 测试照过，
// 正中本特性要堵的「agent 自述测过」盲区。
func TestRunProjectTestsModeAt_RecordsFailure(t *testing.T) {
	dir := t.TempDir()

	goMod := `module testproj
go 1.21
`
	if err := os.WriteFile(filepath.Join(dir, `go.mod`), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}
	failTest := `package testproj

import "testing"

func TestFail(t *testing.T) { t.Fatal("intentional failure") }
`
	if err := os.WriteFile(filepath.Join(dir, `fail_test.go`), []byte(failTest), 0644); err != nil {
		t.Fatal(err)
	}

	err := runProjectTestsModeAt(dir)
	if err == nil {
		t.Fatal(`failing suite should return a non-nil error`)
	}

	entries, loadErr := checklog.LoadAll(dir)
	if loadErr != nil {
		t.Fatalf(`LoadAll: %v`, loadErr)
	}
	var rec *checklog.Entry
	for i := range entries {
		if entries[i].Check == taskpipeline.CheckNameTestRun {
			rec = &entries[i]
			break
		}
	}
	if rec == nil {
		t.Fatal(`CheckNameTestRun entry not recorded on failure — fail path dropped the record`)
	}
	if rec.Passed {
		t.Error(`test-run Passed = true on a red suite, want false（真实退出码未反映）`)
	}
	if !rec.Checked {
		t.Error(`test-run Checked = false, want true（失败也应标记为已检查）`)
	}
	if rec.Source != checklog.EvidenceDeterministic {
		t.Errorf(`test-run Source = %s, want deterministic（forge 实跑的失败也是 deterministic）`, rec.Source)
	}
}
