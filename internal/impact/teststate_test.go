package impact

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectTestStateSurvivesBrokenLedger：记录文件不可读时，账本段仍要为每个
// 建议目录给出 orphaned 计数，而不是整段消失。
func TestCollectTestStateSurvivesBrokenLedger(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "x")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(pkg, "x_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// 用目录占位记录文件路径，让 LoadRecords 的 ReadFile 失败。
	if err := os.MkdirAll(filepath.Join(root, ".forge", "test-exec.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := collectTestState(root, []string{"internal/x"})
	if len(got) != 1 {
		t.Fatalf("账本读失败时仍应输出目录摘要：%+v", got)
	}
	if got[0].Orphaned != 1 || got[0].Live != 0 {
		t.Fatalf("没有可用记录时应全记 orphaned：%+v", got[0])
	}
}
