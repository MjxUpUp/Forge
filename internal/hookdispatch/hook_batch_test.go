package hookdispatch

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// hook_batch_test.go — W0.2 batch 单入口分派的名册收集与合并发射契约。

// TestBatchHookNames：名册现查（档位过滤后的 spec 单一来源）——matcher 精确
// 匹配、顺序保持、空 matcher 聚合全部。
func TestBatchHookNames(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "standard")
	got := batchHookNames("PreToolUse", "Bash")
	// task-drift 追加于 2026-09-18（escape-hatch-hardening P0-B——git 边界
	// 动词 × 任务分支漂移的 choke-point advisory,见 hook_freeze_ratchet_test
	// 的 bump 注释）。
	want := []string{"bash-guard", "hazard-guard", "gate-cmd-form", "skill-trigger", "task-drift"}
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

// TestRunHookBatch_BlockStdoutMatchesSingleHook：阻断时 batch 的 stdout 必须与逐条拉起
// 同一 hook 的 stdout 一致（decision:block JSON）。宿主（forge-dsh 插件等）只从 stdout 的
// JSON decision 字段读阻断——exit code 不是契约（cobra 的内部错误也是非零）。batch 过去在
// 阻断时直接 return runErr，丢掉了内层 hook 写进捕获管道的 JSON，stdout 为空，插件无法读到
// 阻断原因，所以无法切到 batch。
//
// TestRunHookBatch_BlockStdoutMatchesSingleHook: on a block, batch's stdout must equal the
// single-hook stdout for the same payload (decision:block JSON). Hosts read the block only
// from the stdout JSON decision field, never the exit code; batch used to return runErr
// before printing the captured JSON, leaving stdout empty.
func TestRunHookBatch_BlockStdoutMatchesSingleHook(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "standard")
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

	payload := `{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":"bt3","tool_input":{"command":"git push --force origin main"}}`

	// 单 hook 基线：同一 payload 逐条拉起 hazard-guard 的 stdout。
	single, singleErr := runHookCaptured(&cobra.Command{}, "hazard-guard", []byte(payload))
	var blockErr *HookBlockError
	if !errors.As(singleErr, &blockErr) {
		t.Fatalf("基线前提：hazard-guard 应阻断该命令，got err=%v out=%q", singleErr, single)
	}

	// batch：捕获真实 stdout。
	oldStdin, oldStdout := os.Stdin, os.Stdout
	inR, inW, _ := os.Pipe()
	if _, err := inW.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	inW.Close()
	outR, outW, _ := os.Pipe()
	os.Stdin, os.Stdout = inR, outW
	batchErr := runHookBatch(&cobra.Command{}, nil)
	os.Stdin, os.Stdout = oldStdin, oldStdout
	outW.Close()
	batchOut, _ := io.ReadAll(outR)

	if !errors.As(batchErr, &blockErr) {
		t.Fatalf("batch 应透传 *HookBlockError，got %T: %v", batchErr, batchErr)
	}
	if strings.TrimSpace(string(batchOut)) == "" {
		t.Fatal("batch 阻断时 stdout 为空——宿主读不到 decision:block JSON（本回归的症状）")
	}
	if strings.TrimSpace(string(batchOut)) != strings.TrimSpace(single) {
		t.Errorf("batch 阻断 stdout 应与单 hook 一致\nbatch:  %s\nsingle: %s", batchOut, single)
	}
	var doc struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(batchOut, &doc); err != nil {
		t.Fatalf("阻断 stdout 应是单个合法 JSON 文档: %v\n%s", err, batchOut)
	}
	if doc.Decision != "block" || doc.Reason == "" {
		t.Errorf("JSON 应含 decision:block 与非空 reason，got %+v", doc)
	}
}

// largePipeSize 远超任一平台的 OS 管道缓冲（macOS 64KB、Linux 64KB、Windows
// 约 4KB）——同步「先写后跑 / 先跑后读」必在此尺寸上永久阻塞。
//
// largePipeSize is far above every platform's OS pipe buffer, so a
// synchronous write-then-run or run-then-read deadlocks at this size.
const largePipeSize = 256 << 10

// withinTimeout 在 d 内跑完 fn，否则判死锁失败（不让挂死的用例拖垮整包超时）。
//
// withinTimeout runs fn and fails the test as a deadlock if it exceeds d.
func withinTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("未在 %v 内完成——管道死锁回归（payload/输出超 OS 管道缓冲）", d)
	}
}

