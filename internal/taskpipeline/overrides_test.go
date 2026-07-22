package taskpipeline

import "testing"

// TestEscapeDisabled_Precedence 钉住方案5：per-task Overrides 优先判定，全局 env 作
// fallback——防一个任务逃生泄漏到同 shell 的其他任务（"假硬门禁"反噬的根因）。
func TestEscapeDisabled_Precedence(t *testing.T) {
	t.Setenv("FORGE_WORK_ACTIVITY", "disable")
	t.Setenv("FORGE_TEST_COVERAGE", "disable")

	// 无 override → env 生效（fallback）。
	if !EscapeDisabled(&TaskState{}, escapeWorkActivity, envWorkActivity) {
		t.Error("env set, no override: want disabled=true (env fallback)")
	}
	// override 显式空（""）不取消 env——override 仅在值 "disable" 时生效，env 仍是 fallback。
	if !EscapeDisabled(&TaskState{Overrides: TaskOverrides{WorkActivity: ""}}, escapeWorkActivity, envWorkActivity) {
		t.Error("env set + empty override: env fallback should still fire")
	}
}

// TestEscapeDisabled_NoEnvNoOverride：既无 override 也无 env → 不禁用。
func TestEscapeDisabled_NoEnvNoOverride(t *testing.T) {
	t.Setenv("FORGE_WORK_ACTIVITY", "")
	t.Setenv("FORGE_TEST_COVERAGE", "")
	s := &TaskState{}
	if EscapeDisabled(s, escapeWorkActivity, envWorkActivity) {
		t.Error("no env, no override (work-activity): want disabled=false")
	}
	if EscapeDisabled(s, escapeTestCoverage, "FORGE_TEST_COVERAGE") {
		t.Error("no env, no override (test-coverage): want disabled=false")
	}
}

// TestEscapeDisabled_OverrideOnly：override=disable 生效而 env 未设——per-task 路径独立可用。
func TestEscapeDisabled_OverrideOnly(t *testing.T) {
	t.Setenv("FORGE_WORK_ACTIVITY", "")
	s := &TaskState{Overrides: TaskOverrides{WorkActivity: "disable"}}
	if !EscapeDisabled(s, escapeWorkActivity, envWorkActivity) {
		t.Error("override=disable, no env: want disabled=true")
	}
}

// TestEscapeDisabled_NilState：nil state 不 panic，回落 env 判定。
func TestEscapeDisabled_NilState(t *testing.T) {
	t.Setenv("FORGE_TEST_COVERAGE", "disable")
	if !EscapeDisabled(nil, escapeTestCoverage, "FORGE_TEST_COVERAGE") {
		t.Error("nil state + env set: want disabled=true (env fallback, no panic)")
	}
}
