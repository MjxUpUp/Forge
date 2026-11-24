package capability

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLiveDocsCoversEntryDocs 钉住「用户真正会读的文档都在漂移扫描范围内」。
// 2026-11 的文档审查发现：LiveDocs 只收 README/AGENTS/docs/reference/templates，
// 于是 README.zh-CN、npm/README、homepage、docs/design 长期积累已删命令而 docs closure 仍报 0。
func TestLiveDocsCoversEntryDocs(t *testing.T) {
	root := t.TempDir()
	want := []string{
		"README.md",
		"READMEs/README.zh-CN.md",
		"npm/README.md",
		"homepage/index.md",
		"docs/design/positioning.md",
		"docs/compliance/standards-crosswalk.md",
	}
	for _, w := range want {
		p := filepath.Join(root, filepath.FromSlash(w))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 历史设计记录不参与漂移判定：首屏有「状态：历史」即豁免。
	hist := filepath.Join(root, "docs", "design", "old.md")
	if err := os.WriteFile(hist, []byte("# old\n\n状态：历史（能力已移除）\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := LiveDocs(root)
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("LiveDocs 漏收 %s；files=%v", w, files)
		}
	}
	if set["docs/design/old.md"] {
		t.Errorf("带「状态：历史」的文档应豁免，却被当作活文档：%v", files)
	}
}
