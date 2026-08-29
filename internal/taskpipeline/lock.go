package taskpipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// task state 变更锁。
//
// refactor-data-home 之后同 repo 的所有 worktree 共享一个用户级 DataDir
// （~/.forge/projects/<key>/），两个 session 在两个 worktree 里可能并发改同一份
// tasks/<ref>.json（resume 自动锚定与 task decide 对撞）。SaveTaskState 单次写是
// 原子的，但 load→mutate→save 整段不是——后写覆盖先写，静默丢掉对方的 session
// 链接/决策/下一步，恰恰丢的是本设计要保的接续数据。LockTask + MutateTaskState
// 让整段互斥。

const (
	// taskLockStaleAfter 限定无竞争锁文件存在多久后视为被遗弃（持锁者变更中途崩溃）
	// 并被打破。变更都是亚秒级，30s 远超任何合法持锁。
	taskLockStaleAfter = 30 * time.Second
	// taskLockWait 限定等待者重试多久后放弃。显式写命令大声失败是正确的；hook 走自己的
	// advisory 降级路径。
	taskLockWait  = 10 * time.Second
	taskLockRetry = 50 * time.Millisecond
)

// taskLockPath 返回 task ref 的锁文件路径（与 state 文件同目录）。
func taskLockPath(root, ref string) string {
	return filepath.Join(dataHome(root), "tasks", taskcontext.SanitizeRef(ref)+".lock")
}

// LockTask acquires the per-task advisory lock and returns the unlock func.
//
// LockTask 获取 per-task 建议锁并返回解锁函数。锁是 O_CREATE|O_EXCL 文件，内容为
// 获取时刻时间戳，兼作持锁者身份令牌：unlock 只在内容仍匹配时删文件——被挂起超过
// stale 窗口（合盖睡眠）、锁已被打破者接管的持锁者，醒来 unlock 不会删掉打破者
// 新建的锁。超过 taskLockStaleAfter 的锁视为被遗弃（崩溃 orphan）并打破。最多等待
// taskLockWait，超时报错。
//
// 延迟预算说明：等待者可能在 SessionStart / UserPromptSubmit hook 内最多阻塞
// taskLockWait（resume 自动锚定、reinject legacy 清零）。合法持锁是亚秒级，只有
// 崩溃窗口内才会等满；hook 仍降级 exit 0——「不阻塞」契约管退出码，不管延迟。
func LockTask(root, ref string) (unlock func(), err error) {
	path := taskLockPath(root, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create tasks directory: %w", err)
	}
	deadline := time.Now().Add(taskLockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			stamp := fmt.Sprintf("%d\n", time.Now().Unix())
			// 身份戳写失败会留下【空】锁文件：带身份校验的解锁从此永不匹配，
			// 锁比持有者多活整个 stale 窗口，该任务的并发写者在这段时间内全部
			// 快速失败。改为清掉孤儿锁并上抛失败。
			if _, werr := f.WriteString(stamp); werr != nil {
				f.Close()
				os.Remove(path)
				return nil, fmt.Errorf("failed to write lock identity for %s: %w", ref, werr)
			}
			if cerr := f.Close(); cerr != nil {
				os.Remove(path)
				return nil, fmt.Errorf("failed to close lock for %s: %w", ref, cerr)
			}
			return func() {
				// 带身份校验的解锁：锁仍是自己的才删。
				if data, err := os.ReadFile(path); err == nil && string(data) == stamp {
					os.Remove(path)
				}
			}, nil
		}
		if !os.IsExist(err) {
			// Windows 瞬态竞争：打开刚被创建（持锁者尚在 OpenFile 与 Close 之间）或正被
			// AV 索引器扫描的锁文件，返回的是 access-denied / sharing-violation 而非
			// EEXIST。这与超时路径是同一种竞争形态、不是损坏——在同一 deadline 内重试
			// 而非硬失败（真实 flake：windows-latest 的
			// TestMutateTaskState_SequentialLostUpdate，2026-08-19）。
			if !isLockContentionErr(err) {
				return nil, fmt.Errorf("failed to acquire task lock: %w", err)
			}
			if time.Now().After(deadline) {
				// 消息里带上底层错误：持续权限拒绝（只读 DataDir）与真竞争穿同一件
				// 超时外衣——区分两者靠 last error。
				return nil, fmt.Errorf("task %q lock held by another forge process for >%s (last error: %v); retry later", ref, taskLockWait, err)
			}
			time.Sleep(taskLockRetry)
			continue
		}
		// 打破 stale 锁：持锁者变更中途崩溃，否则该 task 未来的所有写者被永久阻塞。
		// 已知罕见竞态（两个打破者同刻、或打破者误删刚重建的新锁）：后果退化为加锁前
		// 的 lost-update 风险，不会更糟——接受。
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

// MutateTaskState performs an atomic load→mutate→save on a task: it takes the
// per-task lock, reloads the state inside the lock (so fn sees the latest
// on-disk content, not a stale pre-lock snapshot), applies fn, and saves. fn
// must only mutate the passed state.
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

// MergeOrPersistTaskState is MutateTaskState for callers holding an in-memory
// state that may not have been persisted yet (ExecuteTaskGate's internal
// persists receive the caller's object; tests and fresh flows construct it
// directly): under the lock it prefers the on-disk state, and only when the file
// does not exist yet does it fall back to base.
//
// MergeOrPersistTaskState 是面向「手里的 state 可能尚未落盘」的调用方（ExecuteTaskGate
// 内部持久化收到的是调用方对象；测试与新建流程直接构造）的 MutateTaskState：
// 锁内优先读盘上状态，仅当文件尚不存在时才回退 base——为首次持久化保留旧的
// 「SaveTaskState 无条件写」行为，同时让之后的每次写入都获得丢失更新保护。
func MergeOrPersistTaskState(root string, base *TaskState, fn func(*TaskState) error) error {
	ref := base.TaskRef
	unlock, err := LockTask(root, ref)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := LoadTaskState(root, ref)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		state = base // first persist: nothing on disk to merge into
	}
	if err := fn(state); err != nil {
		return err
	}
	return SaveTaskState(root, state)
}

// isLockContentionErr 判断 O_EXCL 开锁失败是否为 Windows 瞬态竞争（持锁者尚在
// OpenFile 与 Close 之间、或 AV 索引器正在扫描刚创建的文件时的 sharing-violation /
// access-denied），而非真实失败。按构造仅 Windows 生效：同数值 errno 在 unix 上是
// 无关语义（32=EPIPE），且 unix 上真实的 EACCES 必须保持快速失败，不能重试到超时。
func isLockContentionErr(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return os.IsPermission(err) || // ERROR_ACCESS_DENIED
		errors.Is(err, syscall.Errno(32)) || // ERROR_SHARING_VIOLATION
		errors.Is(err, syscall.Errno(33)) // ERROR_LOCK_VIOLATION
}
