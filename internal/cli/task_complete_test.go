package cli

// task_complete_test.go — complete 顺序契约的钉子测试（dogfood 2026-08-18 死锁修复）。
//
// 死锁实录（feat/trigger-audit-v2 任务）：最后一道 gate（task-complete）通过即
// MarkComplete → ActiveTaskState 对 CompletedAt!=nil 返 nil → 紧随其后的 `forge task
// complete` acceptance pre-flight 要求 AcceptedHeadCommit==HEAD，而刷新只能由
// verify-acceptance（只认 active task）完成 → review 修复 commit 移动 HEAD 后 complete
// 永久 BLOCKED，且 start/claim/attach/resume 无一能复活。修复：完成标记归还
// `forge task complete`（pre-flight 之后、评分之前），gate 不再 MarkComplete。
//
// task_complete_test.go — pins for complete's ordering contract (dogfood 2026-08-18
// deadlock fix). The deadlock (feat/trigger-audit-v2 task): the LAST gate marked
// completion on pass → ActiveTaskState returns nil for CompletedAt!=nil → the following
// `forge task complete` acceptance pre-flight demands AcceptedHeadCommit==HEAD while the
// only refresher (verify-acceptance) accepts ACTIVE tasks only → after a review-fix
// commit moved HEAD, complete was BLOCKED forever with no revival path. Fix: completion
// moved back into `forge task complete` (after pre-flight, before scoring); gates no
// longer MarkComplete.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupDeadlockTask 建一个 git 仓库 + session-scoped active task（三道门禁已过、带验收
// 标准）——模拟「门禁全过、等待 finalize」的窗口态。
//
// setupDeadlockTask builds a git repo + session-scoped active task (all gates passed,
// with acceptance criteria) — the "gates done, awaiting finalize" window state.
func setupDeadlockTask(t *testing.T, acceptRaw []string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	const sid = `test-session-deadlock`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)
	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	run(`git`, `init`)
	run(`git`, `config`, `user.email`, `t@example.com`)
	run(`git`, `config`, `user.name`, `t`)
	run(`git`, `commit`, `--allow-empty`, `-m`, `init`)

	const taskRef = `feat/deadlock`
	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(run(`git`, `rev-parse`, `HEAD`))
	state := &taskpipeline.TaskState{
		TaskRef:    taskRef,
		SessionID:  sid,
		Branch:     `feat/deadlock`,
		StartedAt:  time.Now(),
		HeadCommit: head,
		Acceptance: taskpipeline.ParseAcceptance(acceptRaw),
	}
	// 三道门禁全部通过（RecordGateResult）——旧 bug 正是最后这道完成标记把任务打死。
	//
	// All three gates passed (RecordGateResult) — the old bug was exactly this last
	// gate's completion mark deactivating the task.
	for _, g := range taskpipeline.DefaultGates() {
		state.RecordGateResult(g.ID, true, head)
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestTaskComplete_PreflightFailureKeepsTaskActive 钉死死锁修复的核心契约：pre-flight
// 失败必须保持任务 active（CompletedAt==nil），verify-acceptance 才能刷新过期快照，
// complete 才能重试通过；成功路径上 MarkComplete 落在 pre-flight 之后、评分随行；
// 重复 complete 幂等。
//
// TestTaskComplete_PreflightFailureKeepsTaskActive pins the fix's core contract: a
// failing pre-flight must leave the task ACTIVE (CompletedAt==nil) so verify-acceptance
// can refresh the stale snapshots and complete can be retried; on success MarkComplete
// lands after the pre-flight with scoring in tow; double complete is idempotent.
func TestTaskComplete_PreflightFailureKeepsTaskActive(t *testing.T) {
	dir := setupDeadlockTask(t, []string{`go version :: go version`})
	const taskRef = `feat/deadlock`

	// 1. 验收实跑 @HEAD1（快照新鲜）。
	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr != nil {
		t.Fatalf(`初次验收应通过: %v`, runErr)
	}

	// 2. HEAD 移动（模拟 review 修复 commit）→ 快照过期。
	commit := exec.Command(`git`, `commit`, `--allow-empty`, `-m`, `review-fix`)
	commit.Dir = dir
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf(`commit: %v`+"\n"+`%s`, err, out)
	}

	// 3. complete 被 pre-flight 拒绝——但任务必须保持 active（死锁修复的断言核心：
	//    旧 bug 里 gate 已 MarkComplete，此处 CompletedAt 非 nil，步骤 4 永远走不通）。
	state, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() { runErr = runTaskCompleteAt(dir, state) })
	if runErr == nil || !strings.Contains(runErr.Error(), `acceptance pre-flight`) {
		t.Fatalf(`快照过期应被 pre-flight 拒绝, got %v`, runErr)
	}
	reloaded, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CompletedAt != nil {
		t.Fatal(`pre-flight 失败不得标记完成——任务必须保持 active，verify-acceptance 才能刷新（死锁修复核心契约）`)
	}

	// 4. 刷新路径活着：verify-acceptance（active-only）重跑 → 快照对齐 HEAD2。
	_ = captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr != nil {
		t.Fatalf(`死锁修复后 verify-acceptance 必须仍可刷新: %v`, runErr)
	}

	// 5. complete 重试通过：MarkComplete + 评分。
	fresh, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() { runErr = runTaskCompleteAt(dir, fresh) })
	if runErr != nil {
		t.Fatalf(`刷新后 complete 应通过: %v`, runErr)
	}
	done, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedAt == nil {
		t.Fatal(`成功路径必须 MarkComplete`)
	}
	if done.Score == nil {
		t.Fatal(`成功路径必须评分（Score 非 nil）`)
	}

	// 6. 幂等：重复 complete 不再重跑副作用、不报错。
	var again error
	_ = captureStdout(t, func() { again = runTaskCompleteAt(dir, done) })
	if again != nil {
		t.Fatalf(`重复 complete 应幂等跳过, got %v`, again)
	}
}

