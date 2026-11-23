package clitask

// task_complete_test.go — complete 顺序契约的钉子测试（dogfood 2026-08-18 死锁修复）。
//
// 死锁实录（feat/trigger-audit-v2 任务）：最后一道 gate（task-complete）通过即
// MarkComplete → ActiveTaskState 对 CompletedAt!=nil 返 nil → 紧随其后的 `forge task
// complete` acceptance pre-flight 要求验收快照新鲜（当时为 AcceptedHeadCommit==HEAD，
// 2026-08-25 起为源码内容指纹——见 acceptance.go CheckAcceptanceFresh），而刷新只能由
// verify-acceptance（只认 active task）完成 → review 修复 commit 改动源码后 complete
// 永久 BLOCKED，且 start/claim/attach/resume 无一能复活。修复：完成标记归还
// `forge task complete`（pre-flight 之后、采集事实之前），gate 不再 MarkComplete。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/review"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// setupDeadlockTask 建一个 git 仓库 + session-scoped active task（三道门禁已过、带验收
// 标准）——模拟「门禁全过、等待 finalize」的窗口态。
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
	// 带 go.mod + *_test.go：让 fixture 具备「有测试能力」这个几何形状——
	// 2026-11-19 删掉的 CheckAcceptanceQuality 只在有测试能力时才拦「考卷没有测试类
	// 命令」，夹具若没有测试文件，原门禁复活也不会触发，回归钉子就是空的。
	if err := os.WriteFile(filepath.Join(dir, `go.mod`), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, `main_test.go`), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(`git`, `add`, `-A`)
	run(`git`, `commit`, `-m`, `init`)

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
		// 夹具直接构造状态：本测试验证快照刷新与死锁契约，不验证红先绿。
		RedFirstWaived: "夹具直接构造 TaskState（不经 task start）",
	}
	// 三道门禁全部通过（RecordGateResult）——旧 bug 正是最后这道完成标记把任务打死。
	for _, g := range taskpipeline.DefaultGates() {
		state.RecordGateResult(g.ID, true, head)
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	return dir
}

// markReviewedDirect 直接写入 review 快照（等价于一次 `forge review pass`）。
// 这些测试的临时仓库没有登记成 forge 项目，跑不了 review 命令；但 complete 自己会
// 复核 review 快照（review-ruthless-3 P1-1），所以夹具必须提供一份与当前 HEAD 一致的
// 审查记录。
func markReviewedDirect(t *testing.T, dir, taskRef string) {
	t.Helper()
	state, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	out := exec.Command(`git`, `rev-parse`, `HEAD`)
	out.Dir = dir
	headBytes, err := out.Output()
	if err != nil {
		t.Fatalf(`git rev-parse HEAD: %v`, err)
	}
	head := strings.TrimSpace(string(headBytes))
	hash, _, err := review.SourceChangesSince(dir, head)
	if err != nil {
		t.Fatalf(`SourceChangesSince: %v`, err)
	}
	state.MarkReviewPassed(head, hash)
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
}

