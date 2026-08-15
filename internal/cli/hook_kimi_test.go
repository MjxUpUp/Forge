package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// kimi stdin payloads captured from kimi-code 0.31.0 (debug hook dump). Keep them
// verbatim — kimiNormalize exists because two of these shapes diverge from Claude's.
//
// 以下 kimi stdin payload 取自 kimi-code 0.31.0 实测（debug hook dump）。保持原样
// ——kimiNormalize 存在的理由正是其中两种形状与 Claude 不同。
const (
	kimiPreToolUsePayload = `{"hook_event_name":"PreToolUse","session_id":"session_68f60c4c-485d-4eea-906b-6633cbdc6a86","cwd":"C:/proj","tool_name":"Bash","tool_input":{"command":"echo hello"},"tool_call_id":"tool_ZKyWjqei13lIupOo6zm2DGwI"}`

	kimiPostToolUsePayload = `{"hook_event_name":"PostToolUse","session_id":"session_68f60c4c-485d-4eea-906b-6633cbdc6a86","cwd":"C:/proj","tool_name":"Bash","tool_input":{"command":"echo hello"},"tool_call_id":"tool_ZKyWjqei13lIupOo6zm2DGwI","tool_output":"hello\n"}`

	kimiUserPromptPayload = `{"hook_event_name":"UserPromptSubmit","session_id":"session_68f60c4c-485d-4eea-906b-6633cbdc6a86","cwd":"C:/proj","prompt":[{"type":"text","text":"用 Bash 工具运行 echo hello，然后结束"}]}`

	kimiSessionStartPayload = `{"hook_event_name":"SessionStart","session_id":"session_68f60c4c-485d-4eea-906b-6633cbdc6a86","cwd":"C:/proj","source":"startup"}`
)

func TestKimiNormalize_PreToolUse(t *testing.T) {
	var in HookInput
	kimiNormalize([]byte(kimiPreToolUsePayload), &in)
	if in.SessionID != "session_68f60c4c-485d-4eea-906b-6633cbdc6a86" {
		t.Errorf("SessionID = %q", in.SessionID)
	}
	if in.HookEventName != "PreToolUse" || in.ToolName != "Bash" {
		t.Errorf("event/tool = %q/%q", in.HookEventName, in.ToolName)
	}
	if !strings.Contains(string(in.ToolInput), `"command":"echo hello"`) {
		t.Errorf("ToolInput = %s", in.ToolInput)
	}
}

func TestKimiNormalize_PostToolUse_ToolOutput(t *testing.T) {
	var in HookInput
	kimiNormalize([]byte(kimiPostToolUsePayload), &in)
	// kimi 的 tool_output（纯字符串）包装成 {"output": "..."} 对象——纯字符串会让
	// skill-trigger 的对象解析静默失败，kimi 上所有 PostToolUse 触发条件全灭。
	var out map[string]any
	if err := json.Unmarshal(in.ToolOutput, &out); err != nil {
		t.Fatalf("ToolOutput 必须是可解析对象（包装产物），got %s: %v", in.ToolOutput, err)
	}
	if out["output"] != "hello\n" {
		t.Errorf("ToolOutput.output = %q, want %q", out["output"], "hello\n")
	}
}

func TestKimiNormalize_PostToolUse_ToolOutputEmbeddedJSON(t *testing.T) {
	// 字符串内容本身是对象的序列化 JSON（部分工具输出如此）→ 直接采用内嵌对象，
	// 保留 exit_code 等字段供 test_command_failed condition 使用。
	payload := `{"hook_event_name":"PostToolUse","session_id":"s1","cwd":"C:/proj","tool_name":"Bash","tool_input":{"command":"go test ./..."},"tool_output":"{\"exit_code\":1,\"stderr\":\"--- FAIL: TestX\"}"}`
	var in HookInput
	kimiNormalize([]byte(payload), &in)
	var out map[string]any
	if err := json.Unmarshal(in.ToolOutput, &out); err != nil {
		t.Fatalf("内嵌 JSON 字符串必须解包为对象，got %s: %v", in.ToolOutput, err)
	}
	if code, _ := out["exit_code"].(float64); code != 1 {
		t.Errorf("exit_code = %v, want 1", out["exit_code"])
	}
}

