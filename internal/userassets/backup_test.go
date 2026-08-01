package userassets

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestBackupOriginal_FirstBackup guards the basic contract: backing up an
// existing file stores its bytes under <BackupRoot>/<safe-id>/original plus a
// meta.json with existed=true and the file path.
//
// TestBackupOriginal_FirstBackup 守护基本契约：备份已存在的文件会把其字节存到
// <BackupRoot>/<safe-id>/original，并写 existed=true、含文件路径的 meta.json。
func TestBackupOriginal_FirstBackup(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	target := filepath.Join(t.TempDir(), `CLAUDE.md`)
	if err := os.WriteFile(target, []byte(`user content`), 0644); err != nil {
		t.Fatalf(`seed target: %v`, err)
	}

	if err := BackupOriginal(target); err != nil {
		t.Fatalf(`BackupOriginal: %v`, err)
	}

	dir, err := backupDir(target)
	if err != nil {
		t.Fatalf(`backupDir: %v`, err)
	}
	original, err := os.ReadFile(filepath.Join(dir, `original`))
	if err != nil {
		t.Fatalf(`backup original not written: %v`, err)
	}
	if string(original) != `user content` {
		t.Errorf(`backup original = %q, want %q`, original, `user content`)
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, `meta.json`))
	if err != nil {
		t.Fatalf(`meta.json not written: %v`, err)
	}
	var meta backupMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf(`parse meta.json: %v`, err)
	}
	if meta.Path != target {
		t.Errorf(`meta.Path = %q, want %q`, meta.Path, target)
	}
	if !meta.Existed {
		t.Error(`meta.Existed = false, want true (file existed at backup time)`)
	}
	if meta.BackedUpAt == `` {
		t.Error(`meta.BackedUpAt must be recorded`)
	}
}

// TestBackupOriginal_NeverOverwrites pins the rollback anchor: a second backup
// after the file changed must NOT overwrite the first backup — the first
// backup is the pre-forge state the user rolls back to.
//
// TestBackupOriginal_NeverOverwrites 钉死回滚锚点：文件变更后的第二次备份
// 不得覆盖首次备份——首次备份才是用户回滚到的 forge 触碰前状态。
func TestBackupOriginal_NeverOverwrites(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	target := filepath.Join(t.TempDir(), `AGENTS.md`)
	if err := os.WriteFile(target, []byte(`v1`), 0644); err != nil {
		t.Fatalf(`seed target: %v`, err)
	}
	if err := BackupOriginal(target); err != nil {
		t.Fatalf(`first BackupOriginal: %v`, err)
	}
	if err := os.WriteFile(target, []byte(`v2`), 0644); err != nil {
		t.Fatalf(`modify target: %v`, err)
	}
	if err := BackupOriginal(target); err != nil {
		t.Fatalf(`second BackupOriginal: %v`, err)
	}

	dir, err := backupDir(target)
	if err != nil {
		t.Fatalf(`backupDir: %v`, err)
	}
	original, err := os.ReadFile(filepath.Join(dir, `original`))
	if err != nil {
		t.Fatalf(`read backup original: %v`, err)
	}
	if string(original) != `v1` {
		t.Errorf(`second backup overwrote the anchor: original = %q, want %q`, original, `v1`)
	}
}

// TestBackupOriginal_NotExisted guards the forge-created-file branch: backing
// up a nonexistent path records existed=false and stores NO original file, so
// rollback knows to delete rather than restore.
//
// TestBackupOriginal_NotExisted 守护 forge 创建文件分支：备份不存在的路径记录
// existed=false 且不存 original 文件，回滚据此知道应删除而非恢复。
func TestBackupOriginal_NotExisted(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	target := filepath.Join(t.TempDir(), `does-not-exist.md`)

	if err := BackupOriginal(target); err != nil {
		t.Fatalf(`BackupOriginal: %v`, err)
	}

	dir, err := backupDir(target)
	if err != nil {
		t.Fatalf(`backupDir: %v`, err)
	}
	if _, err := os.Stat(filepath.Join(dir, `original`)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf(`original must not be stored for a nonexistent file, stat err = %v`, err)
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, `meta.json`))
	if err != nil {
		t.Fatalf(`meta.json not written: %v`, err)
	}
	var meta backupMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf(`parse meta.json: %v`, err)
	}
	if meta.Existed {
		t.Error(`meta.Existed = true, want false (file did not exist at backup time)`)
	}
}

