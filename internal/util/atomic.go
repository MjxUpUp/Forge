// Package util 提供 .forge/ 持久化层（task 状态、pipeline 状态、gate 状态、tool/check 日志）
// 共享的横切文件系统辅助函数。
package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// AtomicWrite 原子地把 data 写入 path：先在所在目录写一个临时文件并 fsync，
// 再 rename 覆盖目标文件。
//
// 普通 os.WriteFile 会先 truncate 目标再原地写入，因此崩溃、掉电或半途的并发写
// 都会留下半个文件。每个 .forge/ 状态加载器（task 状态、pipeline 状态、gate 状态、
// active-task-ref）都对结果做 JSON 解析，并把解析错误视为损坏——因此一次不完整写入
// 会把短暂的崩溃放大为永久不可读的 task。AtomicWrite 关掉这个时间窗：读者要么看到
// 完整的旧版本，要么看到完整的新版本，绝不会读到写一半的混合体。
//
// os.Rename 在 POSIX 上是原子的（rename(2)）。Windows 上 Go 1.5+ 用 MoveFileEx
// 配 MOVEFILE_REPLACE_EXISTING，因此原子替换既有目标，不会出现 delete-then-rename 的
// 竞态窗口。临时文件就建在目标文件旁边，rename 永不跨文件系统边界（跨边界会退化成
// copy+delete 并丢失原子性）。
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	removeTmp := func() { os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		removeTmp()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		removeTmp()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		removeTmp()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		removeTmp()
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if err := renameWithRetry(tmpName, path); err != nil {
		removeTmp()
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	return nil
}

// renameWithRetry 把 old 重命名为 new，并在 Windows 因并发读者仍持有目标文件而拒绝
// 替换时短暂重试。此时 MoveFileEx 配 MOVEFILE_REPLACE_EXISTING（即 Go 在 Windows 上的
// os.Rename）返回 ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION，与 POSIX rename(2)
// 无条件替换不同。每次重试仍是原子的；只是把时间窗拉长，覆盖读者的短暂 open 窗口
// （如 LoadTaskState 跑完它的 ReadFile）。不可重试的错误在首次尝试即 fail fast。
func renameWithRetry(old, new string) error {
	const (
		attempts   = 6
		retryDelay = 15 * time.Millisecond
	)
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Rename(old, new); err == nil {
			return nil
		}
		if !isRetryableRenameErr(err) {
			return err
		}
		time.Sleep(retryDelay)
	}
	return err
}

// isRetryableRenameErr 判断一个 rename 错误是否是值得重试的瞬态 Windows
// target held open by a concurrent reader 条件。这些 Windows errno 不会出现在
// POSIX rename 里，所以在 POSIX 上恒返回 false——POSIX rename 要么成功要么以
// 非瞬态方式失败。
func isRetryableRenameErr(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	const (
		errorAccessDenied     syscall.Errno = 5
		errorSharingViolation syscall.Errno = 32
	)
	return errno == errorAccessDenied || errno == errorSharingViolation
}

// ArchivedName 为轮转的日志文件返回一个不冲突的归档路径
// （.forge/<filePrefix>-<stamp>.jsonl）。
//
// stamp 带纳秒精度，因此同一秒创建的两个归档——并发 task start，或一次快速的
// Archive-then-Clear 循环——不会再互相覆盖。原先秒精度 stamp 在 POSIX 上静默冲突
// （os.Rename 覆盖了前一个归档），在 Windows 上报错（Rename 拒绝既有目标），
// 损失两条轮转日志之一。在极罕见的同纳秒撞车情况下，通过 stat 检查追加数字后缀。
func ArchivedName(dir, filePrefix string, now time.Time) string {
	stamp := now.Format("20060102150405.000000000")
	dst := filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", filePrefix, stamp))
	for i := 1; ; i++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			return dst
		} else if err != nil {
			return dst // stat 错误：best-effort，交由调用方的 Rename 上报
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s-%s-%d.jsonl", filePrefix, stamp, i))
	}
}

// archiveTimestamp 解析归档文件名中的时间戳。ArchivedName 产生 {prefix}-{stamp}.jsonl，
// stamp 当前为纳秒精度 "20060102150405.000000000"，早期版本为秒精度 "20060102150405"，
// 同纳秒冲突时追加 "-{i}" 后缀。本函数兼容三种命名，解析失败返回 zero+false（调用方
// fallback mtime）。prefix 不含 glob 元字符，TrimPrefix 安全。
func archiveTimestamp(name, prefix string) (time.Time, bool) {
	rest := strings.TrimPrefix(name, prefix+"-")
	rest = strings.TrimSuffix(rest, ".jsonl")
	// 去掉同纳秒冲突后缀 "-{i}"：stamp 本身是纯数字+点，不含 '-'，故首个 '-' 之前即 stamp。
	if idx := strings.Index(rest, "-"); idx >= 0 {
		rest = rest[:idx]
	}
	for _, layout := range []string{"20060102150405.000000000", "20060102150405"} {
		if t, err := time.Parse(layout, rest); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// PruneArchives 删除 dir 下 {prefix}-*.jsonl 归档中归档时刻早于 cutoff 的文件。
// 不碰 active 文件 {prefix}.jsonl——它无 "-"，glob "{prefix}-*" 不匹配。
//
// 归档龄优先按文件名时间戳（ArchivedName 写入的轮转时刻，语义最准）；解析失败
// fallback mtime：os.Rename 保留源文件 mtime，故对正常归档两者一致，fallback 只为
// 容错外部改动 mtime 的场景。best-effort：单个文件的 stat/解析/删除失败跳过并累积进
// err，不中断整体清理——清理是 Clear 的副作用，不该因一个坏文件让 task start 失败。
// 返回删除数 + 累积的非致命错误。
func PruneArchives(dir, prefix string, cutoff time.Time) (removed int, err error) {
	matches, gerr := filepath.Glob(filepath.Join(dir, prefix+"-*.jsonl"))
	if gerr != nil {
		return 0, gerr
	}
	var errs []string
	for _, path := range matches {
		t, ok := archiveTimestamp(filepath.Base(path), prefix)
		if !ok {
			info, sterr := os.Stat(path)
			if sterr != nil {
				continue // 文件可能被并发删除，跳过
			}
			t = info.ModTime()
		}
		if !t.Before(cutoff) {
			continue
		}
		if rerr := os.Remove(path); rerr != nil {
			if !os.IsNotExist(rerr) {
				errs = append(errs, rerr.Error())
			}
			continue
		}
		removed++
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("prune %s archives: %s", prefix, strings.Join(errs, "; "))
	}
	return removed, nil
}

// RetentionDays 读 envName 解析为 retention 天数；缺失或非法返回 defaultDays。
// ≤0 表示禁用 retention（调用方据此跳过清理）。env 覆盖默认值用于按需调整：设为较小值
// （如 14）后跑一次 task start 即回收对应天数前的归档；设为 0 则完全关闭自动清理。
func RetentionDays(envName string, defaultDays int) int {
	raw := os.Getenv(envName)
	if raw == "" {
		return defaultDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultDays
	}
	return n
}
