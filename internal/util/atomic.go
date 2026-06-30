// Package util holds cross-cutting filesystem helpers shared by the .forge/
// persistence layers (task state, pipeline state, gate status, tool/check logs).
package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