// TestRestoreOriginal_Existed guards the existed=true rollback branch: after
// forge modified the file, restore copies the original bytes back.
//
// TestRestoreOriginal_Existed 守护 existed=true 回滚分支：forge 改过文件后，
// 恢复把原始字节拷回。
func TestRestoreOriginal_Existed(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	target := filepath.Join(t.TempDir(), `CLAUDE.md`)
	if err := os.WriteFile(target, []byte(`user content`), 0644); err != nil {
		t.Fatalf(`seed target: %v`, err)
	}
	if err := BackupOriginal(target); err != nil {
		t.Fatalf(`BackupOriginal: %v`, err)
	}
	if err := os.WriteFile(target, []byte(`user content + forge section`), 0644); err != nil {
		t.Fatalf(`simulate forge write: %v`, err)
	}

	restored, err := RestoreOriginal(target)
	if err != nil {
		t.Fatalf(`RestoreOriginal: %v`, err)
	}
	if !restored {
		t.Error(`RestoreOriginal = false, want true (backup exists)`)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf(`read restored file: %v`, err)
	}
	if string(got) != `user content` {
		t.Errorf(`restored content = %q, want %q`, got, `user content`)
	}
}

// TestRestoreOriginal_NotExisted guards the existed=false rollback branch: the
// file only exists because forge created it, so restore deletes it.
//
// TestRestoreOriginal_NotExisted 守护 existed=false 回滚分支：文件因 forge
// 创建才存在，恢复即删除它。
func TestRestoreOriginal_NotExisted(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	target := filepath.Join(t.TempDir(), `AGENTS.md`)
	if err := BackupOriginal(target); err != nil {
		t.Fatalf(`BackupOriginal: %v`, err)
	}
	if err := os.WriteFile(target, []byte(`forge section`), 0644); err != nil {
		t.Fatalf(`simulate forge create: %v`, err)
	}

	restored, err := RestoreOriginal(target)
	if err != nil {
		t.Fatalf(`RestoreOriginal: %v`, err)
	}
	if !restored {
		t.Error(`RestoreOriginal = false, want true (backup exists)`)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf(`forge-created file must be deleted on restore, stat err = %v`, err)
	}
}

// TestRestoreOriginal_NoBackup guards the no-op contract: with no backup for
// the path, restore reports (false, nil) and leaves the file untouched.
//
// TestRestoreOriginal_NoBackup 守护 no-op 契约：路径无备份时恢复报告
// (false, nil) 且文件保持不动。
func TestRestoreOriginal_NoBackup(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	target := filepath.Join(t.TempDir(), `CLAUDE.md`)
	if err := os.WriteFile(target, []byte(`untouched`), 0644); err != nil {
		t.Fatalf(`seed target: %v`, err)
	}

	restored, err := RestoreOriginal(target)
	if err != nil {
		t.Fatalf(`RestoreOriginal: %v`, err)
	}
	if restored {
		t.Error(`RestoreOriginal = true, want false (no backup)`)
	}
	got, _ := os.ReadFile(target)
	if string(got) != `untouched` {
		t.Errorf(`file changed without a backup: %q`, got)
	}
}

