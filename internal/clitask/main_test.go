package clitask

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// main_test.go —— clitask 测试的子进程 harness（姊妹副本）。task 命令簇 2026-09
// 从 internal/cli 整体迁出时随迁——子进程级 task 命令 E2E 与本包的 flag 全局
// 变量/台账格式强耦合，属本包单测。与 internal/cli TestMain 同构（测试 harness
// 无法跨包共享，两处注释互指防漂移）。

func TestMain(m *testing.M) {
	exeName := "forge"
	if runtime.GOOS == "windows" {
		exeName = "forge.exe"
	}
	tmpDir, err := os.MkdirTemp("", "forge-clitask-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	forgeExe = filepath.Join(tmpDir, exeName)

	cmd := exec.Command("go", "build", "-o", forgeExe, "../../cmd/forge")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build forge binary: %v\n%s\n", err, output)
		os.Exit(1)
	}

	// 与 cli TestMain 同款三重隔离：全局数据根、Claude plugin 检测、用户 HOME
	// （子进程继承本进程 env——不隔离则测试写真实 ~/.forge 与 agent 配置）。
	os.Setenv("FORGE_DATA_HOME", tmpDir)
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)
	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create isolated home: %v\n", err)
		os.Exit(1)
	}
	if runtime.GOOS == "windows" {
		os.Setenv("USERPROFILE", homeDir)
	} else {
		os.Setenv("HOME", homeDir)
	}

	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

var forgeExe string

// runForge 在 dir 下以子进程跑编译好的 forge 二进制（与 internal/cli 同名助手
// 同实现——注释互指防漂移）。
func runForge(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(forgeExe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, "", exitErr.ExitCode()
		}
		return output, err.Error(), -1
	}
	return output, "", 0
}

// runGit is a test helper to run git commands (sister of internal/cli cli_test.go).
//
// runGit 测试助手：跑 git 命令（与 internal/cli/cli_test.go 同名助手同实现——
// 注释互指防漂移）。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, string(out), err)
	}
}

// TestCommitBestEffort_NoopFallback 钉死接缝兜底契约（对齐 cliskills.VersionFn
// 先例）：包级默认值是可调用的 no-op 而非 nil——clitask 进程内单测不经 cli
// 注册器、无注入，若默认值是裸 nil，best-effort 调用即 panic。本测试在默认态
// 直接调用（不设桩）：不 panic 即「默认兜底」契约成立；随后验证注册器路径
// 可整体替换（注入的钩子会被调用到）。
func TestCommitBestEffort_NoopFallback(t *testing.T) {
	if CommitBestEffort == nil {
		t.Fatal("CommitBestEffort 包级默认值不得是 nil（进程内单测无注册器，裸 nil 即 panic）")
	}
	CommitBestEffort("default no-op safe")

	called := ""
	defer func(orig func(string)) { CommitBestEffort = orig }(CommitBestEffort)
	CommitBestEffort = func(reason string) { called = reason }
	CommitBestEffort("injected")
	if called != "injected" {
		t.Fatalf("注入的钩子应被调用, got %q", called)
	}
}
