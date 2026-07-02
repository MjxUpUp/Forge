package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestExtractTo 验证内置 skill 库能完整解压到真实目录：CONVENTIONS.md + 至少一个 skill 的 SKILL.md。
// 这是 embed fallback（无 --canonical 时）的核心，link 模式不可用时的唯一分发路径。
func TestExtractTo(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractTo(dir); err != nil {
		t.Fatalf("ExtractTo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CONVENTIONS.md")); err != nil {
		t.Fatalf("CONVENTIONS.md 未解压: %v", err)
	}
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("ReadDir FS: %v", err)
	}
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("无 skill 的 SKILL.md 被解压")
	}
}

// TestExtractTo_NoGoFiles 守护：embed_test.go（无 //go:embed 指令）会被 `*` 嵌入，
// 但它是包内测试产物，绝不能进入分发缓存——否则 link/copy 会把它带进用户目标目录。
// ExtractTo 必须显式跳过 .go。
func TestExtractTo_NoGoFiles(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractTo(dir); err != nil {
		t.Fatalf("ExtractTo: %v", err)
	}
	var leaked []string
	werr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(p) == ".go" {
			leaked = append(leaked, p)
		}
		return nil
	})
	if werr != nil {
		t.Fatalf("walk extract dir: %v", werr)
	}
	if len(leaked) > 0 {
		t.Fatalf(".go files leaked into extract dir (test artifacts pollute cache): %v", leaked)
	}
}
