package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/hookdispatch"
) // —— 以下为 internal/hookdispatch 迁移后留守 cli 测试的姊妹副本（测试助手无法
// 跨包共享，注释互指防漂移；2026-09 普查 A2-2）。——
// captureOutput swaps os.Stdout/os.Stderr, runs fn, and returns what each captured.
func captureOutput(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	err = fn()

	wOut.Close()
	wErr.Close()
	outBytes, _ := io.ReadAll(rOut)
	errBytes, _ := io.ReadAll(rErr)
	return string(outBytes), string(errBytes), err
}

// convWrite writes rel under root (parent dirs auto-made).
func convWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// runTaskGuardHookOnce 在 newTaskGuardProject 已 chdir 的项目里经 runHook 真跑一次
// task-guard 脚本，按给定 agent 声明与 session id，返回捕获的输出与错误。temp 项目
// 非 git 仓库，脚本的分支解析得 ""，落到无任务 warn/promote 分支——与 main/master
// 上走的同一条路径（项目根由 findProjectRoot 从 cwd 解析，不经参数传递）。
func runTaskGuardHookOnce(t *testing.T, agentDecl, sessionID string) (stdout, stderr string, err error) {
	t.Helper()
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","tool_name":"Write","session_id":%q,%s"tool_input":{"file_path":"src/main.go","content":"package main"}}`, sessionID, agentDecl)
	oldStdin := os.Stdin
	tmpStdin, ierr := os.CreateTemp("", "hook-stdin-*.json")
	if ierr != nil {
		t.Fatal(ierr)
	}
	if _, ierr = tmpStdin.WriteString(payload); ierr != nil {
		t.Fatal(ierr)
	}
	if _, ierr = tmpStdin.Seek(0, 0); ierr != nil {
		t.Fatal(ierr)
	}
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()
	return captureOutput(t, func() error {
		// 最小 cobra root：Go 内 hook 分派读 cmd.Root().Version，nil 会空指针
		// （同 runHookWithStdin 的注释）。
		return hookdispatch.RunHook(&cobra.Command{}, []string{"task-guard"})
	})
}

// newTaskGuardProject 构建本文件 E2E 共用的隔离 fixture：全新 forge 项目 +
// 隔离 DataHome + 清空的 agent env（归因只受被测 payload 驱动）。返回项目根
// （测试期间的 cwd），供调用方推导 FORGE_DATA_DIR 解析出的路径。
func newTaskGuardProject(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("FORGE_HOOK_AGENT", "")
	t.Setenv("FORGE_AGENT", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644); err != nil {
		t.Fatal(err)
	}
	originalWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	return root
}
