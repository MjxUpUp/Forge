package e2e

import (
	"strings"
	"testing"
)

// TestAutoCompile_SilentInResearchMode dogfood 5.1：auto-compile hook 在会话
// 从未 Edit|Write 源码时（AgentFare 调研/审查模式）应输出"PASS research-mode
// session, advisory suppressed"——占 AdditionalContext 字符配额且对纯研究场景无
// 助益。Forge hook wrapper 把 bash stdout 塞进 {"decision":"approve",...,
// "additionalContext":"..."} JSON；断言看 additionalContext。
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
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Fatalf("auto-compile must approve, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "research-mode session, advisory suppressed") {
		t.Errorf("research-mode silent text missing. Got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[auto-compile] Advisory") ||
		strings.Contains(stdout, "no source touched (compile self-check delegated to agent)") {
		t.Errorf("research-mode session must NOT emit Advisory/no-source-touched text. Got:\n%s", stdout)
	}
}

// TestBashGuard_SilentOnWriteInResearchMode dogfood 5.1 bash-guard branch：
// no active task + write cmd + NO source touched in this session → 决策 approve
// 且 additionalContext 不带 "no active task" WARN。
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
	if !strings.Contains(out, `"decision":"approve"`) {
		t.Fatalf("bash-guard must approve a research-mode write. Got:\n%s", out)
	}
	// The whole point of 5.1: no no-task WARN in research mode.
	if strings.Contains(out, "no active task") || strings.Contains(out, "without active task") {
		t.Errorf("research-mode must NOT emit no-task WARN. Got:\n%s", out)
	}
}
