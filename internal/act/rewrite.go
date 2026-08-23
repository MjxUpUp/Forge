package act

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// RewriteAll atomically replaces the conclusions file with the given (time-ordered)
// slice — the write-side counterpart of LoadAll for in-place migrations (remigrate).
// The caller owns the backup (act remigrate writes .migrate-bak before calling);
// RewriteAll writes to a temp file first and renames over the target so a crash never
// leaves a half-written JSONL (LoadAll would skip the corrupt tail silently — the
// damage would be invisible, hence temp+rename).
//
// RewriteAll 用给定（时间序）切片原子替换 conclusions 文件——LoadAll 的写入侧对
// 等物，供就地迁移（remigrate）用。备份由调用方负责（act remigrate 调用前先写
// .migrate-bak）；RewriteAll 先写临时文件再 rename 落位，崩溃不会留下半写的
// JSONL（LoadAll 会静默跳过损坏尾行——那种损坏不可见，所以必须 temp+rename）。
func RewriteAll(p *forgedata.Project, cs []Conclusion) error {
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(p.ActDir(), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(p.ActDir(), ".conclusions-rewrite-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename; cleans up on failure paths
	// / 成功 rename 后是 no-op；失败路径上负责清理

	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	for _, c := range cs {
		if err := enc.Encode(c); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Preserve the original file mode across the temp+rename swap: CreateTemp defaults
	// to 0600, which would silently change permissions of the conclusions file on Unix.
	// Windows relies on ACL inheritance and ignores Chmod — no-op there. If the target
	// is held open by another process (watcher/indexer), Windows rename fails loudly —
	// the caller (act remigrate) has already written .migrate-bak, so data is
	// recoverable; we surface the error rather than corrupt anything.
	//
	// temp+rename 换位时保留原文件权限：CreateTemp 默认 0600，会在 Unix 上静默改变
	// conclusions 文件的权限。Windows 依赖 ACL 继承、忽略 Chmod——在该平台是 no-op。
	// 若目标被其他进程占用（watcher/索引器），Windows rename 会响亮地失败——调用方
	//（act remigrate）已先写 .migrate-bak，数据可恢复；此处上抛错误而非损坏任何东西。
	if info, err := os.Stat(p.ActConclusionsPath()); err == nil {
		_ = os.Chmod(tmpName, info.Mode().Perm())
	}
	if err := os.Rename(tmpName, p.ActConclusionsPath()); err != nil {
		return err
	}
	return nil
}
