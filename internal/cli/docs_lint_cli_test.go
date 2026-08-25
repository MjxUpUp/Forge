package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docs_lint_cli_test.go: unit cover for the CLI's explicit-path target
// collection (directory recursion + .md filter + missing-path error). The
// --base branch is a thin pass-through to taskpipeline.ChangedMarkdownSince,
// whose gate-parity semantics (untracked included, deleted dropped) are
// covered by TestChangedMarkdownSinceIncludesUntrackedAndSkipsDeleted; here
// findProjectRoot would require a forge-registered project, out of scope.
//
// docs_lint_cli_test.go：CLI 显式路径目标收集的单测（目录递归 + .md 过滤 +
// 路径不存在报错）。--base 分支是对 taskpipeline.ChangedMarkdownSince 的薄
// 透传，其门禁一致语义（含未跟踪、剔已删除）由
// TestChangedMarkdownSinceIncludesUntrackedAndSkipsDeleted 覆盖；此处
// findProjectRoot 需要 forge 注册项目，不在单测范围。
func TestCollectLintTargetsExplicitPaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(dir, "a.md"),
		filepath.Join(dir, "notes.txt"),
		filepath.Join(sub, "b.md"),
	} {
		if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := collectLintTargets([]string{dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(files, ",")
	if !strings.Contains(joined, "a.md") || !strings.Contains(joined, "b.md") {
		t.Fatalf("目录递归应收集全部 .md, got %v", files)
	}
	if strings.Contains(joined, "notes.txt") {
		t.Fatalf("非 markdown 不应收集, got %v", files)
	}

	if _, err := collectLintTargets([]string{filepath.Join(dir, "missing.md")}, ""); err == nil {
		t.Fatal("不存在的路径应报错（不静默空集）")
	}
}
