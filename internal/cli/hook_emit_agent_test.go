package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// 本文件钉住 Wave 1 引入的 per-agent 输出协议契约（emitAgentOutput + 各宿主
// emitter）。两条承重不变式：
//   - allow 绝不发 decision:"approve"（Claude PreToolUse 上会绕过权限系统；
//     codex 解析它但把 hook 判为 FAILED）；
//   - block 恒返回 *HookBlockError（→ exit 2），唯一例外 copilot Stop——那里
//     decision JSON + exit 0 才是唯一阻断通道。

func requireBlockErr(t *testing.T, err error, wantSub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("block must return an error (exit 2), got nil")
	}
	var blockErr *HookBlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("block must return *HookBlockError (Execute maps it to exit 2), got %T: %v", err, err)
	}
	if !strings.Contains(blockErr.Reason, wantSub) {
		t.Errorf("block reason = %q, want substring %q", blockErr.Reason, wantSub)
	}
}

func TestEmitCodexOutput_AllowBareHookSpecificOutput(t *testing.T) {
	// Allow with detail on a context-carrying event: BARE hookSpecificOutput, no decision.
	stdout, _, err := captureOutput(t, func() error {
		return emitCodexOutput("PreToolUse", true, "advisory text")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	var out HookOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("codex allow output must be valid JSON: %v\n%s", err, stdout)
	}
	if out.Decision != "" {
		t.Errorf("codex allow must not emit a decision (approve marks the hook FAILED), got %q", out.Decision)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.AdditionalContext != "advisory text" {
		t.Errorf("codex allow must carry additionalContext, got %+v", out.HookSpecificOutput)
	}

	// Stop has no context channel on codex — allow must be silent there.
	stdout, _, err = captureOutput(t, func() error {
		return emitCodexOutput("Stop", true, "advisory text")
	})
	if err != nil {
		t.Fatalf("allow on Stop must return nil, got %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("codex Stop allow must be silent (no context channel), got %q", stdout)
	}
}

func TestEmitCodexOutput_Block(t *testing.T) {
	stdout, stderr, err := captureOutput(t, func() error {
		return emitCodexOutput("PreToolUse", false, "task-guard: 先启动任务")
	})
	requireBlockErr(t, err, "task-guard")
	if strings.Contains(stdout, "decision") {
		t.Errorf("codex block must not rely on stdout decision JSON, got %q", stdout)
	}
	if !strings.Contains(stderr, "task-guard") {
		t.Errorf("codex block reason must go to stderr, got %q", stderr)
	}
}

func TestEmitCursorOutput_AllowSnakeCaseContext(t *testing.T) {
	stdout, _, err := captureOutput(t, func() error {
		return emitCursorOutput("PostToolUse", true, "ctx detail")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &raw); err != nil {
		t.Fatalf("cursor allow output must be valid JSON: %v\n%s", err, stdout)
	}
	if raw["additional_context"] != "ctx detail" {
		t.Errorf("cursor context must be top-level snake_case additional_context, got %q", stdout)
	}

	// PreToolUse has no allow context channel on cursor.
	stdout, _, err = captureOutput(t, func() error {
		return emitCursorOutput("PreToolUse", true, "ctx detail")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("cursor PreToolUse allow must be silent, got %q", stdout)
	}
}

func TestEmitCursorOutput_Block(t *testing.T) {
	stdout, stderr, err := captureOutput(t, func() error {
		return emitCursorOutput("PreToolUse", false, "blocked by freeze")
	})
	requireBlockErr(t, err, "freeze")
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("cursor block emits no stdout (exit 2 is the deny channel), got %q", stdout)
	}
	if !strings.Contains(stderr, "freeze") {
		t.Errorf("cursor block reason must go to stderr, got %q", stderr)
	}
}

func TestEmitCopilotOutput_Shapes(t *testing.T) {
	// Allow context channel: top-level camelCase additionalContext on PostToolUse.
	stdout, _, err := captureOutput(t, func() error {
		return emitCopilotOutput("PostToolUse", "auto-compile", true, "ctx detail")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &raw); err != nil {
		t.Fatalf("copilot allow output must be valid JSON: %v\n%s", err, stdout)
	}
	if raw["additionalContext"] != "ctx detail" {
		t.Errorf("copilot context must be top-level camelCase additionalContext, got %q", stdout)
	}

	// PreToolUse block: deny decision JSON + HookBlockError (exit 2 = fail-closed deny).
	stdout, _, err = captureOutput(t, func() error {
		return emitCopilotOutput("PreToolUse", "task-guard", false, "need a task")
	})
	requireBlockErr(t, err, "need a task")
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &raw); err != nil {
		t.Fatalf("copilot PreToolUse block stdout must be valid JSON: %v\n%s", err, stdout)
	}
	if raw["permissionDecision"] != "deny" {
		t.Errorf("copilot PreToolUse block must emit permissionDecision:deny, got %q", stdout)
	}

	// Stop block: decision JSON + exit 0 (exit 2 on agentStop is only a warning).
	stdout, _, err = captureOutput(t, func() error {
		return emitCopilotOutput("Stop", "task-verify", false, "gates not done")
	})
	if err != nil {
		t.Fatalf("copilot Stop block must return nil (exit 2 is only a warning there), got %v", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &raw); err != nil {
		t.Fatalf("copilot Stop block stdout must be valid JSON: %v\n%s", err, stdout)
	}
	if raw["decision"] != "block" {
		t.Errorf("copilot Stop block must emit decision:block + exit 0, got %q", stdout)
	}
}

func TestEmitWindsurfOutput_NoStdoutProtocol(t *testing.T) {
	// Windsurf has no stdout JSON protocol at all: allow silent, block stderr+exit 2.
	stdout, _, err := captureOutput(t, func() error {
		return emitWindsurfOutput("bash-guard", true, "advisory")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("windsurf allow must emit NO stdout, got %q", stdout)
	}

	stdout, stderr, err := captureOutput(t, func() error {
		return emitWindsurfOutput("bash-guard", false, "quarantine risk")
	})
	requireBlockErr(t, err, "quarantine")
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("windsurf block must emit NO stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "quarantine") {
		t.Errorf("windsurf block reason must go to stderr, got %q", stderr)
	}
}

func TestEmitClineOutput_CancelJSON(t *testing.T) {
	stdout, _, err := captureOutput(t, func() error {
		return emitClineOutput(true, "context to inject")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &raw); err != nil {
		t.Fatalf("cline allow output must be valid JSON: %v\n%s", err, stdout)
	}
	if raw["cancel"] != false {
		t.Errorf("cline allow must emit cancel:false, got %q", stdout)
	}
	if raw["contextModification"] != "context to inject" {
		t.Errorf("cline allow must carry contextModification, got %q", stdout)
	}

	stdout, _, err = captureOutput(t, func() error {
		return emitClineOutput(false, "blocked reason")
	})
	requireBlockErr(t, err, "blocked reason")
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &raw); err != nil {
		t.Fatalf("cline block stdout must be valid JSON: %v\n%s", err, stdout)
	}
	if raw["cancel"] != true {
		t.Errorf("cline block must emit cancel:true, got %q", stdout)
	}
	if raw["errorMessage"] != "blocked reason" {
		t.Errorf("cline block must carry errorMessage, got %q", stdout)
	}
}

func TestEmitClaudeOutput_DefaultAllowSilentAndBlock(t *testing.T) {
	stdout, _, err := captureOutput(t, func() error {
		return emitClaudeOutput("PreToolUse", true, "")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("claude allow with no detail must be silent, got %q", stdout)
	}

	stdout, _, err = captureOutput(t, func() error {
		return emitClaudeOutput("PreToolUse", true, "detail")
	})
	if err != nil {
		t.Fatalf("allow must return nil, got %v", err)
	}
	var out HookOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("claude allow-with-detail must be valid JSON: %v\n%s", err, stdout)
	}
	if out.Decision != "" {
		t.Errorf("claude allow must not emit a decision (approve bypasses the permission system), got %q", out.Decision)
	}

	stdout, stderr, err := captureOutput(t, func() error {
		return emitClaudeOutput("PreToolUse", false, "blocked reason")
	})
	requireBlockErr(t, err, "blocked reason")
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("claude block must emit decision:block JSON, got %q", stdout)
	}
	if !strings.Contains(stderr, "blocked reason") {
		t.Errorf("claude block reason must go to stderr, got %q", stderr)
	}
}

// TestEmitAgentOutput_DispatchRoutesByAgent spot-checks the dispatcher: the same
// verdict must render differently per host (stdout shape AND error type).
func TestEmitAgentOutput_DispatchRoutesByAgent(t *testing.T) {
	stdout, _, err := captureOutput(t, func() error {
		return emitAgentOutput("codex", "PreToolUse", "h", true, "d")
	})
	if err != nil {
		t.Fatalf("codex allow via dispatcher must return nil, got %v", err)
	}
	if !strings.Contains(stdout, "hookSpecificOutput") {
		t.Errorf("codex dispatch must reach emitCodexOutput, got %q", stdout)
	}

	stdout, _, _ = captureOutput(t, func() error {
		return emitAgentOutput("cursor", "PostToolUse", "h", true, "d")
	})
	if !strings.Contains(stdout, "additional_context") {
		t.Errorf("cursor dispatch must reach emitCursorOutput, got %q", stdout)
	}

	_, _, err = captureOutput(t, func() error {
		return emitAgentOutput("windsurf", "PreToolUse", "h", false, "denied")
	})
	requireBlockErr(t, err, "denied")

	_, _, err = captureOutput(t, func() error {
		return emitAgentOutput("", "PreToolUse", "h", false, "denied")
	})
	requireBlockErr(t, err, "denied")
}

// TestEmitAgentOutput_AllowNeverApproves sweeps every agent × representative events:
// no allow-path stdout may contain "approve" — the single invariant that broke codex
// hooks and bypassed Claude permissions before Wave 1.
func TestEmitAgentOutput_AllowNeverApproves(t *testing.T) {
	agents := []string{"", "kimi", "codex", "cursor", "copilot", "windsurf", "cline", "reasonix"}
	for _, agent := range agents {
		for _, ev := range []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit"} {
			stdout, _, err := captureOutput(t, func() error {
				return emitAgentOutput(agent, ev, "h", true, "detail text")
			})
			if err != nil {
				t.Fatalf("agent=%q event=%q allow must return nil, got %v", agent, ev, err)
			}
			if strings.Contains(stdout, "approve") {
				t.Errorf("agent=%q event=%q allow emitted approve: %q", agent, ev, stdout)
			}
		}
	}
}

// TestApplyPatchFilePath pins the codex apply_patch file_path synthesis (Wave 3a):
// the FIRST *** Add/Update/Delete File: header wins; malformed payloads return "".
func TestApplyPatchFilePath(t *testing.T) {
	cases := []struct {
		name  string
		patch string
		want  string
	}{
		{"add file", "*** Begin Patch\n*** Add File: main.go\n+package main\n*** End Patch", "main.go"},
		{"update file", "*** Update File: internal/cli/hook.go\n@@\n x\n*** End Patch", "internal/cli/hook.go"},
		{"delete file", "*** Delete File: old.go", "old.go"},
		{"first header wins", "*** Update File: a.go\n*** Add File: b.go", "a.go"},
		{"header only, empty path", "*** Add File:", ""},
		{"no header", "echo hello world", ""},
		{"empty", "", ""},
		{"indented header", "  *** Update File:  spaced.go  ", "spaced.go"},
	}
	for _, tc := range cases {
		if got := applyPatchFilePath(tc.patch); got != tc.want {
			t.Errorf("%s: applyPatchFilePath = %q, want %q", tc.name, got, tc.want)
		}
	}
}