func TestKimiNormalize_PostToolUse_ToolOutputEdgeStates(t *testing.T) {
	// wrapKimiToolOutput 的三态边界（审查 L3 补测）：null 字面量会经
	// json.Unmarshal(null, &s) 得空串并包装成 {"output":""}（良性变形，钉住防回退）；
	// 字符串以 { 开头但非合法 JSON 走包装回退；数组序列化字符串同样包装（matchKeywords
	// 只读 output 键，数组裸放反而取不到文本）。
	tests := []struct {
		name    string
		output  string // tool_output 的原始 JSON 值
		wantOut string // 期望包装后的 output 键值；"*" 表示断言可解析对象即可
	}{
		{"null literal wraps to empty output", `null`, ""},
		{"brace-prefixed invalid JSON falls back to wrap", `"{oops"`, "{oops"},
		{"array-JSON string wraps output key", `"[1,2]"`, "[1,2]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{"hook_event_name":"PostToolUse","session_id":"s1","cwd":"C:/proj","tool_name":"Bash","tool_input":{},"tool_output":` + tt.output + `}`
			var in HookInput
			kimiNormalize([]byte(payload), &in)
			var out map[string]any
			if err := json.Unmarshal(in.ToolOutput, &out); err != nil {
				t.Fatalf("ToolOutput 必须是可解析对象，got %s: %v", in.ToolOutput, err)
			}
			if out["output"] != tt.wantOut {
				t.Errorf("ToolOutput.output = %#v, want %#v", out["output"], tt.wantOut)
			}
		})
	}
}

func TestKimiNormalize_UserPromptSubmit_PromptArray(t *testing.T) {
	var in HookInput
	kimiNormalize([]byte(kimiUserPromptPayload), &in)
	// prompt 是 content-block 数组——必须展平为纯文本，否则 skill-trigger 的
	// coding_intent condition 拿不到 prompt。
	if in.Prompt != "用 Bash 工具运行 echo hello，然后结束" {
		t.Errorf("Prompt = %q", in.Prompt)
	}
	if in.HookEventName != "UserPromptSubmit" {
		t.Errorf("HookEventName = %q", in.HookEventName)
	}
}

func TestKimiNormalize_SessionStart(t *testing.T) {
	var in HookInput
	kimiNormalize([]byte(kimiSessionStartPayload), &in)
	if in.HookEventName != "SessionStart" || in.SessionID == "" {
		t.Errorf("event/session = %q/%q", in.HookEventName, in.SessionID)
	}
}

func TestKimiNormalize_EditPathAlias(t *testing.T) {
	// kimi 的 Edit/Write 用 tool_input.path（相对路径），无 file_path——必须别名过去，
	// 否则 read-before-edit 等基于路径的 hook fail-open（实测踩坑：Edit 拦截失效）。
	payload := `{"hook_event_name":"PreToolUse","session_id":"s1","cwd":"C:/proj","tool_name":"Edit","tool_input":{"path":"main.go","old_string":"a","new_string":"b"},"tool_call_id":"tool_x"}`
	var in HookInput
	kimiNormalize([]byte(payload), &in)
	var fields toolInputFields
	if err := json.Unmarshal(in.ToolInput, &fields); err != nil {
		t.Fatalf("ToolInput 不可解析: %v", err)
	}
	if fields.FilePath != "main.go" {
		t.Errorf("FilePath = %q, want %q（path→file_path 别名）", fields.FilePath, "main.go")
	}
}

func TestKimiNormalize_EmptyAndGarbage(t *testing.T) {
	var in HookInput
	kimiNormalize(nil, &in)
	kimiNormalize([]byte("not json"), &in) // warns on stderr, must not panic
	if in.SessionID != "" || in.HookEventName != "" {
		t.Errorf("garbage input must leave HookInput empty, got %+v", in)
	}
}

// captureOutput swaps os.Stdout/os.Stderr, runs fn, and returns what each captured.
func captureOutput(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	err = fn()

	wOut.Close()
	wErr.Close()
	outBytes, _ := io.ReadAll(rOut)
	errBytes, _ := io.ReadAll(rErr)
	return string(outBytes), string(errBytes), err
}

func TestEmitKimiOutput_Pass(t *testing.T) {
	stdout, stderr, err := captureOutput(t, func() error {
		return emitKimiOutput(true, "task 已接续：feat/xxx")
	})
	if err != nil {
		t.Fatalf("pass must return nil error (exit 0), got %v", err)
	}
	if !strings.Contains(stdout, "task 已接续：feat/xxx") {
		t.Errorf("advisory detail must go to stdout on the allow path, got %q", stdout)
	}
	if stderr != "" {
		t.Errorf("pass must not write stderr, got %q", stderr)
	}
}

func TestEmitKimiOutput_PassSilent(t *testing.T) {
	stdout, _, err := captureOutput(t, func() error {
		return emitKimiOutput(true, "")
	})
	if err != nil || stdout != "" {
		t.Errorf("silent pass must emit nothing, got stdout=%q err=%v", stdout, err)
	}
}

func TestEmitKimiOutput_Block(t *testing.T) {
	stdout, stderr, err := captureOutput(t, func() error {
		return emitKimiOutput(false, "BLOCKED: 未读即改")
	})
	var blockErr *HookBlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("block must return *HookBlockError (Execute maps it to exit 2), got %T %v", err, err)
	}
	if !strings.Contains(stderr, "BLOCKED: 未读即改") {
		t.Errorf("block reason must go to stderr (kimi shows it to the model), got %q", stderr)
	}
	if stdout != "" {
		t.Errorf("block must not write stdout, got %q", stdout)
	}
}

