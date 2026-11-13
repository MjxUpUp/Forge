package hookdispatch

import (
	"encoding/json"
	"strings"
	"testing"
)

// 阶段 B（ADR-0002）把 hook.go 的契约门从 `root != "" && !contractAllowsHook(...)`
// 改成 `!contractAllowsHook(...)`——无契约的宿主层（root==""，global hook 场景）
// 也走这道门，默认 audit-only。下面两个 RunHook 级测试钉死这条新接线：一个默认
// 静音、一个 opt-in 复原，互为构造性对侧，防「skill-scan 本就恒静默」的 vacuous pass。
//
// 选 skill-scan：它是 global hook（项目无关，扫 $HOME/.claude/skills），且自身路径
// 运行时恒输出含 "skill-scan" 的 advisory PASS detail（见
// TestHookOutput_GlobalHookRunsOutsideProject）。standard 档位门不拦它，所以这里的
// 静音只能来自契约门——正是要钉的接线。

// TestRunHook_ContractGateDefaultQuietOutsideProject：无契约宿主层 + 未开
// FORGE_HOOKS → 契约门静音 global hook（无 stdout、无 advisory detail、nil error）。
func TestRunHook_ContractGateDefaultQuietOutsideProject(t *testing.T) {
	reanchorCwd(t)
	restore := chdirToNonForgeRoot(t) // findProjectRoot 失败 → root==""
	defer restore()
	t.Setenv("HOME", t.TempDir()) // skill-scan 走「无 skills」PASS 分支
	t.Setenv("FORGE_HOOKS", "")   // 阶段 B 默认 audit-only（覆盖 TestMain 的 on）

	out, hookErr := runHookCapture(t, "skill-scan",
		`{"hook_event_name":"SessionStart","tool_name":"","tool_input":{}}`)
	if hookErr != nil {
		t.Fatalf("默认 audit-only 应静默放行（nil error），got: %v", hookErr)
	}
	if got := strings.TrimSpace(out); got != "" {
		t.Errorf("默认 audit-only：无契约宿主层 skill-scan 应被契约门静音（无 stdout），got: %q", got)
	}
}

// TestRunHook_ContractGateOptInRestoresGlobalHook：同一场景开 FORGE_HOOKS=on →
// 契约门放行，skill-scan 自身路径运行并输出 advisory detail。与上一用例互为对侧：
// 证明上一用例的空输出确系契约门静音，而非 hook 本就无声。
func TestRunHook_ContractGateOptInRestoresGlobalHook(t *testing.T) {
	reanchorCwd(t)
	restore := chdirToNonForgeRoot(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FORGE_HOOKS", "on")

	out, hookErr := runHookCapture(t, "skill-scan",
		`{"hook_event_name":"SessionStart","tool_name":"","tool_input":{}}`)
	if hookErr != nil {
		t.Fatalf("FORGE_HOOKS=on 应执法（nil error），got: %v", hookErr)
	}
	var result HookOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("opt-in 后 skill-scan 应运行并输出 JSON，got: %q, err: %v", out, err)
	}
	if result.HookSpecificOutput == nil || !strings.Contains(result.HookSpecificOutput.AdditionalContext, "skill-scan") {
		t.Errorf("FORGE_HOOKS=on 时 skill-scan 应运行（advisory detail 含 skill-scan），got: %+v", result.HookSpecificOutput)
	}
}
