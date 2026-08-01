// Package userassets implements the backup-and-rollback mechanism for forge's
// user-level file modifications (~/.claude/CLAUDE.md, ~/.codex/AGENTS.md).
// Unlike project-level files (rebuildable, team-shared), user-level files are
// personal and not rebuildable — forge must never irreversibly clobber them.
// The contract: before forge's FIRST modification of a user-level file, the
// original bytes are backed up; the user can roll back at any time.
//
// Package userassets 实现 forge 用户级文件修改（~/.claude/CLAUDE.md、
// ~/.codex/AGENTS.md）的备份+回滚机制。与项目级文件（可重建、团队共享）
// 不同，用户级文件是个人的、不可重建——forge 绝不能不可逆地破坏它们。
// 契约：forge 首次修改某用户级文件前，先备份原始字节；用户可随时回滚。
package userassets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// backupMeta records one backup's metadata: which file it anchors, whether the
// file existed at backup time (existed=false means forge created the file, so
// rollback deletes it), and when the backup was taken.
//
// backupMeta 记录一份备份的元数据：锚定哪个文件、备份时文件是否已存在
// （existed=false 表示文件由 forge 创建，回滚即删除它）、备份时间。
type backupMeta struct {
	Path       string `json:"path"`
	Existed    bool   `json:"existed"`
	BackedUpAt string `json:"backed_up_at"`
}

// BackupRoot returns <GlobalHome>/backups — the root under which every
// user-level file backup lives (one subdirectory per file, keyed by a
// filesystem-safe hash of its absolute path). Exported for tests and
// uninstall messaging.
//
// BackupRoot 返回 <GlobalHome>/backups——所有用户级文件备份的根目录
// （每个文件一个子目录，以其绝对路径的文件系统安全哈希命名）。
// 导出供测试与 uninstall 提示使用。
func BackupRoot() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return "", fmt.Errorf("resolve global home: %w", err)
	}
	return filepath.Join(home, "backups"), nil
}

// backupDir resolves the backup directory for one file:
// <BackupRoot>/<safe-id>/ where safe-id is a short sha256 of the absolute path
// (stable across runs, free of path separators and drive letters).
//
// backupDir 解析单个文件的备份目录：<BackupRoot>/<safe-id>/，safe-id 是
// 绝对路径的短 sha256（跨运行稳定，不含路径分隔符与盘符）。
func backupDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path for %s: %w", path, err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	safeID := hex.EncodeToString(sum[:])[:16]
	root, err := BackupRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, safeID), nil
}

// BackupOriginal backs up a user-level file before forge's first modification.
// Stores under <BackupRoot>/<safe-id>/:
//   - original  (file bytes; only when the file existed)
//   - meta.json ({"path": ..., "existed": true/false, "backed_up_at": ...})
//
// NEVER overwrites an existing backup — the first backup wins, because it is
// the rollback anchor (the state before forge ever touched the file).
// Backing up a nonexistent file records existed=false (rollback then deletes
// the forge-created file).
//
// Concurrency: the anchor is claimed with O_CREATE|O_EXCL on meta.json — when two
// forge processes race the first backup, exactly one wins the claim; the loser
// sees EEXIST and returns (the winner's backup covers it). The previous
// stat-then-write sequence let both pass the stat check, and the slower writer
// could record already-forge-modified bytes as the "original", corrupting the
// anchor. Crash window: a winner dying between the meta claim and the original
// write leaves meta without original — RestoreOriginal reports that loudly
// (delete the backup dir to reset), which beats a silently poisoned anchor.
//
// BackupOriginal 在 forge 首次修改前备份一个用户级文件。存储到
// <BackupRoot>/<safe-id>/ 下：
//   - original  （文件字节；仅当文件已存在）
//   - meta.json （{"path": ..., "existed": true/false, "backed_up_at": ...}）
//
// 绝不覆盖已有备份——首次备份生效，因为它是回滚锚点（forge 触碰文件之前
// 的状态）。备份不存在的文件会记录 existed=false（回滚时删除 forge 创建
// 的文件）。
//
// 并发：锚点经 meta.json 的 O_CREATE|O_EXCL 认领——两个 forge 进程竞速首备时
// 只有一个赢得认领；负者见 EEXIST 返回（胜者的备份已覆盖）。此前的
// stat-then-write 序列让双方都过 stat 检查，慢者可能把已被 forge 改过的
// 字节记为"original"，污染锚点。崩溃窗口：胜者在 meta 认领与 original
// 写入之间死掉会留下有 meta 无 original 的备份——RestoreOriginal 会响亮报错
// （删除该备份目录即可重置），这好过锚点被静默污染。
func BackupOriginal(path string) error {
	dir, err := backupDir(path)
	if err != nil {
		return err
	}

	meta := backupMeta{Path: path, BackedUpAt: time.Now().UTC().Format(time.RFC3339)}
	original, err := os.ReadFile(path)
	switch {
	case err == nil:
		meta.Existed = true
	case errors.Is(err, fs.ErrNotExist):
		meta.Existed = false
	default:
		return fmt.Errorf("read %s for backup: %w", path, err)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal backup meta: %w", err)
	}
	// Atomic claim: only the first process creates meta.json (O_EXCL). A loser
	// returns immediately — the winner's anchor covers it.
	//
	// 原子认领：只有首个进程能创建 meta.json（O_EXCL）。负者立即返回——
	// 胜者的锚点已覆盖。
	metaPath := filepath.Join(dir, "meta.json")
	fd, err := os.OpenFile(metaPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, fs.ErrExist) {
		// First backup wins — keep the rollback anchor untouched.
		//
		// 首次备份生效——回滚锚点保持不动。
		return nil
	}
	if err != nil {
		return fmt.Errorf("claim backup meta: %w", err)
	}
	if _, err := fd.Write(metaBytes); err != nil {
		fd.Close()
		return fmt.Errorf("write backup meta: %w", err)
	}
	if err := fd.Close(); err != nil {
		return fmt.Errorf("close backup meta: %w", err)
	}
	if meta.Existed {
		if err := util.AtomicWrite(filepath.Join(dir, "original"), original, 0644); err != nil {
			return fmt.Errorf("write backup original: %w", err)
		}
	}
	return nil
}

