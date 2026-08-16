package taskpipeline

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLockTask_MutualExclusion guards the core contract of the per-task lock: while one
// holder holds it, a second acquirer cannot take it (and errors after taskLockWait would
// be too slow for a unit test — so we assert non-acquisition indirectly: the second
// acquire must not succeed while the first holds). After unlock, acquisition succeeds
// again (no leftover lock file).
//
// TestLockTask_MutualExclusion 钉住 per-task 锁的核心契约：持锁期间第二个获取者拿不到
// （直接等到 taskLockWait 超时对单测太慢——故间接断言不获取：第一持锁期间第二次获取
// 必不成功）。解锁后可再次获取（无残留锁文件）。
func TestLockTask_MutualExclusion(t *testing.T) {
	dir := t.TempDir()

	unlock, err := LockTask(dir, "feat/lock")
	if err != nil {
		t.Fatalf("first LockTask: %v", err)
	}

	// A second acquire must not succeed while the first holds. We cannot wait the full
	// taskLockWait in a unit test, so probe in a goroutine and assert it does NOT
	// complete quickly.
	//
	// 第一持锁期间第二次获取必不成功。单测不能真等满 taskLockWait，故用 goroutine
	// 探测并断言它不会很快完成。
	acquired := make(chan struct{})
	go func() {
		u, err := LockTask(dir, "feat/lock")
		if err == nil {
			u()
			close(acquired)
		}
	}()
	select {
	case <-acquired:
		t.Fatal("持锁期间第二个 LockTask 不应成功")
	case <-time.After(300 * time.Millisecond):
		// expected: still waiting
	}

	unlock()
	select {
	case <-acquired:
		// acquired after unlock — correct
	case <-time.After(taskLockWait + 2*time.Second):
		t.Fatal("解锁后第二个 LockTask 应能获取")
	}
}

// TestLockTask_StaleBreak: a lock file older than taskLockStaleAfter is treated as a
// crash orphan and broken, so a crashed holder never blocks a task forever.
//
// TestLockTask_StaleBreak：超过 taskLockStaleAfter 的锁文件视为崩溃 orphan 并打破——
// 崩溃的持锁者不会永久阻塞该 task。
func TestLockTask_StaleBreak(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dataHome(dir)+"/tasks", 0755); err != nil {
		t.Fatal(err)
	}
	stale := taskLockPath(dir, "feat/stale")
	if err := os.WriteFile(stale, []byte("0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-taskLockStaleAfter - time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockTask(dir, "feat/stale")
	if err != nil {
		t.Fatalf("stale 锁应被打破并获取成功: %v", err)
	}
	unlock()
}

// TestMutateTaskState_SequentialLostUpdate: the whole point of MutateTaskState — two
// mutators that both read-modify-write the same field no longer lose each other's
// update. Simulates the pre-lock bug shape (load stale snapshot, append, save) by
// running N mutators whose fn appends one NextStep; without the lock+reload the final
// count would be < the number of SUCCESSFUL mutations.
//
// Contract split (2026-08-16 CI flake): the invariant this test exists for is
// lost-update-freedom — count == succeeded — not "all N finish within taskLockWait".
// On a thrashing -race CI runner a waiter can legitimately hit the 10s lock-wait
// timeout; that is MutateTaskState's DESIGNED fail-loud behavior (see lock.go), not a
// lost update. The old shape asserted count == N while swallowing errors with `_ =`,
// so a timeout surfaced as a misleading "lost update: got 19, want 20" (CI run
// 31953505720, unreproducible in 25 local -race runs). Guards: zero successes fails
// outright; and only lock-wait-timeout failures are tolerated — any other error shape
// (flaky save, dir failure) fails the test rather than passing in a timeout costume.
//
// TestMutateTaskState_SequentialLostUpdate：MutateTaskState 的存在意义——两个都走
// read-modify-write 的变更者不再互丢更新。模拟加锁前的 bug 形态（读过期快照、追加、
// 保存）：跑 N 个各追加一条 NextStep 的变更者；无锁+重载时最终计数会 < 成功的
// 变更数。
//
// 契约拆分（2026-08-16 CI 偶发）：本测试的存在意义是不丢更新——count == succeeded
// ——而非「N 个全都在 taskLockWait 内完成」。高负载 -race CI runner 上等待者可能合法
// 触发 10s 锁等待超时；那是 MutateTaskState 设计好的 fail-loud 行为（见 lock.go），
// 不是丢更新。旧形状断言 count == N 且用 `_ =` 吞错，超时就伪装成 "丢更新: got 19,
// want 20"（CI run 31953505720，本地 25 次 -race 复现不出）。守卫：零成功直接失败；
// 且只容忍锁等待超时这一种失败形态——其他错误形态（保存间歇失败、目录故障）直接
// 失败，不许穿着超时外衣假绿。
func TestMutateTaskState_SequentialLostUpdate(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "feat/mut", Branch: "feat/mut"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	const n = 20
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = MutateTaskState(dir, "feat/mut", func(s *TaskState) error {
				s.AddNext("step")
				return nil
			})
		}(i)
	}
	wg.Wait()

	succeeded := 0
	var firstErr error // 首个非 nil 错误做代表（errs[0] 可能恰好是成功者，打出来是 <nil>）
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if firstErr == nil {
			firstErr = err
		}
	}
	if succeeded == 0 {
		t.Fatalf("全部 %d 个变更者都失败（不是丢更新，是系统性错误）: %v", n, firstErr)
	}
	// 容忍的失败形态只有锁等待超时（lock.go 的 fail-loud 文案）——其他任何错误（保存失败、
	// 目录故障等间歇问题）穿着超时外衣绿过就是假绿（评审 3a）。
	//
	// The only tolerated failure shape is the lock-wait timeout (lock.go's fail-loud
	// message) — any other error (flaky save, dir failure, …) passing green in a
	// timeout costume is a false green (review 3a).
	for _, err := range errs {
		if err != nil && !strings.Contains(err.Error(), "lock held by another forge process") {
			t.Fatalf("非超时形态的失败不容忍（不是丢更新契约的一部分）: %v", err)
		}
	}

	reloaded, err := LoadTaskState(dir, "feat/mut")
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.NextSteps) != succeeded {
		t.Errorf("并发 MutateTaskState 丢更新: got %d NextSteps, 成功变更者 %d（失败 %d 个: %v）",
			len(reloaded.NextSteps), succeeded, n-succeeded, firstErr)
	}
	if succeeded < n {
		// 锁等待超时是设计内 fail-loud（非丢更新），记录供诊断不计失败——慢 CI runner
		// 上 20 路串行竞争可能超 taskLockWait。
		//
		// Lock-wait timeouts are designed fail-loud (not lost updates) — logged for
		// diagnosis, not failed: 20-way serialized contention can exceed taskLockWait
		// on a slow CI runner.
		t.Logf("note: %d/%d mutators hit the designed lock-wait timeout (fail-loud, not lost update); first: %v", n-succeeded, n, firstErr)
	}
}

