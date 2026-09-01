package cli

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// helpers_test.go —— cli 测试的共享输出捕获助手。曾住在 skills_install_test.go，
// 2026-09 代码普查 A2-1 迁出 skills 簇时留在本包（act/conventions/health 等多处
// 消费）；internal/cliskills/helpers_test.go 有同实现的姊妹副本——测试助手无法
// 跨包共享，两处注释互指防漂移。

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

// seedTaskState 经 SaveTaskState 写真实 TaskState（生产路径：SanitizeRef 折叠的
// 文件名、签名、MarshalIndent 格式），fixture 落在 forge 自己放它的确切位置。
// 任务状态机的链式构造（AssignTo→Claim→…）经 mutate 回调表达——各 save* 助手
// 只声明目标形态，写入路径单一（2026-09 普查 P3-5：曾 7 份近重复）。
func seedTaskState(t *testing.T, root, ref string, mutate func(*taskpipeline.TaskState)) {
	t.Helper()
	s := &taskpipeline.TaskState{TaskRef: ref, Branch: ref, Source: `explicit`}
	if mutate != nil {
		mutate(s)
	}
	if err := taskpipeline.SaveTaskState(root, s); err != nil {
		t.Fatal(err)
	}
}
