package hookdispatch

// hook_idempotency_test.go —— dispatch 短窗幂等守卫(P0-C)的契约:窗口解析
// 旋钮语义(默认/禁用/钳制/非法回落);窗口内同键跳过、异键放行;root 空
// (global hook)不守;窗口禁用时永真放行。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestHookDedupWindowParsing(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", 3 * time.Second},
		{"0", 0},
		{"7", 7 * time.Second},
		{"99", 10 * time.Second}, // 钳制上限
		{"-5", 3 * time.Second},  // 负数回落默认(旋钮不得弄断 hook)
		{"abc", 3 * time.Second}, // 非法回落默认
		{" 2 ", 2 * time.Second}, // 容忍空白
	}
	for _, c := range cases {
		t.Setenv(forgeHookDedupEnv, c.env)
		if got := hookDedupWindow(); got != c.want {
			t.Errorf("FORGE_HOOK_DEDUP_WINDOW=%q → %v, want %v", c.env, got, c.want)
		}
	}
}

func TestSkipDuplicateHookRun_WindowSemantics(t *testing.T) {
	root := trackTestProject(t)
	t.Setenv(forgeHookDedupEnv, "2")
	raw, _ := json.Marshal(map[string]string{"command": "git commit -m x"})
	in := HookInput{HookEventName: "PreToolUse", SessionID: "sess-dedup", ToolName: "Bash", ToolInput: raw}

	// 正例用白名单成员(failure-track)——阻断型钩子已永不入守卫(语义反转
	// 修复),不得再作去重正例(只读复检必改项 1)。
	if skipDuplicateHookRun(root, "failure-track", in) {
		t.Fatal("first invocation must never skip")
	}
	if !skipDuplicateHookRun(root, "failure-track", in) {
		t.Fatal("identical invocation inside the window must skip")
	}

	// 异键放行:name / tool / input / session 任一不同即新键(异名例同为白名单
	// 成员——task-guard 等阻断型本就永不跳过,作异名例会让断言空洞)。
	in2 := in
	in2.ToolName = "Write"
	if skipDuplicateHookRun(root, "failure-track", in2) {
		t.Error("different tool must not skip")
	}
	if skipDuplicateHookRun(root, "tool-track", in) {
		t.Error("different hook name must not skip")
	}
	in3 := in
	in3.ToolInput, _ = json.Marshal(map[string]string{"command": "go build ./..."})
	if skipDuplicateHookRun(root, "failure-track", in3) {
		t.Error("different tool input must not skip")
	}

	// 禁用旋钮:永真放行。
	t.Setenv(forgeHookDedupEnv, "0")
	if skipDuplicateHookRun(root, "failure-track", in) {
		t.Error("window=0 must disable the guard entirely")
	}

	// global hook(root="")不守。
	t.Setenv(forgeHookDedupEnv, "2")
	if skipDuplicateHookRun("", "failure-track", in) {
		t.Error("empty root (global hook) must bypass the guard")
	}
	_ = skipDuplicateHookRun("", "failure-track", in)
}

// TestRunHook_DuplicateInvocationSkippedSecond 端到端钉守卫的**目标表面**
// (dispatch):同 payload 连跑两次 RunHook,第二次必须静默(无 stdout、零新增
// checklog 行)——这是双通道(zcode 用户级+插件)重放被消除的实证形态。
func TestRunHook_DuplicateInvocationSkippedSecond(t *testing.T) {
	root := trackTestProject(t)
	t.Setenv(forgeHookDedupEnv, "2")
	// RunHook 经 projectroot.Find() 解析项目根——walk-up 兜底认项目级 .forge/
	// 目录标记(注册表成员资格之外的遗留路径),bare temp 目录会被判为非 forge
	// 项目而静默放行。
	_ = os.MkdirAll(filepath.Join(root, ".forge"), 0o755)
	startTrackTask(t, root, "dedup/e2e", "feat/dedup")

	originalWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })

	runOnce := func() string {
		t.Helper()
		oldStdin := os.Stdin
		tmpStdin, err := os.CreateTemp("", "hook-stdin-*.json")
		if err != nil {
			t.Fatal(err)
		}
		payload := `{"session_id":"sess-dedup-e2e","hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"go build ./..."},"error":"go build failed: undefined: util.Foo"}`
		if _, err := tmpStdin.WriteString(payload); err != nil {
			t.Fatal(err)
		}
		_, _ = tmpStdin.Seek(0, 0)
		os.Stdin = tmpStdin
		defer func() {
			os.Stdin = oldStdin
			tmpStdin.Close()
			os.Remove(tmpStdin.Name())
		}()
		return captureStdout(t, func() {
			_ = RunHook(&cobra.Command{}, []string{"failure-track"})
		})
	}

	first := runOnce()
	if first == "" {
		t.Fatal("first invocation must emit (failure fingerprint → nudge)")
	}
	before := len(findTrackEntries(t, root, "tool-failure"))
	second := runOnce()
	if second != "" {
		t.Errorf("duplicate invocation inside window must be silent, got: %q", second)
	}
	if after := len(findTrackEntries(t, root, "tool-failure")); after != before {
		t.Errorf("duplicate invocation must not record a second row (%d → %d)", before, after)
	}
}

// TestObservationOnlyGuardScope 钉住守卫的执法安全边界(审查必改项 1):观测型
// 钩子窗口内去重;阻断型/执法型钩子(含现在 advisory、未来 ratchet BLOCK 的)
// 永不去重——skip=allow 对它们是语义反转(deny 后逐字重试必须继续被 deny)。
func TestObservationOnlyGuardScope(t *testing.T) {
	root := trackTestProject(t)
	t.Setenv(forgeHookDedupEnv, "2")
	raw, _ := json.Marshal(map[string]string{"command": "some cmd"})
	in := HookInput{HookEventName: "PreToolUse", SessionID: "sess-scope", ToolName: "Bash", ToolInput: raw}

	_ = skipDuplicateHookRun(root, "tool-track", in)
	if !skipDuplicateHookRun(root, "tool-track", in) {
		t.Error("observation hook (tool-track) must dedup inside window")
	}
	for _, blocking := range []string{"hazard-guard", "task-guard", "bash-guard", "read-before-edit", "gate-cmd-form", "file-sentinel", "task-drift", "freeze-guard"} {
		if skipDuplicateHookRun(root, blocking, in) {
			t.Errorf("blocking hook %q must NEVER be deduped (skip=allow is enforcement inversion)", blocking)
		}
	}
	// Stop 面执法钩子同样不入白名单(task-verify 带 P1 有界阻断/review-stop 阻断)。
	for _, stop := range []string{"task-verify", "review-stop"} {
		if skipDuplicateHookRun(root, stop, in) {
			t.Errorf("stop hook %q carries block semantics (P1 bounded block) and must not dedup", stop)
		}
	}
}
