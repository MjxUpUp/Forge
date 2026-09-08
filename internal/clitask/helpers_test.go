package clitask

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// helpers_test.go —— clitask 测试共享助手。captureStdout/captureStderr 与
// internal/cli/helpers_test.go 同实现、seedTaskState 与 internal/cli/helpers_test.go
// 同实现——测试助手无法跨包共享，注释互指防漂移（2026-09 普查 A2-3 迁移）。

// captureStdout 临时重定向 os.Stdout 捕获命令输出（输出层单测）。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	// 并发排水必须先于 fn 启动：Windows os.Pipe 缓冲约 4KB，捕获输出超限时
	// 「先跑后读」会让 fn 的 stdout 写永久阻塞（2026-09 hazard 阻断信封 ~4.7KB
	// 实测挂死）。三包同实现拷贝（普查 A2-2）须同款修复。
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

// captureStderr 临时重定向 os.Stderr 捕获告警输出（与 cli 版同款 panic 安全收尾）。
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

// seedTaskState 经 SaveTaskState 写真实 TaskState（mutate 回调表达状态机链式
// 构造）——与 internal/cli/helpers_test.go 同实现，注释互指防漂移。
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