// TestTaskComplete_PreflightFailureKeepsTaskActive pins the fix's core contract: a failing pre-flight must leave the task ACTIVE (CompletedAt==nil) so verify-acceptance can refresh the stale snapshots and complete can be retried; on success MarkComplete lands after the pre-flight with scoring in tow; double complete is idempotent.
//
// TestTaskComplete_PreflightFailureKeepsTaskActive 钉死死锁修复的核心契约：pre-flight
// 失败必须保持任务 active（CompletedAt==nil），verify-acceptance 才能刷新过期快照，
// complete 才能重试通过；成功路径上 MarkComplete 落在 pre-flight 之后、事实采集随行；
// 重复 complete 幂等。
func TestTaskComplete_PreflightFailureKeepsTaskActive(t *testing.T) {
	dir := setupDeadlockTask(t, []string{`go version :: go version`})
	const taskRef = `feat/deadlock`

	// 1. 验收实跑 @HEAD1（快照新鲜）。
	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if runErr != nil {
		t.Fatalf(`初次验收应通过: %v`, runErr)
	}

	// 2. 源码变更 + commit（模拟 review 修复改动代码）→ 内容快照过期。
	//    注意：2026-08-25 起快照绑源码内容指纹而非 HEAD——空 commit 不再使快照
	//    过期，故此处必须是真实源码改动才能触发 pre-flight 拒绝。
	if err := os.WriteFile(filepath.Join(dir, `fix.go`), []byte("package main\n\nfunc fix() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command(`git`, `add`, `.`)
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf(`git add: %v`+"\n"+`%s`, err, out)
	}
	commit := exec.Command(`git`, `commit`, `-m`, `review-fix`)
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

	// 5. 复审（review-ruthless-3 P1-1：complete 自己复核 review 快照）后重试：
	//    MarkComplete + 采集完成事实。
	markReviewedDirect(t, dir, taskRef)
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
	if done.Evidence == nil {
		t.Fatal(`成功路径必须落事实清单（Evidence 非 nil）`)
	}

	// 6. 幂等：重复 complete 不再重跑副作用、不报错。
	var again error
	_ = captureStdout(t, func() { again = runTaskCompleteAt(dir, done) })
	if again != nil {
		t.Fatalf(`重复 complete 应幂等跳过, got %v`, again)
	}
}

// TestTaskComplete_ClearsAllTaskAnchors pins E.3 (docs/design/harness-fixes-a-g-2026-09.md): completing a task clears every pointer that still names it — other sessions' active-task-ref files, the legacy global file, and the workspace binding — not just the completing session's own pointer.
//
// TestTaskComplete_ClearsAllTaskAnchors 钉死 E.3：complete 清掉所有仍指向该任务的锚点
// ——其他会话的 active-task-ref、legacy 全局文件、workspace 绑定——而非只清自己会话的
// 指针。乙机实录：多会话任务（session_links≥2）漏清后，异会话/无 session 的 hook 行
// 继续归到已封印任务名下两天。
func TestTaskComplete_ClearsAllTaskAnchors(t *testing.T) {
	dir := setupDeadlockTask(t, []string{`go version :: go version`})
	const taskRef = `feat/deadlock`
	for _, sid := range []string{`other-session-a`, `other-session-b`, ``} {
		if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
			t.Fatal(err)
		}
	}
	if err := taskpipeline.SetActiveTaskRef(dir, `unrelated-session`, `feat/unrelated`); err != nil {
		t.Fatal(err)
	}
	if err := worktree.BindTask(dir, taskRef, `feat/deadlock`, `other-session-a`); err != nil {
		t.Fatal(err)
	}

	state, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	// oracle-pipeline L1：登记门要求验收实跑快照（registration + freshness 两道
	// pre-flight 都消费真实执行）——anchors 测试的 state 由此满足新契约。
	var verErr error
	_ = captureStdout(t, func() { verErr = runTaskVerifyAcceptanceAt(dir, "", false) })
	if verErr != nil {
		t.Fatalf(`verify-acceptance 应通过: %v`, verErr)
	}
	state, err = taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	markReviewedDirect(t, dir, taskRef)
	state, err = taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskCompleteAt(dir, state) })
	if runErr != nil {
		t.Fatalf(`complete should pass: %v`, runErr)
	}
	for _, sid := range []string{`test-session-deadlock`, `other-session-a`, `other-session-b`, ``} {
		if got := taskpipeline.ReadActiveTaskRef(dir, sid); got != "" {
			t.Errorf(`pointer for session %q still names %q after complete`, sid, got)
		}
	}
	if got := taskpipeline.ReadActiveTaskRef(dir, `unrelated-session`); got != `feat/unrelated` {
		t.Errorf(`unrelated task's pointer must survive, got %q`, got)
	}
	if b := worktree.Load(dir); b != nil && b.TaskRef == taskRef {
		t.Errorf(`workspace binding must be cleared on complete, got %+v`, b)
	}
}

// TestTaskComplete_IdempotentGuardOnGeneric a completed generic task re-completes idempotently (generic never scores — the guard must key on IsGeneric, not Score).
//
// TestTaskComplete_IdempotentGuardOnGeneric 已完成的 generic 任务重复 complete 幂等
// （generic 不采集事实，守卫须走 IsGeneric 分支而非 Evidence）。
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

