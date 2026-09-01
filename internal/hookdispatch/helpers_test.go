package hookdispatch

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// helpers_test.go —— hookdispatch 测试共享助手。captureStdout 与
// internal/cli/helpers_test.go、internal/clitask/helpers_test.go 同实现；
// captureOutput 与本包 hook_kimi_test.go 原定义一致（迁移自带）——测试助手
// 无法跨包共享，注释互指防漂移（2026-09 普查 A2-2）。

// captureStdout 临时重定向 os.Stdout 捕获命令输出（输出层单测）。
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
