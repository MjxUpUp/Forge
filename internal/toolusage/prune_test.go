package toolusage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// TestPrune_NonDestructive 钉住 T1 契约（multi-task-concurrency §5）：Prune 是 task
// start 处取代 Clear 的非破坏性半边——只删超期的 toollog-*.jsonl【归档】，绝不碰
// active toollog.jsonl；目录空/缺失（init 前降级）时为无操作。
func TestPrune_NonDestructive(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	dir := dataDir(root)
	os.MkdirAll(dir, 0o755)

	active := filepath.Join(dir, "toollog.jsonl")
	freshArchive := util.ArchivedName(dir, "toollog", time.Now())
	staleArchive := util.ArchivedName(dir, "toollog", time.Now().AddDate(0, 0, -40))
	for _, p := range []string{active, freshArchive, staleArchive} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	Prune(root)

	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active 文件绝不可被 Prune 删除: %v", err)
	}
	if _, err := os.Stat(freshArchive); err != nil {
		t.Fatalf("窗口内归档不应被清理: %v", err)
	}
	if _, err := os.Stat(staleArchive); !os.IsNotExist(err) {
		t.Fatalf("超期归档应被清理: %v", err)
	}

	// 目录缺失：无操作不 panic（init 前降级）。
	Prune(t.TempDir() + "-missing-root")
}
