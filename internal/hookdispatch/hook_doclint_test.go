package hookdispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docLintWriteIn 构造 PostToolUse Write|Edit 形的 hook 输入。
func docLintWriteIn(tool, file string) HookInput {
	raw, _ := json.Marshal(map[string]string{"file_path": file})
	return HookInput{
		HookEventName: "PostToolUse",
		SessionID:     "sess-dl-1",
		ToolName:      tool,
		ToolInput:     raw,
	}
}

func resetDocLintMarkers(t *testing.T, sessionID string) {
	t.Helper()
	_ = os.RemoveAll(filepath.Join(os.TempDir(), "forge-doclint", sessionID))
}

// TestDocLintHook_EmitsOnViolation 命中禁令短语的 .md 写入发 advisory（带规则
// 与行号），checklog 落 doc-lint 观察条目。
func TestDocLintHook_EmitsOnViolation(t *testing.T) {
	root := trackTestProject(t)
	resetDocLintMarkers(t, "sess-dl-1")
	defer resetDocLintMarkers(t, "sess-dl-1")

	doc := filepath.Join(root, "docs", "bad.md")
	if err := os.MkdirAll(filepath.Dir(doc), 0755); err != nil {
		t.Fatal(err)
	}
	// 注意：本测试文件自身的源码以反引号引用禁令短语（数据不是使用）。
	if err := os.WriteFile(doc, []byte("# 报告\n\n综上所述，测试通过：`go test` 全绿。\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := injectedText(t, captureStdout(t, func() {
		if err := runDocLintHook(docLintWriteIn("Write", doc), root, "test", ""); err != nil {
			t.Fatalf("doc-lint hook must never error: %v", err)
		}
	}))
	if !strings.Contains(out, "doc-lint") || !strings.Contains(out, "D1") {
		t.Fatalf("advisory missing doc-lint header or D1 finding:\n%s", out)
	}
	if !strings.Contains(out, "L3") {
		t.Fatalf("advisory missing line number:\n%s", out)
	}
	entries := findTrackEntries(t, root, "doc-lint")
	if len(entries) != 1 {
		t.Fatalf("expected 1 doc-lint observation, got %d", len(entries))
	}
}

// TestDocLintHook_DedupOnUnchangedReemitOnChange 同一文件同一组命中重复保存不
// 重发；命中集合变化（修复后改用另一短语、行号变化）即重发。
func TestDocLintHook_DedupOnUnchangedReemitOnChange(t *testing.T) {
	root := trackTestProject(t)
	resetDocLintMarkers(t, "sess-dl-2")
	defer resetDocLintMarkers(t, "sess-dl-2")

	doc := filepath.Join(root, "docs", "dedup.md")
	_ = os.MkdirAll(filepath.Dir(doc), 0755)
	write := func(content string) {
		if err := os.WriteFile(doc, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run := func() string {
		return captureStdout(t, func() {
			_ = runDocLintHook(docLintWriteIn("Edit", doc), root, "test", "")
		})
	}

	write("# t\n\n综上所述，A。\n")
	first := run()
	if first == "" {
		t.Fatal("first violation must emit")
	}
	if !strings.Contains(injectedText(t, first), "D1") {
		t.Fatalf("emission missing D1 finding:\n%s", first)
	}
	write("# t\n\n综上所述，A。\n")
	if again := run(); again != "" {
		t.Fatalf("unchanged issues must be silent, got:\n%s", again)
	}
	write("# t\n\n基本可以，B。\n")
	if changed := run(); changed == "" {
		t.Fatal("changed issues must re-emit")
	}
	write("# t\n\n干净正文，无命中。\n")
	if clean := run(); clean != "" {
		t.Fatalf("clean doc must be silent, got:\n%s", clean)
	}
}

// TestDocLintHook_SilentPaths 非 .md、项目根外、豁免路径（decisions.md）均静默。
func TestDocLintHook_SilentPaths(t *testing.T) {
	root := trackTestProject(t)
	resetDocLintMarkers(t, "sess-dl-3")
	defer resetDocLintMarkers(t, "sess-dl-3")

	bad := "# t\n\n综上所述。\n"
	cases := map[string]struct {
		path    string
		writeIt bool
	}{
		"非 markdown": {filepath.Join(root, "notes.txt"), true},
		"项目根外":       {filepath.Join(t.TempDir(), "elsewhere.md"), true},
		"豁免路径":       {filepath.Join(root, "skills", "x", "decisions.md"), true},
		"读取失败（不存在）":  {filepath.Join(root, "docs", "ghost.md"), false},
	}
	for name, tc := range cases {
		_ = os.MkdirAll(filepath.Dir(tc.path), 0755)
		if tc.writeIt {
			_ = os.WriteFile(tc.path, []byte(bad), 0644)
		}
		out := captureStdout(t, func() {
			_ = runDocLintHook(docLintWriteIn("Write", tc.path), root, "test", "")
		})
		if out != "" {
			t.Errorf("%s must be silent, got:\n%s", name, out)
		}
	}
}
