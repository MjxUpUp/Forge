package hookdispatch

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

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

// TestKimiAdvisoryNeverPromotes 钉住 kimi advisory 提升的 2026-08-24 退役：
// 所有 advisory 类结果（task-guard WARN、bash-guard、assertion-check）在 kimi
// 上必须保持放行——被提升的 exit-2 deny 自相矛盾（reason 自述「allowed」），
// 且 kimi 把 PreToolUse 上**任何** stdout 当 deny，故 advisory 改为按项目入队、
// UserPromptSubmit 攒发（hook_kimi_advisory.go）。此测试变红 = 有人重新注册了
// kimi 的 PromoteAdvisory 规则——另见 hostcap 的 TestKimiNoPromoteAdvisory。
func TestKimiAdvisoryNeverPromotes(t *testing.T) {
	cases := []struct {
		hook   string
		detail string
	}{
		{"task-guard", "[task-guard] Untracked source edit — no active task. Why: changes outside a task skip verify/review/score gates."},
		{"bash-guard", "[bash-guard] Bash write without active task. Changes are allowed but not tracked."},
		{"assertion-check", "[assertion-check] Advisory: 疑似断言弱化——[Go] t.Fatal net removed. 请核查。"},
	}
	for _, c := range cases {
		if got := promoteAdvisory("kimi", c.hook, true, c.detail); got {
			t.Errorf("promoteAdvisory(kimi, %q, true, %q) = %v, want false (kimi advisories queue, never block)", c.hook, c.detail, got)
		}
	}
}