// TestTaskComplete_IdempotentGuardOnGeneric 已完成的 generic 任务重复 complete 幂等
// （generic 不评分，守卫须走 IsGeneric 分支而非 Score）。
//
// TestTaskComplete_IdempotentGuardOnGeneric a completed generic task re-completes
// idempotently (generic never scores — the guard must key on IsGeneric, not Score).
func TestTaskComplete_IdempotentGuardOnGeneric(t *testing.T) {
	dir := t.TempDir()
	const sid = `test-session-generic`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)
	const taskRef = `chore/generic-done`
	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	state := &taskpipeline.TaskState{
		TaskRef: taskRef, SessionID: sid, Branch: taskRef,
		StartedAt: now, Kind: `generic`, CompletedAt: &now,
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	var runErr error
	out := captureStdout(t, func() { runErr = runTaskCompleteAt(dir, state) })
	if runErr != nil {
		t.Fatalf(`已完成 generic 任务重复 complete 应幂等: %v`, runErr)
	}
	if !strings.Contains(out, `already completed`) && !strings.Contains(out, `已完成`) {
		t.Fatalf(`幂等跳过应有提示, got %s`, out)
	}
}

// TestTaskGate_PassesWithoutCompleting E2E 钉住 gate 不再 MarkComplete：**三道门禁全部
// 通过**（review pass → task-complete）后，任务必须仍处 active（CompletedAt==nil）且对
// ActiveTaskState 可见。死锁修复前最后一道 gate 会在 IsComplete 时标记完成——只测首道
// gate 钉不住该条件式行为（review m5），必须走满三门。
//
// TestTaskGate_PassesWithoutCompleting E2E: gates no longer MarkComplete — after ALL
// THREE gates pass (review pass → task-complete), the task must still be active
// (CompletedAt==nil) and visible to ActiveTaskState. Before the fix the LAST gate
// marked completion on IsComplete — testing only the first gate cannot pin that
// conditional behavior (review m5); all three gates must run.
func TestTaskGate_PassesWithoutCompleting(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-gate-active`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}
	// git 仓库：task start 先记录 HeadCommit 基准，之后的真实代码提交构成增量变更
	//（hasCodeChanges 的判定依据），task-complete 的 review-snapshot 一致性需要 git HEAD。
	//
	// Git repo: task start first records the HeadCommit base; the real code commit
	// AFTER it forms the incremental change (what hasCodeChanges judges), and
	// task-complete's review-snapshot consistency needs a git HEAD.
	runGit(t, dir, `init`)
	runGit(t, dir, `config`, `user.email`, `t@example.com`)
	runGit(t, dir, `config`, `user.name`, `t`)
	runGit(t, dir, `commit`, `--allow-empty`, `-m`, `base`)
	gateOut, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/gate-active`)
	if code != 0 {
		t.Fatalf(`task start failed: %s`, gateOut)
	}
	if err := os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `impl`)
	for _, g := range []string{`task-implement`, `task-verify`} {
		out, _, code := runForge(t, dir, `task`, `gate`, g)
		if code != 0 {
			t.Fatalf(`task gate %s 应通过: %s`, g, out)
		}
	}
	// task-complete 的 ReviewPassed 硬前置：先 review pass。
	//
	// task-complete's ReviewPassed hard prerequisite: review pass first.
	if out, _, code := runForge(t, dir, `review`, `pass`); code != 0 {
		t.Fatalf(`forge review pass failed: %s`, out)
	}
	if out, _, code := runForge(t, dir, `task`, `gate`, `task-complete`); code != 0 {
		t.Fatalf(`task gate task-complete 应通过: %s`, out)
	}
	state, err := taskpipeline.LoadTaskState(dir, `feat/gate-active`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if !state.IsComplete() {
		t.Fatal(`前置自检：三道门禁应已全过`)
	}
	if state.CompletedAt != nil {
		t.Fatal(`最后一道 gate 通过不得 MarkComplete——完成属于 forge task complete（死锁修复契约，review m5 三门钉死）`)
	}
	if active, _ := taskpipeline.ActiveTaskState(dir, `e2e-gate-active`); active == nil {
		t.Fatal(`三门全过后任务必须仍对 ActiveTaskState 可见（active）——verify-acceptance 刷新窗口`)
	}
}
