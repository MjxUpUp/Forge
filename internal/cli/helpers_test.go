package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
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

// runForgeStreams 与 runForge 的分流版（stdout/stderr 分开捕获）——与
// internal/clitask/task_nongit_test.go 的同名助手同实现（迁移自该文件：原定义
// 随 task 簇走 clitask，health_test 等 cli 侧测试留此副本；注释互指防漂移）。
func runForgeStreams(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(forgeExe, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return out.String(), errb.String(), exitErr.ExitCode()
		}
		return out.String(), errb.String() + err.Error(), -1
	}
	return out.String(), errb.String(), 0
}

// taskStatePath 推导 task state 文件落点（DataDir/tasks/<sanitized>.json）——
// 与 internal/clitask/task_abort_test.go 的同名助手同实现（迁移自该文件；测试
// 助手无法跨包共享，注释互指防漂移）。
func taskStatePath(dir, taskRef string) string {
	return filepath.Join(forgedata.DataDirFor(dir), "tasks", taskcontext.SanitizeRef(taskRef)+".json")
}
