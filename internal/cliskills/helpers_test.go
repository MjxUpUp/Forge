package cliskills

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// mustMkdir 把 (t, err) 收口为 Fatal——与 internal/cli 测试同名的 3 行助手
// （测试文件无法跨包共享未导出助手，复制是测试代码的常规代价）。
func mustMkdir(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// writeSkill 在 canonical dir 下建一个带 triggers 的 skill（裸 JSON——frontmatter.go
// nestedRe 不剥嵌套 metadata 引号）。与 internal/cli/skill_trigger_hook_test.go 的
// 同名助手同实现——测试助手无法跨包共享，注释互指防漂移。
func writeSkill(t *testing.T, canonicalDir, name, triggersJSON string) {
	t.Helper()
	dir := filepath.Join(canonicalDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\nmetadata:\n  triggers: " + triggersJSON + "\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// captureStdout 临时重定向 os.Stdout 捕获输出（输出层单测）。与 internal/cli
// helpers_test.go 的同名助手同实现——测试助手无法跨包共享，注释互指防漂移。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// captureStderr 临时重定向 os.Stderr 捕获 Warnings 等告警输出（告警走 stderr，与正常输出分离）。
// defer 在赋值后立即注册：fn panic 时也保证 Stderr 恢复 + pipe 关闭（防污染后续测试 + pipe 半挂）。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = old
		w.Close()
		r.Close()
	}()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	w.Close() // 触发 goroutine EOF
	<-done    // 等待读取完成
	return buf.String()
}

// TestPrintInstallReport_DriftSkipDetail: drift+skip must list details + give a sync reminder.
//
// TestPrintInstallReport_DriftSkipDetail：drift+skip 必须列明细 + 给出同步提醒。

// TestVersionFn_LazyAndFallback 钉死版本 seam 契约（2026-09 普查 A2-1 审查发现
// F1 的回归守卫）：惰性闭包取值（SetVersion 在 main() 才写 rootCmd.Version，
// init 期拷贝恒空串）+ 未注册时兜底 "dev"（空串会污染 embed 缓存版本标记，
// 与真实版本永不等值触发无谓全量重建）。
func TestVersionFn_LazyAndFallback(t *testing.T) {
	// 未注册：兜底 "dev"，绝不出空串。
	if got := version(); got != "dev" {
		t.Fatalf("VersionFn 未注册时 version() = %q, want %q", got, "dev")
	}
	// 惰性：注册后每次调用现取——provider 返回值变化能被观察到。
	VersionFn = func() string { return "1.2.3" }
	defer func() { VersionFn = nil }()
	if got := version(); got != "1.2.3" {
		t.Fatalf("version() = %q, want %q", got, "1.2.3")
	}
	VersionFn = func() string { return "9.9.9" }
	if got := version(); got != "9.9.9" {
		t.Fatalf("version() 应惰性现取: got %q, want %q", got, "9.9.9")
	}
}