// RestoreOriginal rolls back one file to its backed-up state:
// meta.existed=true → the original bytes are copied back; existed=false → the
// file is deleted (forge created it). Returns (false, nil) when no backup
// exists for the path — rollback is a no-op there.
//
// RestoreOriginal 将单个文件回滚到备份状态：meta.existed=true → 拷回
// 原始字节；existed=false → 删除该文件（由 forge 创建）。该路径无备份时
// 返回 (false, nil)——回滚为 no-op。
func RestoreOriginal(path string) (restored bool, err error) {
	dir, err := backupDir(path)
	if err != nil {
		return false, err
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read backup meta: %w", err)
	}
	var meta backupMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return false, fmt.Errorf("parse backup meta: %w", err)
	}

	if !meta.Existed {
		// Forge created the file — rollback removes it (missing file is fine:
		// the user may have deleted it manually already).
		//
		// 文件由 forge 创建——回滚即删除（文件已不存在也算成功：
		// 用户可能已手动删除）。
		if err := os.Remove(meta.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("remove forge-created file %s: %w", meta.Path, err)
		}
		return true, nil
	}

	original, err := os.ReadFile(filepath.Join(dir, "original"))
	if err != nil {
		return false, fmt.Errorf("read backup original: %w", err)
	}
	if err := util.AtomicWrite(meta.Path, original, 0644); err != nil {
		return false, fmt.Errorf("restore %s: %w", meta.Path, err)
	}
	return true, nil
}

// RestoreAll iterates all backups under the backup root and restores each.
// Returns the paths that were actually restored. Individual failures are
// collected in errs, not fatal — one corrupt backup must not block the
// rollback of the rest.
//
// RestoreAll 遍历备份根下的所有备份并逐一恢复。返回实际恢复的路径。
// 单个失败收集进 errs 而非中断——一份损坏的备份不得阻塞其余文件的回滚。
func RestoreAll() (restored []string, errs []error) {
	root, err := BackupRoot()
	if err != nil {
		return nil, []error{err}
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{fmt.Errorf("read backup root: %w", err)}
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaBytes, err := os.ReadFile(filepath.Join(root, e.Name(), "meta.json"))
		if err != nil {
			errs = append(errs, fmt.Errorf("read backup meta under %s: %w", e.Name(), err))
			continue
		}
		var meta backupMeta
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			errs = append(errs, fmt.Errorf("parse backup meta under %s: %w", e.Name(), err))
			continue
		}
		ok, err := RestoreOriginal(meta.Path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok {
			restored = append(restored, meta.Path)
		}
	}
	return restored, errs
}