// TestMutateTaskState_FnErrorNoSave: when fn errors, nothing is saved (no partial
// mutation persisted) and the error propagates.
//
// TestMutateTaskState_FnErrorNoSave：fn 报错时不保存（不落盘半截变更）且错误向上传。
func TestMutateTaskState_FnErrorNoSave(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "feat/err", Branch: "feat/err"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	err := MutateTaskState(dir, "feat/err", func(s *TaskState) error {
		s.AddNext("should-not-persist")
		return os.ErrPermission
	})
	if err == nil {
		t.Fatal("fn 报错应向上传")
	}
	reloaded, _ := LoadTaskState(dir, "feat/err")
	if len(reloaded.NextSteps) != 0 {
		t.Errorf("fn 报错不应保存变更: %v", reloaded.NextSteps)
	}
}

// TestResumeStale_PerSession: the per-session sentinel — Mark then Consume for the same
// sid returns true exactly once; a different sid is unaffected (the multi-session fix:
// B's prompt no longer consumes A's compaction mark).
//
// TestResumeStale_PerSession：per-session sentinel——同 sid Mark 后 Consume 恰为 true
// 一次；不同 sid 互不影响（多 session 修复：B 的 prompt 不再消费 A 的压缩标记）。
func TestResumeStale_PerSession(t *testing.T) {
	dir := t.TempDir()
	if err := MarkResumeStale(dir, "sid-a"); err != nil {
		t.Fatal(err)
	}
	if ConsumeResumeStale(dir, "sid-b") {
		t.Error("sid-b 不应消费 sid-a 的标记")
	}
	if !ConsumeResumeStale(dir, "sid-a") {
		t.Error("sid-a 应消费到自己的标记")
	}
	if ConsumeResumeStale(dir, "sid-a") {
		t.Error("标记应只被消费一次")
	}
	// Empty sid never consumes (legacy sessions use the task-scoped bool).
	//
	// 空 sid 永不消费（legacy session 走 task-scoped bool）
	if err := MarkResumeStale(dir, "sid-c"); err != nil {
		t.Fatal(err)
	}
	if ConsumeResumeStale(dir, "") {
		t.Error("空 sid 不应消费任何标记")
	}
}

