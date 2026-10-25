package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunJSInfraNotice 钉死 npm/run.js 对「平台子包缺失」这类包装器侧基础设施失败的
// 可见性（docs/plans/hook-silent-failure-visibility-2026-10.md）：hook 调用同一宿主
// 进程同因首次 exit 1 + 带修复办法的 stderr，之后静默 exit 0；stdout 的 approve 契约
// 不变；非 hook 调用每次都有 stderr 提示、退出码保持 0；FORGE_HOOK_INFRA_DEDUP=0
// 恢复逐次 exit 1。需要 node——没有则跳过（run.js 本身就依赖 node）。
func TestRunJSInfraNotice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("去重状态键用 ppid，Windows 不在此测范围")
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node 不可用")
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "npm", "run.js"))
	if err != nil {
		t.Fatalf("读 npm/run.js: %v", err)
	}
	// 拷到临时目录再跑：其上没有 node_modules，平台子包必然解析不到。
	dir := t.TempDir()
	script := filepath.Join(dir, "run.js")
	if err := os.WriteFile(script, src, 0o755); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	env := func(extra ...string) []string {
		return append(append(os.Environ(),
			"HOME="+dir, "NODE_PATH=", "TMPDIR="+state, "FORGE_HOOK_INFRA_DEDUP="), extra...)
	}
	run := func(extraEnv []string, args ...string) (code int, stdout, stderr string) {
		cmd := exec.Command(nodeBin, append([]string{script}, args...)...)
		cmd.Env = env(extraEnv...)
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		err := cmd.Run()
		var ee *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &ee):
			code = ee.ExitCode()
		default:
			t.Fatalf("运行 run.js: %v", err)
		}
		return code, so.String(), se.String()
	}
	const approve = `{"decision":"approve"}`

	// hook 调用：首次 exit 1 + 修复办法。
	code, out, errOut := run(nil, "hook", "batch")
	if code != 1 {
		t.Fatalf("hook 调用首次应 exit 1，got %d（stderr=%q）", code, errOut)
	}
	for _, want := range []string{"subpackage-missing", "不阻断", "npm i -g @agent_forge/forge --include=optional", "FORGE_HOOK_INFRA_DEDUP=0"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("首次 stderr 应含 %q，got %q", want, errOut)
		}
	}
	if !strings.Contains(out, approve) {
		t.Errorf("stdout 的 approve 契约不变，got %q", out)
	}

	// 同一父进程（本测试进程）再次：静默 exit 0，仍打印 approve。
	code, out, errOut = run(nil, "hook", "batch")
	if code != 0 || strings.TrimSpace(errOut) != "" || !strings.Contains(out, approve) {
		t.Errorf("同因重复应静默 exit 0 + approve，got code=%d stdout=%q stderr=%q", code, out, errOut)
	}

	// 逃生口：去重关闭 → 逐次 exit 1。
	code, _, errOut = run([]string{"FORGE_HOOK_INFRA_DEDUP=0"}, "hook", "batch")
	if code != 1 || !strings.Contains(errOut, "subpackage-missing") {
		t.Errorf("去重关闭应逐次 exit 1，got code=%d stderr=%q", code, errOut)
	}

	// 非 hook 调用（交互使用）：每次都有 stderr 提示，退出码保持 0。
	for i := 0; i < 2; i++ {
		code, _, errOut = run(nil, "--version")
		if code != 0 || !strings.Contains(errOut, "subpackage-missing") {
			t.Errorf("非 hook 调用第 %d 次应 exit 0 且 stderr 有提示，got code=%d stderr=%q", i+1, code, errOut)
		}
	}
}
