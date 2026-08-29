package taskpipeline

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameTestRun 是 forge 实跑项目测试套件的 checklog 条目（deterministic
// 来源）。test-coverage-gate 只检查改动文件是否带配对测试文件（写测试 ≠ 跑
// 测试）；test-capability-scan 只报告可运行测试存在。本条目记录套件真跑了
// 及其真实 exit code——对抗 agent 自述测试通过却没跑这一盲点的最强手段，因为
// 是 forge 自己观察到的结果（不可伪造，区别于 agent 自述）。
const CheckNameTestRun checklog.CheckName = "test-run"

// DetectTestCommand returns the most likely test command for the project's
// detected stack (e.g. go test ./..., cargo test, npm test), or an empty string
// when the stack/runner is unrecognized.
//
// DetectTestCommand 返回项目所探测 stack 的最可能测试命令（如 go test ./...、
// cargo test、npm test），未识别 stack/runner 时返回空串。是 capability scanner
// manifest 嗅探的薄包装，让 caller（forge verify --run-tests）不必重写。
func DetectTestCommand(root string) string {
	_, cmd := detectStackAndCmd(root)
	return cmd
}

// RunTestCommand 在 root 下执行命令字符串，返回是否成功（exit 0）及合并的
// stdout/stderr。纯执行——不记 checklog；由 caller 决定是否/在哪记录，使本函数
// 保持可单测不触磁盘。命令字符串按空白切分：detectStackAndCmd 产出的每个 test
// command 都以空格分隔、无引号参数（go test ./...、cargo test、npx vitest run 等）。
//
// testRunTimeout 限定单次套件执行时长。嗅探出的命令含 watch 模式默认（npm test →
// vitest watch）；无上限的运行会把 forge verify 及其上的 hook 挂到终端关闭为止。
// 覆盖方式：FORGE_TEST_TIMEOUT（Go duration 字符串，如 30m）。
var testRunTimeout = 15 * time.Minute

func init() {
	if v := os.Getenv("FORGE_TEST_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			testRunTimeout = d
		}
	}
}

func RunTestCommand(root, cmdStr string) (passed bool, output string) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return false, "empty command"
	}
	ctx, cancel := context.WithTimeout(context.Background(), testRunTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, parts[0], parts[1:]...)
	c.Dir = root
	c.Env = os.Environ()
	out, err := c.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return false, strings.TrimSpace(string(out)) + "\n[forge] TIMEOUT: 测试命令超过 " + testRunTimeout.String() + " 被终止（FORGE_TEST_TIMEOUT 可调）"
	}
	return err == nil, strings.TrimSpace(string(out))
}
