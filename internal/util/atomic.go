// Package util holds cross-cutting filesystem helpers shared by the .forge/
// persistence layers (task state, pipeline state, gate status, tool/check logs).
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

// AtomicWrite writes data to path atomically: a temp file in the SAME directory
// is fully written and fsynced, then renamed over the target.
//
// Plain os.WriteFile truncates the target first and writes into place, so a
// crash, power loss, or concurrent write mid-flight leaves a partial file. Every
// .forge/ state loader (task state, pipeline state, gate status,
// active-task-ref) JSON-parses the result and treats a parse error as corrupt —
// a partial write therefore turns a transient crash into a permanently
// unreadable task. AtomicWrite closes that window: readers observe either the
// complete previous version or the complete new version, never a half-written
// mix.
//
// os.Rename is atomic on POSIX (rename(2)). On Windows Go 1.5+ uses MoveFileEx
// with MOVEFILE_REPLACE_EXISTING, so it atomically replaces an existing target
// without a delete-then-rename race window. The temp file is created next to the
// target so the rename never crosses a filesystem boundary (which would degrade
// to copy+delete and lose atomicity).
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

// renameWithRetry renames old→new, retrying briefly when Windows refuses to
// replace a target a concurrent reader still holds open. MoveFileEx with
// MOVEFILE_REPLACE_EXISTING (Go's os.Rename on Windows) returns
// ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION in that case, unlike POSIX
// rename(2) which replaces unconditionally. Each attempt stays atomic; only the
// timing stretches to cover a reader's short open window (e.g. a LoadTaskState
// finishing its ReadFile). Non-retryable errors fail fast on the first attempt.
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

// isRetryableRenameErr reports whether a rename error is the transient Windows
// "target held open by a concurrent reader" condition worth retrying. The
// Windows errno values never appear from POSIX rename, so this always returns
// false there — POSIX renames either succeed or fail non-transiently.
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

// ArchivedName returns a non-colliding archive path for a rotated log file
// (.forge/<filePrefix>-<stamp>.jsonl).
//
// The stamp carries nanosecond precision so two archives created in the same
// second — concurrent task starts, or a fast Archive-then-Clear cycle — no
// longer clobber each other. The previous second-precision stamp collided
// silently on POSIX (os.Rename overwrote the prior archive) and errored on
// Windows (Rename refuses an existing target), losing one of the two rotated
// logs. On the astronomically rare same-nanosecond tie, a numeric suffix is
// appended via a stat check.
func ArchivedName(dir, filePrefix string, now time.Time) string {
	stamp := now.Format("20060102150405.000000000")
	dst := filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", filePrefix, stamp))
	for i := 1; ; i++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			return dst
		} else if err != nil {
			return dst // stat error: best-effort, let the caller's Rename surface it
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