// TestColdStartInjected_PerSessionNotConsumed: the per-session cold-start sentinel differs
// from resume-stale in ONE way — it is NOT consumed on read (a session needs its cold-start
// handoff exactly once, so the sentinel persists for the session lifetime). Mark is idempotent
// (AtomicWrite overwrites). Cross-session isolation mirrors resume-stale (shared user-level
// DataDir). Empty sid never marks nor reports injected (the backfill is gated on non-empty sid
// upstream, but the guard prevents an empty-id sentinel that would collide across sessions).
//
// TestColdStartInjected_PerSessionNotConsumed：per-session cold-start sentinel 与 resume-stale
// 唯一不同——读时不被消费（一个 session 恰需一次冷启动 handoff，故 sentinel 存活整个 session
// 生命周期）。Mark 幂等（AtomicWrite 覆写）。跨 session 隔离镜像 resume-stale（用户级共享 DataDir）。
// 空 sid 既不标记也不报已注入（回填上游已门控在非空 sid，此守卫防空 id sentinel 跨 session 碰撞）。
func TestColdStartInjected_PerSessionNotConsumed(t *testing.T) {
	dir := t.TempDir()
	if err := MarkColdStartInjected(dir, "sid-a"); err != nil {
		t.Fatal(err)
	}
	if !IsColdStartInjected(dir, "sid-a") {
		t.Error("sid-a Mark 后应报已注入")
	}
	// NOT consumed: IsColdStartInjected is read-only, a second read still true (the key
	// difference from ConsumeResumeStale which clears on read).
	//
	// 不被消费：IsColdStartInjected 只读，第二次读仍 true（与 ConsumeResumeStale 读即清的关键区别）
	if !IsColdStartInjected(dir, "sid-a") {
		t.Error("cold-start sentinel 不应被读消费，第二次应仍报已注入")
	}
	// Cross-session isolation: sid-b unaffected by sid-a's sentinel.
	//
	// 跨 session 隔离：sid-b 不受 sid-a 的 sentinel 影响
	if IsColdStartInjected(dir, "sid-b") {
		t.Error("sid-b 不应被 sid-a 的 sentinel 影响")
	}
	// Idempotent: a second Mark overwrites the same path, sentinel still present.
	//
	// 幂等：第二次 Mark 覆写同路径，sentinel 仍在
	if err := MarkColdStartInjected(dir, "sid-a"); err != nil {
		t.Fatal(err)
	}
	if !IsColdStartInjected(dir, "sid-a") {
		t.Error("重复 Mark 后应仍报已注入")
	}
	// Empty sid never marks nor reports injected.
	//
	// 空 sid 既不标记也不报已注入
	if err := MarkColdStartInjected(dir, ""); err != nil {
		t.Fatalf("空 sid Mark 应 no-op 不报错: %v", err)
	}
	if IsColdStartInjected(dir, "") {
		t.Error("空 sid 不应报已注入")
	}
}

// TestCurrentSessionID_FallbackChain: multi-host session detection — Claude env wins;
// FORGE_SESSION_ID (injected by runHook from any host's stdin session_id) is the
// fallback; "default" (the scripts' empty placeholder) and unset both yield "".
//
// TestCurrentSessionID_FallbackChain：多 host session 探测——Claude env 优先；
// FORGE_SESSION_ID（runHook 从任意 host stdin 的 session_id 注入）兜底；"default"
// （脚本侧空占位符）与未设都返回 ""。
func TestCurrentSessionID_FallbackChain(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_SESSION_ID", "")
	if got := CurrentSessionID(); got != "" {
		t.Errorf("全空应返回 \"\": %q", got)
	}

	t.Setenv("FORGE_SESSION_ID", "kimi-sess-1")
	if got := CurrentSessionID(); got != "kimi-sess-1" {
		t.Errorf("FORGE_SESSION_ID 兜底: %q", got)
	}

	t.Setenv("FORGE_SESSION_ID", "default")
	if got := CurrentSessionID(); got != "" {
		t.Errorf("\"default\" 占位符应按空处理: %q", got)
	}

	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-sess")
	t.Setenv("FORGE_SESSION_ID", "kimi-sess-1")
	if got := CurrentSessionID(); got != "cc-sess" {
		t.Errorf("CLAUDE_CODE_SESSION_ID 应优先: %q", got)
	}
}
