package clitask

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// AcceptancePreflight 是 task start 时对验收判据的预跑结果。
// 失败不阻断 start：它的价值是让"判据写错"当场可见，而不是等任务做完。
type AcceptancePreflight struct {
	Total  int             `json:"total"`
	Passed int             `json:"passed"`
	Fails  []PreflightFail `json:"fails,omitempty"`
}

type PreflightFail struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	Output  string `json:"output"` // 截断到 300 字节
}

const (
	// preflightTimeout 是单条判据的预跑上限。预跑只服务于「判据当场可见」，
	// 不能把 task start 挂到验收套件（go test ./... 等）跑完为止。
	preflightTimeout = 15 * time.Second
	// preflightOutputLimit 是失败输出在摘要里保留的字节数——够看清第一处报错，
	// 又不至于把 start 输出刷屏。
	preflightOutputLimit = 300
)

// RunAcceptancePreflight 逐条执行 state.Acceptance 的 Run 字段。
// 每条最多 15s；命令按 strings.Fields 切分后直接 exec（与 verify-acceptance 同语义，不经 shell）。
// 不返回 error：预跑从不阻止 start；单条执行失败计入 Fails。
func RunAcceptancePreflight(root string, crits []tasktypes.AcceptanceCriterion) AcceptancePreflight {
	pre := AcceptancePreflight{}
	for _, c := range crits {
		run := strings.TrimSpace(c.Run)
		if run == "" {
			// file-* 断言等无 Run 的条目没有可执行命令——不计入 Total，
			// 否则「N/M 通过」会因为不存在的命令永远凑不满。
			continue
		}
		pre.Total++
		code, out := runPreflightCommand(root, run)
		if code == 0 {
			pre.Passed++
			continue
		}
		pre.Fails = append(pre.Fails, PreflightFail{
			Command: run,
			Exit:    code,
			Output:  truncatePreflightOutput(out),
		})
	}
	return pre
}

// runPreflightCommand 在 root 下执行一条判据，返回退出码与合并输出。
// 切分/执行语义与 taskpipeline.RunTestCommandCode 一致：strings.Fields + 直接
// exec（不经 shell），因此引号、管道、&& 都不生效；spawn 失败与超时记 -1。
func runPreflightCommand(root, run string) (int, string) {
	parts := strings.Fields(run)
	if len(parts) == 0 {
		return -1, "empty command"
	}
	ctx, cancel := context.WithTimeout(context.Background(), preflightTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, parts[0], parts[1:]...)
	c.Dir = root
	c.Env = os.Environ()
	out, err := c.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return -1, "超时"
	}
	if err == nil {
		return 0, string(out)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}
	// spawn 失败（命令不存在、无执行权限等）时 CombinedOutput 通常为空；
	// 回填 err 让「判据写错」当场可读，而不是只留一个 exit -1。
	if len(out) == 0 {
		return -1, err.Error()
	}
	return -1, string(out)
}

// truncatePreflightOutput 把预跑输出压成单行摘要：按字节截断到 300（不切断
// UTF-8 rune），再把换行替换成空格。只在失败条目上调用。
func truncatePreflightOutput(out string) string {
	if len(out) > preflightOutputLimit {
		out = out[:preflightOutputLimit]
		for len(out) > 0 && !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
	}
	out = strings.ReplaceAll(out, "\r\n", " ")
	out = strings.ReplaceAll(out, "\n", " ")
	out = strings.ReplaceAll(out, "\r", " ")
	return strings.TrimSpace(out)
}
