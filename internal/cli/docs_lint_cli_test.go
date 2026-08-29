package cli

import (
	"errors"
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

// TestRunDocsLintHardFailureReturnsSentinel pins the exit-code refactor: a hard
// lint failure must surface as the errHardExit sentinel (mapped to exit 2 by
// Execute), NOT an in-RunE os.Exit(2) — os.Exit skips every defer in the cobra
// chain and Execute's panic-recovery funnel. A clean file returns nil. The
// sentinel is matched via errors.As, so wrapping survives.
//
// TestRunDocsLintHardFailureReturnsSentinel 钉住退出码重构：lint 硬失败必须以
// errHardExit 哨兵浮出（由 Execute 映射为 exit 2），而不是 RunE 内 os.Exit(2)
// ——os.Exit 会跳过 cobra 链上的所有 defer 与 Execute 的 panic 恢复盘。干净文件
// 返回 nil。用 errors.As 匹配哨兵，包装后仍可命中。
func TestRunDocsLintHardFailureReturnsSentinel(t *testing.T) {
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.md")
	if err := os.WriteFile(clean, []byte("# ok\n内容干净，无禁令短语。\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "hard.md")
	if err := os.WriteFile(hard, []byte("# bad\n综上所述，这个方案是可行的。\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Clean file: no hard failure → nil (exit 0 path).
	//
	// 干净文件：无硬失败 → nil（exit 0 路径）。
	if err := runDocsLint(nil, []string{clean}); err != nil {
		t.Fatalf("clean file: err = %v, want nil", err)
	}

	// Hard failure (D1 ban phrase) → errHardExit sentinel via errors.As.
	//
	// 硬失败（D1 禁令短语）→ errors.As 命中 errHardExit 哨兵。
	err := runDocsLint(nil, []string{hard})
	var hex *hardExitError
	if !errors.As(err, &hex) {
		t.Fatalf("hard failure: err = %v, want errHardExit sentinel (Execute maps it to exit 2)", err)
	}
}
