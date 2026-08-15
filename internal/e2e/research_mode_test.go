package e2e

import (
	"strings"
	"testing"
)

// TestAutoCompile_SilentInResearchMode dogfood 5.1: when the session never
// Edit|Write'd source code (AgentFare research/review mode), the auto-compile
// hook should emit "PASS research-mode session, advisory suppressed" —
// the advisory occupies AdditionalContext character budget yet adds no value
// in a pure-research scenario. Under the Wave-1 allow contract an allowing hook
// emits context (if any) in a {"hookSpecificOutput":{...,"additionalContext":...}}
// object and never decision:approve; the assertion checks additionalContext.
//
// TestAutoCompile_SilentInResearchMode dogfood 5.1：auto-compile hook 在会话
// 从未 Edit|Write 源码时（AgentFare 调研/审查模式）应输出「PASS research-mode
// session, advisory suppressed」——占 AdditionalContext 字符配额且对纯研究场景无
// 助益。Wave-1 放行契约下，放行的 hook 的上下文（若有）走
// {"hookSpecificOutput":{...,"additionalContext":...}} 对象，绝无 decision:approve；
// 断言看 additionalContext。
func TestAutoCompile_SilentInResearchMode(t *testing.T) {
	dir := freshProject(t)
	const sid = "sess-research"

	t.Setenv("FORGE_SESSION_ID", sid)
	t.Setenv("TMPDIR", t.TempDir())

	stdout, _, err := forgeHook(t, dir, "auto-compile", hookStdin(t, sid, "PostToolUse", "Write", map[string]any{
		"file_path": "docs/notes.md",
		"content":   "# research-mode note",
	}))
	if err != nil {
		t.Fatalf("auto-compile: %v", err)
	}
	assertAllowOutput(t, stdout)
	if !strings.Contains(stdout, "research-mode session, advisory suppressed") {
		t.Errorf("research-mode silent text missing. Got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[auto-compile] Advisory") ||
		strings.Contains(stdout, "no source touched (compile self-check delegated to agent)") {
		t.Errorf("research-mode session must NOT emit Advisory/no-source-touched text. Got:\n%s", stdout)
	}
}

// TestBashGuard_SilentOnWriteInResearchMode dogfood 5.1 bash-guard branch:
// no active task + write cmd + NO source touched in this session → the hook
// allows (exit 0, no decision:approve — Wave-1 contract) and the output does
// not carry the "no active task" WARN.
//
// TestBashGuard_SilentOnWriteInResearchMode dogfood 5.1 bash-guard branch：
// no active task + write cmd + NO source touched in this session → 放行
// （退出码 0，无 decision:approve——Wave-1 契约）且输出不带"no active task" WARN。
func TestBashGuard_SilentOnWriteInResearchMode(t *testing.T) {
	dir := freshProject(t)
	const sid = "sess-research-bg"

	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("FORGE_SESSION_ID", sid)

	// 'tee' triggers IS_WRITE_CMD=1 (bash-guard's has_write_pattern) without
	// tripping hazard-guard (rm -rf writes too but hazard intercepts first).
	in := hookStdin(t, sid, "PreToolUse", "Bash", map[string]any{
		"command": "tee /tmp/forge-bg-research-test.txt",
	})
	out, _, err := forgeHook(t, dir, "bash-guard", in)
	if err != nil {
		t.Fatalf("bash-guard: %v", err)
	}
	assertAllowOutput(t, out)
	// The whole point of 5.1: no no-task WARN in research mode.
	if strings.Contains(out, "no active task") || strings.Contains(out, "without active task") {
		t.Errorf("research-mode must NOT emit no-task WARN. Got:\n%s", out)
	}
}
