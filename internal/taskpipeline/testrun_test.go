package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectTestCommand 锁定测试命令探测：go.mod→go test，无可识别 manifest→""（caller
// 据此跳过，不发空命令）。detectStackAndCmd 的展开，保证 forge verify --run-tests 的
// 入口探测可回归。
func TestDetectTestCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := DetectTestCommand(dir); got != "go test ./..." {
		t.Errorf("DetectTestCommand(go.mod) = %q, want %q", got, "go test ./...")
	}

	empty := t.TempDir()
	if got := DetectTestCommand(empty); got != "" {
		t.Errorf("DetectTestCommand(no manifest) = %q, want empty", got)
	}
}

// TestRunTestCommand_ExitCode 验证真实退出码捕获：exit 0→passed=true，非 0→false。
// 用 go（Go 模块测试环境必有）的子命令做确定性退出码，不跑真实测试套件（快）。
func TestRunTestCommand_ExitCode(t *testing.T) {
	dir := t.TempDir()

	// go version exits 0
	passed, _ := RunTestCommand(dir, "go version")
	if !passed {
		t.Error(`"go version" should pass (exit 0)`)
	}

	// unknown subcommand exits non-zero
	passed2, out2 := RunTestCommand(dir, "go forge-nope-nope")
	if passed2 {
		t.Error(`unknown go subcommand should fail (non-zero exit)`)
	}
	if out2 == "" {
		t.Error("expected non-empty stderr on failed command")
	}

	// empty command string is a safe no-op, not a crash
	passed3, _ := RunTestCommand(dir, "")
	if passed3 {
		t.Error(`empty command should not pass`)
	}
}
