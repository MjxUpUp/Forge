package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskGuardHookOffTreeDenies 钉住错树守护在 guard 脚本层的行为
// （docs/design/task-tree-guard.md §4/§7）：FORGE_TASK_OFFTREE 非空时源码写入必须
// fail-closed 并指出三条出路；测试文件豁免优先于该阻断（TDD 不受影响）。
func TestTaskGuardHookOffTreeDenies(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "task-guard.sh")
	if err := os.WriteFile(script, []byte(TaskGuardHook), 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(env map[string]string) (string, int) {
		t.Helper()
		cmd := exec.Command("bash", script)
		cmd.Dir = dir
		base := []string{
			"FORGE_SESSION_ID=tree-test",
			"FORGE_DATA_DIR=" + filepath.Join(dir, "data"),
			"FORGE_TASK_REF=feat/tg",
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + dir,
		}
		for k, v := range env {
			base = append(base, k+"="+v)
		}
		cmd.Env = base
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("运行 guard 脚本失败: %v\n%s", err, out)
			}
			code = exitErr.ExitCode()
		}
		return string(out), code
	}

	out, code := run(map[string]string{
		"FORGE_FILE_PATH":     "internal/cli/review.go",
		"FORGE_TASK_OFFTREE":  filepath.Join(dir, "elsewhere"),
		"FORGE_TASK_WORKTREE": filepath.Join(dir, "elsewhere"),
	})
	if code != 1 {
		t.Fatalf("错树源码写入必须 exit 1，got %d\n%s", code, out)
	}
	for _, want := range []string{"锚在另一棵树", "forge worktree use", "FORGE_TASK_TREE_GUARD=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("拒绝信息缺少 %q：\n%s", want, out)
		}
	}

	// 测试文件豁免先于错树判定：TDD 在错树上仍可写测试。
	tout, tcode := run(map[string]string{
		"FORGE_FILE_PATH":    "internal/cli/review_test.go",
		"FORGE_TASK_OFFTREE": filepath.Join(dir, "elsewhere"),
	})
	if tcode != 0 {
		t.Fatalf("测试文件必须豁免，got exit=%d\n%s", tcode, tout)
	}

	// 未注入 = 当前树就是任务的树（或无任务）：不得出现错树阻断文案。
	nout, ncode := run(map[string]string{"FORGE_FILE_PATH": "internal/cli/review.go"})
	if ncode != 0 {
		t.Fatalf("非错树场景不得阻断，got exit=%d\n%s", ncode, nout)
	}
	if strings.Contains(nout, "锚在另一棵树") {
		t.Fatalf("非错树场景不得出现错树文案：\n%s", nout)
	}
}
