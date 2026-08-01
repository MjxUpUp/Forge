package taskpipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// Task-state mutation locking.
//
// After refactor-data-home all worktrees of a repo share one user-level DataDir
// (~/.forge/projects/<key>/), so two sessions in two worktrees can mutate the same
// tasks/<ref>.json concurrently (resume auto-attach + task decide racing). SaveTaskState
// is atomic per write but the load→mutate→save sequence is not — last writer wins and
// silently drops the other's session links/decisions/next-steps, i.e. exactly the
// continuity data this design exists to preserve. LockTask + MutateTaskState make the
// whole sequence mutually exclusive.
//
// task state 变更锁。
//
// refactor-data-home 之后同 repo 的所有 worktree 共享一个用户级 DataDir
// （~/.forge/projects/<key>/），两个 session 在两个 worktree 里可能并发改同一份
// tasks/<ref>.json（resume 自动锚定与 task decide 对撞）。SaveTaskState 单次写是
// 原子的，但 load→mutate→save 整段不是——后写覆盖先写，静默丢掉对方的 session
// 链接/决策/下一步，恰恰丢的是本设计要保的接续数据。LockTask + MutateTaskState
// 让整段互斥。

const (
	// taskLockStaleAfter bounds how long an uncontended lock file may exist before it is
	// considered abandoned (holder crashed mid-mutation) and broken. Mutations are
	// sub-second, so 30s is far beyond any legitimate hold.
	//
	// taskLockStaleAfter 限定无竞争锁文件存在多久后视为被遗弃（持锁者变更中途崩溃）
	// 并被打破。变更都是亚秒级，30s 远超任何合法持锁。
	taskLockStaleAfter = 30 * time.Second
	// taskLockWait bounds how long a waiter retries before giving up. Failing loudly is
	// correct for an explicit write command; hooks degrade via their own advisory path.
	//
	// taskLockWait 限定等待者重试多久后放弃。显式写命令大声失败是正确的；hook 走自己的
	// advisory 降级路径。
	taskLockWait  = 10 * time.Second
	taskLockRetry = 50 * time.Millisecond
)

// taskLockPath returns the lock file path for a task ref (sibling of the state file).
//
// taskLockPath 返回 task ref 的锁文件路径（与 state 文件同目录）。
func taskLockPath(root, ref string) string {
	return filepath.Join(dataHome(root), "tasks", taskcontext.SanitizeRef(ref)+".lock")
}

// LockTask acquires the per-task advisory lock and returns the unlock func. The lock is
// an O_CREATE|O_EXCL file; a lock older than taskLockStaleAfter is treated as abandoned
// (crash orphan) and broken. Waits up to taskLockWait, then errors.
//
// LockTask 获取 per-task 建议锁并返回解锁函数。锁是 O_CREATE|O_EXCL 文件；超过
// taskLockStaleAfter 的锁视为被遗弃（崩溃 orphan）并打破。最多等待 taskLockWait，
// 超时报错。
func LockTask(root, ref string) (unlock func(), err error) {
	path := taskLockPath(root, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create tasks directory: %w", err)
	}
	deadline := time.Now().Add(taskLockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", time.Now().Unix())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to acquire task lock: %w", err)
		}
		// Break the stale lock: a holder that crashed mid-mutation would otherwise block
		// every future writer of this task forever.
		//
		// 打破 stale 锁：持锁者变更中途崩溃，否则该 task 未来的所有写者被永久阻塞。
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > taskLockStaleAfter {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("task %q lock held by another forge process for >%s; retry later", ref, taskLockWait)
		}
		time.Sleep(taskLockRetry)
	}
}

// MutateTaskState performs an atomic load→mutate→save on a task: it takes the per-task
// lock, reloads the state inside the lock (so fn sees the latest on-disk content, not a
// stale pre-lock snapshot), applies fn, and saves. fn must only mutate the passed state.
//
// MutateTaskState 对 task 做原子的 load→mutate→save：先取 per-task 锁，在锁内重载
// state（fn 看到的是盘上最新内容，不是取锁前的过期快照），应用 fn，再保存。fn 只应
// 改动传入的 state。
func MutateTaskState(root, ref string, fn func(*TaskState) error) error {
	unlock, err := LockTask(root, ref)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := LoadTaskState(root, ref)
	if err != nil {
		return err
	}
	if err := fn(state); err != nil {
		return err
	}
	return SaveTaskState(root, state)
}