// TestRunHookCaptured_LargePipes：payload 与 hook 输出任一超 OS 管道缓冲都必须
// 完成且数据完整（v1.57.0–v1.77.0 batch 在 >64KB Write/Edit payload 上挂死的
// 回归）。runHookFn 换桩以精确控制 stdin 消费与 stdout 产出尺寸；hook 不读
// stdin 时写 goroutine 不得泄漏挂死；block 错误照常透传且已产出的 stdout 不丢。
func TestRunHookCaptured_LargePipes(t *testing.T) {
	big := strings.Repeat("x", largePipeSize)
	cases := []struct {
		name      string
		payload   string
		readStdin bool
		emit      string
		blockErr  bool
	}{
		{name: "large payload, small output", payload: big, readStdin: true, emit: "ok"},
		{name: "large payload, stdin ignored", payload: big, readStdin: false, emit: "ok"},
		{name: "small payload, large output", payload: `{"a":1}`, readStdin: true, emit: big},
		{name: "large payload, large output", payload: big, readStdin: true, emit: big},
		{name: "large payload, block forwarded", payload: big, readStdin: true, emit: "partial", blockErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotIn int
			orig := runHookFn
			runHookFn = func(_ *cobra.Command, args []string) error {
				if tc.readStdin {
					b, err := io.ReadAll(os.Stdin)
					if err != nil {
						return err
					}
					gotIn = len(b)
				}
				if _, err := io.WriteString(os.Stdout, tc.emit); err != nil {
					return err
				}
				if tc.blockErr {
					return &HookBlockError{Reason: "blocked " + args[0]}
				}
				return nil
			}
			t.Cleanup(func() { runHookFn = orig })

			oldIn, oldOut := os.Stdin, os.Stdout
			var out string
			var err error
			withinTimeout(t, 10*time.Second, func() {
				out, err = runHookCaptured(&cobra.Command{}, "stub-hook", []byte(tc.payload))
			})
			if os.Stdin != oldIn || os.Stdout != oldOut {
				t.Fatal("返回后 os.Stdin/os.Stdout 必须复原")
			}
			if out != tc.emit {
				t.Fatalf("捕获输出不完整: got %d bytes, want %d", len(out), len(tc.emit))
			}
			if tc.readStdin && gotIn != len(tc.payload) {
				t.Fatalf("hook 收到的 stdin 不完整: got %d bytes, want %d", gotIn, len(tc.payload))
			}
			var be *HookBlockError
			if tc.blockErr {
				if !errors.As(err, &be) || be.Reason != "blocked stub-hook" {
					t.Fatalf("block 应原样透传 *HookBlockError，got %T: %v", err, err)
				}
			} else if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
		})
	}
}

// TestRunHookBatch_LargeWriteEditPayloadNonForgeDir：事故现场端到端复现——非
// forge 目录里 Write|Edit 组收到 >64KB payload（Write 大文件 content / Edit 的
// tool_response 携原文件），真实 RunHook 全组必须在时限内静默 allow（空输出、
// 无错误），不得挂死在管道写上。
func TestRunHookBatch_LargeWriteEditPayloadNonForgeDir(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "standard")
	t.Setenv("HOME", t.TempDir()) // 隔离开发者的用户级 forge 状态 / isolate user-level forge state
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	big := strings.Repeat("y", largePipeSize)
	file := filepath.Join(dir, "big.txt")
	mk := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	cases := []struct {
		name    string
		event   string
		payload string
	}{
		{"PreToolUse Write large content", "PreToolUse", mk(map[string]any{
			"hook_event_name": "PreToolUse", "tool_name": "Write", "session_id": "bt-large", "cwd": dir,
			"tool_input": map[string]any{"file_path": file, "content": big},
		})},
		{"PostToolUse Edit large tool_response", "PostToolUse", mk(map[string]any{
			"hook_event_name": "PostToolUse", "tool_name": "Edit", "session_id": "bt-large", "cwd": dir,
			"tool_input":    map[string]any{"file_path": file, "old_string": "a", "new_string": "b"},
			"tool_response": map[string]any{"filePath": file, "originalFile": big},
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(batchHookNames(tc.event, "Write|Edit")) == 0 {
				t.Fatalf("%s 的 Write|Edit 组不应为空", tc.event)
			}
			oldEvent, oldMatcher := batchEvent, batchMatcher
			batchEvent, batchMatcher = tc.event, "Write|Edit"
			t.Cleanup(func() { batchEvent, batchMatcher = oldEvent, oldMatcher })

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				_, _ = w.WriteString(tc.payload)
				_ = w.Close()
			}()
			oldStdin := os.Stdin
			os.Stdin = r
			t.Cleanup(func() { os.Stdin = oldStdin; _ = r.Close() })

			var out string
			var hookErr error
			withinTimeout(t, 30*time.Second, func() {
				out = captureStdout(t, func() {
					hookErr = runHookBatch(&cobra.Command{}, nil)
				})
			})
			if hookErr != nil {
				t.Fatalf("非 forge 目录应静默 allow，got err: %v", hookErr)
			}
			if out != "" {
				t.Fatalf("非 forge 目录应静默 allow（空输出），got %q", out)
			}
		})
	}
}