// 2026-11-19 入口诚实：complete 不再按「命令像不像测试」拦任务。base 上曾有一条
// 「考卷必须含测试类命令」的启发式（CheckAcceptanceQuality），它把 `test -f x` /
// `node --test` 这类合法判据当不合格；删除后这里钉住「非测试类判据也能 complete」，
// 避免门禁再长回来（评审 P3-e：删门禁本身没有回归钉子）。
func TestTaskComplete_AcceptsNonTestCommands(t *testing.T) {
	dir := setupDeadlockTask(t, []string{`test -f marker.txt ::`})
	if cap := taskpipeline.CheckTestCapability(dir); !cap.HasTests {
		t.Fatalf(`夹具必须是「有测试能力」的仓库，否则这条钉子钉不住原启发式：%+v`, cap)
	}
	if err := os.WriteFile(filepath.Join(dir, `marker.txt`), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(`git`, `add`, `-A`)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command(`git`, `commit`, `-m`, `marker`)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	head := exec.Command(`git`, `rev-parse`, `HEAD`)
	head.Dir = dir
	out, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	dir2 := dir
	state, err := taskpipeline.LoadTaskState(dir2, `feat/deadlock`)
	if err != nil {
		t.Fatal(err)
	}
	state.HeadCommit = strings.TrimSpace(string(out))
	if err := taskpipeline.SaveTaskState(dir2, state); err != nil {
		t.Fatal(err)
	}

	var verErr error
	_ = captureStdout(t, func() { verErr = runTaskVerifyAcceptanceAt(dir2, "", false) })
	if verErr != nil {
		t.Fatalf(`verify-acceptance 应通过（判据只有 test -f）: %v`, verErr)
	}
	markReviewedDirect(t, dir2, `feat/deadlock`)
	state, err = taskpipeline.LoadTaskState(dir2, `feat/deadlock`)
	if err != nil {
		t.Fatal(err)
	}
	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskCompleteAt(dir2, state) })
	if runErr != nil {
		t.Fatalf(`非测试类判据不应被 complete 拒绝: %v`, runErr)
	}
}

// TestTaskGate_PassesWithoutCompleting E2E: gates no longer MarkComplete — after ALL THREE gates pass (review pass → task-complete), the task must still be active (CompletedAt==nil) and visible to ActiveTaskState.
//
// TestTaskGate_PassesWithoutCompleting E2E 钉住 gate 不再 MarkComplete：**三道门禁全部
// 通过**（review pass → task-complete）后，任务必须仍处 active（CompletedAt==nil）且对
// ActiveTaskState 可见。死锁修复前最后一道 gate 会在 IsComplete 时标记完成——只测首道
// gate 钉不住该条件式行为（review m5），必须走满三门。
func TestTaskGate_PassesWithoutCompleting(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `e2e-gate-active`)
	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, stdout)
	}
	// git 仓库：task start 先记录 HeadCommit 基准，之后的真实代码提交构成增量变更
	//（hasCodeChanges 的判定依据），task-complete 的 review-snapshot 一致性需要 git HEAD。
	runGit(t, dir, `init`)
	runGit(t, dir, `config`, `user.email`, `t@example.com`)
	runGit(t, dir, `config`, `user.name`, `t`)
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `base`)
	gateOut, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/gate-active`,
		`--accept`, `go version :: go version`)
	if code != 0 {
		t.Fatalf(`task start failed: %s`, gateOut)
	}
	if err := os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `impl`)
	for _, g := range []string{`task-implement`} {
		out, _, code := runForge(t, dir, `task`, `gate`, g)
		if code != 0 {
			t.Fatalf(`task gate %s 应通过: %s`, g, out)
		}
	}
	// task-complete 的验收前置（review-ruthless-3 P2-1）：先实跑考卷。
	if out, _, code := runForge(t, dir, `task`, `verify-acceptance`); code != 0 {
		t.Fatalf(`task verify-acceptance failed: %s`, out)
	}
	// task-complete 的 ReviewPassed 硬前置：先 review pass。
	if out, _, code := runForge(t, dir, `review`, `pass`, `--criteria-reviewed`, `0`); code != 0 {
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
