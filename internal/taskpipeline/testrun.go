package taskpipeline

import (
	"os"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameTestRun is the checklog entry where forge actually runs the project test suite (a deterministic
// source). test-coverage-gate only checks whether changed files carry a paired test file (writing tests != running
// tests); test-capability-scan only reports runnable tests exist. This entry records that the suite actually ran
// and its real exit code — the strongest counter to the blind spot of an agent claiming tests passed without running them, because
// it is a result forge observed itself (unforgeable, unlike an agent self-report).
//
// CheckNameTestRun 是 forge 实跑项目测试套件的 checklog 条目（deterministic
// 来源）。test-coverage-gate 只检查改动文件是否带配对测试文件（写测试 ≠ 跑
// 测试）；test-capability-scan 只报告可运行测试存在。本条目记录套件真跑了
// 及其真实 exit code——对抗 agent 自述测试通过却没跑这一盲点的最强手段，因为
// 是 forge 自己观察到的结果（不可伪造，区别于 agent 自述）。
const CheckNameTestRun checklog.CheckName = "test-run"

// DetectTestCommand returns the most likely test command for the project's detected stack (e.g. go test ./...,
// cargo test, npm test), or an empty string when the stack/runner is unrecognized. It is a thin wrapper over the capability scanner
// manifest sniffing, so the caller (forge verify --run-tests) does not have to rewrite it.
//
// DetectTestCommand 返回项目所探测 stack 的最可能测试命令（如 go test ./...、
// cargo test、npm test），未识别 stack/runner 时返回空串。是 capability scanner
// manifest 嗅探的薄包装，让 caller（forge verify --run-tests）不必重写。
func DetectTestCommand(root string) string {
	_, cmd := detectStackAndCmd(root)
	return cmd
}

// RunTestCommand executes the command string under root, returning whether it succeeded (exit 0) and the merged
// stdout/stderr. Pure execution — it does not record checklog; the caller decides whether/where to record, keeping this function
// unit-testable without touching disk. The command string is split on whitespace: every test command produced by detectStackAndCmd
// is space-separated with no quoted args (go test ./..., cargo test, npx vitest run, etc.).
//
// RunTestCommand 在 root 下执行命令字符串，返回是否成功（exit 0）及合并的
// stdout/stderr。纯执行——不记 checklog；由 caller 决定是否/在哪记录，使本函数
// 保持可单测不触磁盘。命令字符串按空白切分：detectStackAndCmd 产出的每个 test
// command 都以空格分隔、无引号参数（go test ./...、cargo test、npx vitest run 等）。
func RunTestCommand(root, cmdStr string) (passed bool, output string) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return false, "empty command"
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.Dir = root
	c.Env = os.Environ()
	out, err := c.CombinedOutput()
	return err == nil, strings.TrimSpace(string(out))
}