// TestRestoreAll guards the bulk rollback: every backup under the root is
// restored (existed=true → bytes back, existed=false → file deleted) and the
// restored paths are reported.
//
// TestRestoreAll 守护批量回滚：备份根下每份备份都被恢复（existed=true → 字节
// 拷回，existed=false → 文件删除）并报告已恢复路径。
func TestRestoreAll(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	tmp := t.TempDir()

	existed := filepath.Join(tmp, `CLAUDE.md`)
	if err := os.WriteFile(existed, []byte(`original claude`), 0644); err != nil {
		t.Fatalf(`seed existed: %v`, err)
	}
	created := filepath.Join(tmp, `AGENTS.md`)

	if err := BackupOriginal(existed); err != nil {
		t.Fatalf(`BackupOriginal(existed): %v`, err)
	}
	if err := BackupOriginal(created); err != nil {
		t.Fatalf(`BackupOriginal(created): %v`, err)
	}

	// Simulate forge's modifications.
	//
	// 模拟 forge 的修改。
	if err := os.WriteFile(existed, []byte(`modified`), 0644); err != nil {
		t.Fatalf(`modify existed: %v`, err)
	}
	if err := os.WriteFile(created, []byte(`forge section`), 0644); err != nil {
		t.Fatalf(`create created: %v`, err)
	}

	restored, errs := RestoreAll()
	if len(errs) > 0 {
		t.Fatalf(`RestoreAll errors: %v`, errs)
	}
	if len(restored) != 2 {
		t.Errorf(`RestoreAll restored %d paths, want 2: %v`, len(restored), restored)
	}
	got, err := os.ReadFile(existed)
	if err != nil {
		t.Fatalf(`read restored existed file: %v`, err)
	}
	if string(got) != `original claude` {
		t.Errorf(`existed file restored to %q, want %q`, got, `original claude`)
	}
	if _, err := os.Stat(created); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf(`created file must be deleted by RestoreAll, stat err = %v`, err)
	}
}

// TestBackupRoot guards the root resolution: BackupRoot must follow
// FORGE_DATA_HOME (never the real home in tests) and end in "backups".
//
// TestBackupRoot 守护根目录解析：BackupRoot 必须跟随 FORGE_DATA_HOME
// （测试中绝不碰真实 home）且以 "backups" 结尾。
func TestBackupRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, tmp)
	root, err := BackupRoot()
	if err != nil {
		t.Fatalf(`BackupRoot: %v`, err)
	}
	if !strings.HasPrefix(root, tmp) || !strings.HasSuffix(root, `backups`) {
		t.Errorf(`BackupRoot = %q, want <FORGE_DATA_HOME>/backups under %q`, root, tmp)
	}
}

// TestBackupOriginal_ConcurrentClaims pins the O_EXCL anchor claim: when many
// goroutines (standing in for racing forge processes) back up the same file
// simultaneously, every call succeeds, exactly one meta.json results, and the
// stored original still holds the PRE-forge bytes (no goroutine's later write
// can clobber the anchor — the stat-then-write race this test guards against
// let a slow writer record already-modified bytes as the "original").
//
// TestBackupOriginal_ConcurrentClaims 钉死 O_EXCL 锚点认领：大量 goroutine
// （模拟竞速的 forge 进程）同时备份同一文件时，全部调用成功，只产生一份
// meta.json，且存下的 original 仍是 forge 修改前的字节（任何后来者都覆盖
// 不了锚点——本测试防的 stat-then-write 竞态曾让慢写者把已修改字节记为
// "original"）。
func TestBackupOriginal_ConcurrentClaims(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	target := filepath.Join(t.TempDir(), `CLAUDE.md`)
	if err := os.WriteFile(target, []byte(`pristine bytes`), 0644); err != nil {
		t.Fatalf(`create target: %v`, err)
	}

	const racers = 32
	errs := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- BackupOriginal(target)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf(`concurrent BackupOriginal: %v`, err)
		}
	}

	// Exactly one valid anchor, holding the pre-forge bytes.
	//
	// 恰好一份有效锚点，持有 forge 修改前的字节。
	dir, err := backupDir(target)
	if err != nil {
		t.Fatalf(`backupDir: %v`, err)
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, `meta.json`))
	if err != nil {
		t.Fatalf(`read meta: %v`, err)
	}
	var meta backupMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf(`parse meta: %v`, err)
	}
	if !meta.Existed {
		t.Errorf(`meta.Existed = false, want true（目标文件备份时已存在）`)
	}
	original, err := os.ReadFile(filepath.Join(dir, `original`))
	if err != nil {
		t.Fatalf(`read original: %v`, err)
	}
	if string(original) != `pristine bytes` {
		t.Errorf(`original = %q, want %q（锚点被竞态污染）`, original, `pristine bytes`)
	}
}
