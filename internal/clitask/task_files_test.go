package clitask

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// snapshotTaskFiles reads every task state file under root's DataDir, keyed by file name.
//
// snapshotTaskFiles 读出 root 的 DataDir 下全部任务状态文件（按文件名键控），供测试做
// 「前后字节相同」的断言。
func snapshotTaskFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(forgedata.DataDirFor(root), `tasks`, `*.json`))
	if err != nil {
		t.Fatalf(`glob task files: %v`, err)
	}
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf(`read %s: %v`, p, err)
		}
		out[filepath.Base(p)] = string(b)
	}
	return out
}
