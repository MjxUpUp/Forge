package hooks

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoCompileHook_DocsOnlyDiffExitsZero 钉住一次真实事故：纯文档改动让
// auto-compile 的 gate 模式静默退出 1。
//
// 场景：task-implement 门禁经 runEmbeddedHook 跑本脚本（无 FORGE_FILE_PATH，
// 走 git diff 分支）。脚本里原来写的是
//
//	[ -n "$_f" ] && is_source "$_f" && { TOUCHED_SOURCE=1; break; }
//
// 遇到 README.md 时 is_source 返回 1，整条 and-list 随之返回 1；在 set -e 下
// 这是循环体的失败状态 → 脚本不输出任何东西就 exit 1 → 门禁报
// 「build failed: 」（空 detail），任何只改文档的任务都过不了实现门。
//
// 断言两件事：退出码 0，且 stdout 有 PASS（否则 hook 会把空 additionalContext
// 当作没有提醒，虽然不阻塞，但也不该静默）。
func TestAutoCompileHook_DocsOnlyDiffExitsZero(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 不在 PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不在 PATH")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	// 工作区只改文档：脚本要能遍历完 diff 且不因 is_source 返回 1 而 abort。
	if err := os.WriteFile(readme, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	script := filepath.Join(t.TempDir(), "auto-compile.sh")
	if err := os.WriteFile(script, []byte(AutoCompileHook), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, dir)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("auto-compile 退出 %d（stderr: %s）——纯文档 diff 不该让脚本失败",
				exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		t.Fatalf("auto-compile: %v", err)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("auto-compile 输出里没有 PASS:\n%s", out)
	}
}
