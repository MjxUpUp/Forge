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

// TestRunJSInfraNotice 钉死 npm/run.js 对「平台子包缺失」的失败可见性：包装器必须
// stderr 带修复办法并 exit 1，stdout 不得出现任何「成功/approve」标记——1.x 的
// hook fail-open（打 approve JSON 并 exit 0）随 hook 层删除一并移除。需要 node，
// 没有则跳过（run.js 本身就依赖 node）。
func TestRunJSInfraNotice(t *testing.T) {
	if runtime.GOOS == "windows" {
		// 平台子包在 Windows 上同样解析不到；但 Windows 没有 node 的偶发路径差异，
		// 统一跑一遍更有说服力（此前跳过是历史原因）。
		t.Log("windows: 照常执行")
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
	env := append(os.Environ(), "HOME="+dir, "NODE_PATH=", "TMPDIR="+dir)

	run := func(args ...string) (code int, stdout, stderr string) {
		cmd := exec.Command(nodeBin, append([]string{script}, args...)...)
		cmd.Env = env
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

	// 缺失子包：exit 1、stderr 给出修复办法、stdout 不得有成功标记。
	code, out, errOut := run("--version")
	if code != 1 {
		t.Fatalf("缺平台子包应 exit 1，got %d（stderr=%q）", code, errOut)
	}
	for _, want := range []string{"is not installed", "npm i -g @agent_forge/forge --include=optional"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr 应含 %q，got %q", want, errOut)
		}
	}
	if strings.Contains(out, "approve") || strings.Contains(out, "decision") {
		t.Errorf("stdout 不得出现 1.x 的 approve 契约，got %q", out)
	}

	// 重复调用：每次都是同样的响亮失败，不做「同因静默」去重。
	code2, out2, errOut2 := run("task", "report")
	if code2 != 1 {
		t.Fatalf("重复调用也应 exit 1，got %d", code2)
	}
	if strings.TrimSpace(out2) != "" {
		t.Errorf("重复调用 stdout 应为空，got %q", out2)
	}
	if !strings.Contains(errOut2, "is not installed") {
		t.Errorf("重复调用 stderr 应仍带原因，got %q", errOut2)
	}
}
