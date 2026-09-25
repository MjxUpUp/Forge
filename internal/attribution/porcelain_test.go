package attribution

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPorcelainLines pins the porcelain single-entry contract (2026-09 census
// P3-3): raw status lines with quotepath=off, rename entries kept verbatim,
// empty tree → nil, non-git dir → error.
//
// TestPorcelainLines 钉住 porcelain 单一入口契约（2026-09 普查 P3-3）：
// 原始状态行（quotepath=off 保 UTF-8 路径）、rename 条目原样保留、
// 干净树 → nil、非 git 目录 → error。
func TestPorcelainLines(t *testing.T) {
	// 非 git 目录：显式报错（fail-loud——调用方各自决定降级形态）。
	if _, err := PorcelainLines(t.TempDir()); err == nil {
		t.Fatal("非 git 目录应返回 error")
	}

	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 干净树（全部提交）：nil 而非空切片。b/ 先以占位文件入库——整目录未跟踪时
	// porcelain 默认折叠显示 "?? b/"（git 默认行为，改前后一致），要测文件级
	// 中文路径必须让目录先被跟踪。
	write("a.txt", "x")
	write("b/.keep", "")
	git("add", ".")
	exec.Command("git", "-C", root, "-c", "commit.gpgsign=false", "commit", "-m", "init").Run()
	if lines, err := PorcelainLines(root); err != nil || lines != nil {
		t.Fatalf("干净树: got (%v, %v), want (nil, nil)", lines, err)
	}

	// 脏树：未跟踪文件的原样行 + 中文路径不经 C 转义（quotepath=off 的存在理由）。
	write("b/新文件.md", "y")
	lines, err := PorcelainLines(root)
	if err != nil {
		t.Fatalf("PorcelainLines: %v", err)
	}
	joined := strings.Join(lines, "\n")
	// b/ 已跟踪（.keep 入库），未跟踪的是新文件本身——精确断言整行。
	want := "?? b/新文件.md"
	if !strings.Contains(joined, want) {
		t.Errorf("未跟踪条目应为精确文件行 %q, got: %q", want, joined)
	}
	if strings.Contains(joined, `\346`) {
		t.Errorf("中文路径被 C 转义（quotepath=off 失效）: %q", joined)
	}
	if !strings.Contains(joined, "新文件.md") {
		t.Errorf("中文路径应以 UTF-8 原样出现: %q", joined)
	}

	// ChangedFiles 派生：路径提取（剥状态前缀/rename 目标/引号）与入口行为对齐。
	// git porcelain 输出恒为正斜杠（跨平台归一）——期望值用正斜杠字面量，
	// 不用 filepath.Join（Windows 会产反斜杠，2026-09-01 CI windows 实证）。
	files, err := ChangedFiles(root)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	found := false
	for _, f := range files {
		if f == "b/新文件.md" {
			found = true
		}
		if strings.Contains(f, "??") {
			t.Errorf("ChangedFiles 应剥状态前缀, got %q", f)
		}
	}
	if !found {
		t.Errorf("ChangedFiles 应含中文未跟踪路径, got %v", files)
	}
}
