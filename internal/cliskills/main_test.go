package cliskills

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// main_test.go —— cliskills 测试的子进程 harness（姊妹副本）。skills 簇 2026-09
// 从 internal/cli 整体迁出时，3 个子进程级测试（decide/closure/mutex）随迁——
// 它们消费本包的 flag 全局变量与台账格式，属本包单测；runForge/环境隔离与
// internal/cli TestMain 同构（测试 harness 无法跨包共享，两处注释互指防漂移）。

func TestMain(m *testing.M) {
	exeName := "forge"
	if runtime.GOOS == "windows" {
		exeName = "forge.exe"
	}
	tmpDir, err := os.MkdirTemp("", "forge-cliskills-test-*")
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

// runForge 在 dir 下以子进程跑编译好的 forge 二进制（与 internal/cli 同名助手
// 同实现——注释互指防漂移）。
var forgeExe string

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

// runForgeStreams 与 runForge 的分流版（stdout/stderr 分开捕获；与
// internal/cli/task_nongit_test.go 的同名助手同实现——注释互指防漂移）。
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