func TestEmitKimiOutput_BlockEmptyReason(t *testing.T) {
	_, stderr, err := captureOutput(t, func() error {
		return emitKimiOutput(false, "")
	})
	var blockErr *HookBlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("expected *HookBlockError, got %v", err)
	}
	if !strings.Contains(stderr, "forge hook blocked the action") {
		t.Errorf("empty reason must fall back to a default, got %q", stderr)
	}
}

// TestPromoteKimiAdvisory covers the pure helper that decides whether an advisory hook result is
// promoted to a kimi block. The table is built around the TWO over-blocking traps (verified against
// embed.go): every advisory hook emits both a real-advisory branch and a success/clean branch that
// share the hook name — a name allowlist would block the success/clean branches too. The detail text
// here mirrors what extractDetail produces from the real bash stdout (PASS/WARN prefix stripped).
// The caller's agent=="kimi" gate lives at the call site (runHook step 5b); this helper is
// agent-agnostic by design so it can be exercised directly.
//
// TestPromoteKimiAdvisory 覆盖决定 advisory hook 结果是否在 kimi 下提升为阻断的纯函数。
// 表围绕两个"过阻断陷阱"构建（对照 embed.go 核实过）：每个 advisory hook 都同时发真
// advisory 分支和成功/干净分支，共享 hook 名——名字白名单会连成功/干净分支一起阻断。
// 此处 detail 文本镜像 extractDetail 从真实 bash stdout 产出的样子（已去 PASS/WARN 前缀）。
// 调用处的 agent=="kimi" 判定在 runHook step 5b；本函数刻意不依赖 agent，故可直接测。
func TestPromoteKimiAdvisory(t *testing.T) {
	tests := []struct {
		name   string
		hook   string
		passed bool
		detail string
		want   bool
	}{
		// task-guard: real advisory promotes; the Auto-created SUCCESS path must NOT (it just
		// started a task for the agent — blocking would hard-stop the edit it enabled).
		{"task-guard no-task (promote)", "task-guard", true, "[task-guard] No active task. Source changes are allowed but not tracked by a Forge task.", true},
		{"task-guard auto-create (success path, must NOT promote)", "task-guard", true, "[task-guard] Auto-created task 'feat/x' from branch. Source changes tracked.", false},
		{"task-guard has-task bare PASS (empty detail)", "task-guard", true, "", false},

		// assertion-check: real Advisory promotes; the clean "no weakening detected" branch must
		// NOT (lowercase "(advisory)" lacks the capital "Advisory:" the predicate keys on).
		{"assertion-check weakening (promote)", "assertion-check", true, "[assertion-check] Advisory: 疑似断言弱化——[Go] t.Fatal net removed. 请核查。", true},
		{"assertion-check clean (must NOT promote)", "assertion-check", true, "[assertion-check] no weakening detected (advisory)", false},

		// bash-guard: write-without-task promotes; read-only / has-task emit nothing (empty detail).
		{"bash-guard write-no-task (promote)", "bash-guard", true, "[bash-guard] Bash write without active task. Changes are allowed but not tracked.", true},
		{"bash-guard read-only (empty detail)", "bash-guard", true, "", false},

		// Guards: non-allowlisted hooks, already-blocked, whitespace detail.
		{"non-allowlist hook (file-sentinel)", "file-sentinel", true, "[file-sentinel] snapshot taken", false},
		{"non-allowlist hook (auto-compile)", "auto-compile", true, "[auto-compile] Advisory: 已修改源码", false},
		{"already blocked (no double-flip)", "task-guard", false, "[task-guard] No active task.", false},
		{"whitespace-only detail", "task-guard", true, "   \t\n  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := promoteKimiAdvisory(tt.hook, tt.passed, tt.detail)
			if got != tt.want {
				t.Errorf("promoteKimiAdvisory(%q, passed=%v, detail=%q) = %v, want %v", tt.hook, tt.passed, tt.detail, got, tt.want)
			}
		})
	}
}

func TestPromoteKimiAdvisory_EscapeHatch(t *testing.T) {
	// FORGE_KIMI_ADVISORY=soft reverts to pure advisory: even a real-advisory predicate match is
	// suppressed. t.Setenv auto-restores at test end so it cannot leak into other tests.
	t.Setenv("FORGE_KIMI_ADVISORY", "soft")
	got := promoteKimiAdvisory("task-guard", true, "[task-guard] No active task. Source changes are allowed but not tracked.")
	if got {
		t.Errorf("FORGE_KIMI_ADVISORY=soft must suppress promotion, got %v", got)
	}
}
