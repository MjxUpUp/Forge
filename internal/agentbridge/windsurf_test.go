package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// windsurfHooksPathUnder 拼出隔离 home 下的 user-level hooks.json 路径。
func windsurfHooksPathUnder(home string) string {
	return filepath.Join(home, ".codeium", "windsurf", "hooks.json")
}

// TestWindsurfTranslator_Translate_CreatesFile verifies Translate creates the
// user-level ~/.codeium/windsurf/hooks.json (Cascade natively loads user-level
// hooks, see https://docs.windsurf.com/windsurf/cascade/hooks) even when the
// directory tree does not exist yet.
func TestWindsurfTranslator_Translate_CreatesFile(t *testing.T) {
	home := isolateHome(t)

	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(windsurfHooksPathUnder(home))
	if err != nil {
		t.Fatalf("user-level hooks.json not created: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"pre_write_code"`, `"pre_run_command"`, "forge hook task-guard --agent windsurf"} {
		if !strings.Contains(content, want) {
			t.Errorf("windsurf user-level hooks.json missing %q", want)
		}
	}
}

// TestWindsurfTranslator_MergePreservesUserEntries pins the merge contract on
// windsurf's flat schema: user entries and unknown top-level fields survive; forge
// entries are replaced wholesale (stale ones removed, current ones present exactly once).
func TestWindsurfTranslator_MergePreservesUserEntries(t *testing.T) {
	home := isolateHome(t)
	path := windsurfHooksPathUnder(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	existing := `{
  "customKey": true,
  "hooks": {
    "pre_run_command": [
      {"command": "echo user-hook", "show_output": true},
      {"command": "forge hook stale-removed-hook --agent windsurf", "show_output": false}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, `"customKey"`) {
		t.Error("user top-level field not preserved")
	}
	if !strings.Contains(content, "echo user-hook") {
		t.Error("user hook entry not preserved")
	}
	if strings.Contains(content, "stale-removed-hook") {
		t.Error("stale forge hook entry not replaced")
	}
	if n := strings.Count(content, `"forge hook task-guard --agent windsurf"`); n != 1 {
		t.Errorf("forge hook task-guard appears %d times, want 1", n)
	}
}

// TestWindsurfTranslator_Idempotent verifies a second Translate leaves the
// user-level hooks.json byte-identical.
func TestWindsurfTranslator_Idempotent(t *testing.T) {
	home := isolateHome(t)
	assertTranslateIdempotent(t, &WindsurfTranslator{}, windsurfHooksPathUnder(home))
}

// TestStripWindsurfHooksUserLevel covers the strip roundtrip: Translate then Strip
// removes every forge entry while preserving user entries; a second Strip and a
// missing file are clean no-ops.
func TestStripWindsurfHooksUserLevel(t *testing.T) {
	home := isolateHome(t)
	path := windsurfHooksPathUnder(home)

	// Missing file → clean no-op.
	changed, err := StripWindsurfHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("missing file: changed=%v err=%v, want false/nil", changed, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	seed := `{"hooks": {"pre_run_command": [{"command": "echo user-hook", "show_output": true}]}}`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatal(err)
	}

	changed, err = StripWindsurfHooksUserLevel()
	if err != nil || !changed {
		t.Fatalf("strip after Translate: changed=%v err=%v, want true/nil", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "forge hook") || strings.Contains(content, "forge gate") {
		t.Errorf("forge entries remain after strip:\n%s", content)
	}
	if !strings.Contains(content, "echo user-hook") {
		t.Error("user hook entry lost after strip")
	}

	// Second strip → no-op.
	changed, err = StripWindsurfHooksUserLevel()
	if err != nil || changed {
		t.Fatalf("second strip: changed=%v err=%v, want false/nil", changed, err)
	}
}

// TestWindsurfTranslator_MergePreservesUnknownFields pins the raw-merge fix: user hook entries carrying fields the typed windsurfHookEntry struct does not declare (powershell, working_directory) must survive Translate with values intact — a typed round-trip silently dropped them.
//
// TestWindsurfTranslator_MergePreservesUnknownFields 钉死 raw-merge 修复：携带
// 类型化 windsurfHookEntry 未声明字段（powershell、working_directory）的用户
// hook 条目必须在 Translate 后值完整保留——类型化往返会静默丢弃它们。
func TestWindsurfTranslator_MergePreservesUnknownFields(t *testing.T) {
	home := isolateHome(t)
	path := windsurfHooksPathUnder(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	existing := `{
  "hooks": {
    "pre_run_command": [
      {"command": "echo user-hook", "show_output": true, "powershell": "echo user-hook.ps1", "working_directory": "src"}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Field-level assertion (formatting-agnostic): the kept user entry must carry
	// every original field with its original value.
	var cfg struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	var userEntry map[string]any
	for _, e := range cfg.Hooks["pre_run_command"] {
		if e["command"] == "echo user-hook" {
			userEntry = e
		}
	}
	if userEntry == nil {
		t.Fatal("user hook entry not preserved")
	}
	if userEntry["powershell"] != "echo user-hook.ps1" {
		t.Errorf("user entry unknown field powershell lost or altered: %v", userEntry["powershell"])
	}
	if userEntry["working_directory"] != "src" {
		t.Errorf("user entry unknown field working_directory lost or altered: %v", userEntry["working_directory"])
	}
}

// TestWindsurfHooks_OnlyLegalCascadeEvents pins the event-name fix: Cascade's official hook roster has NO session_start/session_end — the SessionStart group hangs on pre_user_prompt and the Stop group on post_cascade_response.
//
// TestWindsurfHooks_OnlyLegalCascadeEvents 钉死事件名修复：Cascade 官方 hook
// 名册没有 session_start/session_end——SessionStart 组挂 pre_user_prompt、
// Stop 组挂 post_cascade_response。生成接线里出现任何 session_* 事件都永不
// 触发。
func TestWindsurfHooks_OnlyLegalCascadeEvents(t *testing.T) {
	home := isolateHome(t)
	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	data, err := os.ReadFile(windsurfHooksPathUnder(home))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	legal := map[string]bool{
		"pre_read_code": true, "post_read_code": true,
		"pre_write_code": true, "post_write_code": true,
		"pre_run_command": true, "post_run_command": true,
		"pre_mcp_tool_use": true, "post_mcp_tool_use": true,
		"pre_user_prompt":       true,
		"post_cascade_response": true, "post_cascade_response_with_transcript": true,
		"post_setup_worktree": true,
	}
	// The two remapped groups must be present: SessionStart hangs on pre_user_prompt
	// and Stop on post_cascade_response (Cascade's roster has no session_* events).
	assertOnlyLegalEvents(t, "windsurf", cfg.Hooks, legal,
		[]string{"pre_user_prompt", "post_cascade_response"}, nil, "")
}

// TestWindsurfGlobalRules_ConditionalPreamble pins the conditional-activation preamble: global_rules.md is loaded by Cascade in EVERY workspace, so the forge section must state it applies only to forge-initialized projects.
//
// TestWindsurfGlobalRules_ConditionalPreamble 钉死条件激活前置：global_rules.md
// 被 Cascade 在每个 workspace 加载，forge 段必须声明仅对已 forge init 的项目
// 生效。
func TestWindsurfGlobalRules_ConditionalPreamble(t *testing.T) {
	isolateHome(t)
	if err := (&WindsurfTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	rulesPath, err := WindsurfGlobalRulesPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("global_rules.md not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[forge-session]") {
		t.Error("global_rules.md preamble must anchor activation to the managed-session banner（P3）")
	}
	if !strings.Contains(content, "ignore this section entirely") {
		t.Error("global_rules.md preamble must tell agents to ignore the section outside forge projects")
	}
}
