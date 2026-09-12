package hookdispatch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// hook_batch_test.go — W0.2 batch 单入口分派的名册收集与合并发射契约。

// TestBatchHookNames：名册现查（档位过滤后的 spec 单一来源）——matcher 精确
// 匹配、顺序保持、空 matcher 聚合全部。
func TestBatchHookNames(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "standard")
	got := batchHookNames("PreToolUse", "Bash")
	want := []string{"bash-guard", "hazard-guard", "gate-cmd-form", "skill-trigger"}
	if len(got) != len(want) {
		t.Fatalf("PreToolUse/Bash 组应含 %v，got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序/成员不符: got %v", got)
		}
	}
	all := batchHookNames("PreToolUse", "")
	if len(all) != len(got)+6 { // Write|Edit 组 6 条
		t.Errorf("空 matcher 应聚合全部（%d 条），got %d", len(got)+6, len(all))
	}
	if none := batchHookNames("NoSuchEvent", ""); len(none) != 0 {
		t.Errorf("未知事件应空名册，got %v", none)
	}
}

// TestRunHookBatch_MergesSessionStartContext：SessionStart 组的 allow 输出合并
// 为一次 claude 注入（additionalContext 拼接），而不是逐 hook 多文档流。
func TestRunHookBatch_MergesSessionStartContext(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "standard")
	oldEvent, oldMatcher := batchEvent, batchMatcher
	batchEvent, batchMatcher = "SessionStart", ""
	defer func() { batchEvent, batchMatcher = oldEvent, oldMatcher }()

	payload := `{"hook_event_name":"SessionStart","session_id":"bt"}`
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	if _, err := w.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	var out string
	var hookErr error
	stdout := captureStdout(t, func() {
		hookErr = runHookBatch(&cobra.Command{}, nil)
	})
	out = stdout
	t.Logf("captured out (%d bytes): %s", len(out), out)
	if hookErr != nil {
		t.Fatalf("batch 不应报错: %v\nout: %s", hookErr, out)
	}
	if !strings.Contains(out, `"SessionStart"`) || !strings.Contains(out, "additionalContext") {
		t.Fatalf("应合并发射为一次 claude 注入，got:\n%s", out)
	}
	if strings.Count(out, "hookSpecificOutput") != 1 {
		t.Errorf("合并后应只有一个 hookSpecificOutput 文档，got:\n%s", out)
	}
}

// TestRunHookBatch_ForwardsBlock：组内 hook 阻断时原样透传 *HookBlockError
// （外层 cobra 映射 exit 2），阻断语义与逐条拉起一致。
func TestRunHookBatch_ForwardsBlock(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "standard")
	// 项目 fixture：hazard-guard 的 HITL 链（fingerprint/confirmed）依赖项目
	// 上下文——非项目路径按设计静默放行。git init + .forge 目录即命中
	// findProjectRoot/registry。
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	oldEvent, oldMatcher := batchEvent, batchMatcher
	batchEvent, batchMatcher = "PreToolUse", "Bash"
	defer func() { batchEvent, batchMatcher = oldEvent, oldMatcher }()

	payload := `{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":"bt2","tool_input":{"command":"git push --force origin main"}}`
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	if _, err := w.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	hookErr := runHookBatch(&cobra.Command{}, nil)
	if hookErr == nil {
		for _, h := range batchHookNames("PreToolUse", "Bash") {
			oldErr := os.Stderr
			errR, errW, _ := os.Pipe()
			os.Stderr = errW
			out, rerr := runHookCaptured(&cobra.Command{}, h, []byte(payload))
			errW.Close()
			os.Stderr = oldErr
			eBuf := make([]byte, 300)
			en, _ := errR.Read(eBuf)
			t.Logf("debug hook=%q err=%v out=%.120q stderr=%.160q", h, rerr, out, string(eBuf[:en]))
		}
		t.Fatal("高危命令应被组内 hazard-guard 阻断")
	}
	var blockErr *HookBlockError
	if !errors.As(hookErr, &blockErr) {
		t.Fatalf("透传的错误应为 *HookBlockError，got %T: %v", hookErr, hookErr)
	}
	if !strings.Contains(blockErr.Reason, "git push") {
		t.Errorf("阻断错误应携带原因，got %q", blockErr.Reason)
	}
}
