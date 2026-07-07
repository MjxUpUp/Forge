package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_InitGeneratesStructure verifies forge init scaffolds the expected
// project layout (task-pipeline era: no project-level pipeline.yml/state.json)
// and that status operates on it. This is the init entry point (0→1) —
// regression protection for the core init contract documented in README.
func TestE2E_InitGeneratesStructure(t *testing.T) {
	dir := freshProject(t) // git init + go project + forge init

	// 项目级管道已删除：init 不再生成 pipeline.yml / state.json / forge-pipeline skill。
	// 新契约：hooks + Claude 集成 + 质量协议 skill + CLAUDE.md + sync stamp。
	for _, p := range []string{
		".forge/hooks",
		".forge/.sync-version",
		".claude/settings.local.json",
		".claude/CLAUDE.md",
		".claude/skills/forge-quality/SKILL.md",
	} {
		if !fileExists(t, dir, p) {
			t.Errorf("forge init did not generate %s", p)
		}
	}
	// 反向断言：废弃产物不再生成（防回归——曾有 pipeline.yml/state.json/forge-pipeline）。
	for _, p := range []string{
		".forge/pipeline.yml",
		".forge/state.json",
		".claude/skills/forge-pipeline",
	} {
		if fileExists(t, dir, p) {
			t.Errorf("forge init must not generate removed artifact %s", p)
		}
	}

	// status 始终打印项目头（即使无任务），让用户确认 forge 已就位。
	out := forge(t, dir, "status")
	if !strings.Contains(out, "Project:") {
		t.Errorf("forge status missing 'Project:' header:\n%s", out)
	}
}

// TestE2E_TaskStartCreatesState verifies the task-level pipeline entry point
// (1→100): task start creates a branch and the task status reflects the first
// gate. This is the everyday loop Forge exists to govern.
func TestE2E_TaskStartCreatesState(t *testing.T) {
	dir := freshProject(t)
	// Commit the scaffold so the branch checkout is clean.
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")

	// On the default branch, start a task that creates its own branch.
	forge(t, dir, "task", "start", "--ref", "feat/e2e-probe",
		"--title", "probe", "--branch")

	branches := git(t, dir, "branch", "--list")
	if !strings.Contains(branches, "feat/e2e-probe") {
		t.Errorf("task start --branch did not create branch:\n%s", branches)
	}

	// task status must show the first gate (task-implement) as pending.
	out := forge(t, dir, "task", "status")
	if !strings.Contains(strings.ToLower(out), "implement") {
		t.Errorf("task status does not show first gate:\n%s", out)
	}
}

// TestE2E_InitMultiAgent verifies the multi-agent init path end-to-end: an
// explicit --agents list must scaffold quality configs for every listed
// backend in a single init run. agentbridge unit tests cover each translator
// and ParseAgentFlag in isolation; this guards the CLI integration contract
// (init.go → ParseAgentFlag → TranslateForAgents) that nothing else
// exercises. Without it, if init stops wiring TranslateForAgents every
// backend except .claude silently loses its quality config — a regression
// the component-level tests cannot catch.
func TestE2E_InitMultiAgent(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)

	// One init naming two backends explicitly (not auto-detect).
	forge(t, dir, "init", "--agents", "claude-code,cursor")

	// .claude/settings.local.json is baseline init output (always generated).
	// .cursor/rules/forge-quality.mdc is the proof the explicit cursor agent
	// ran through ParseAgentFlag → TranslateForAgents → CursorTranslator
	// (which MkdirAll's the rules dir, so it does not need a pre-existing .cursor).
	for _, p := range []string{
		".claude/settings.local.json",
		".cursor/rules/forge-quality.mdc",
	} {
		if !fileExists(t, dir, p) {
			t.Errorf("init --agents claude-code,cursor did not generate %s", p)
		}
	}
}

// TestE2E_InitCodex verifies init --agents codex generates a Codex hooks.json
// that mirrors the Claude Code wiring. This guards the integration contract
// that codex, alongside claude-code, actually enforces the Forge gates (the
// only two agents whose translator emits real hook commands rather than
// guidance text).
func TestE2E_InitCodex(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)

	forge(t, dir, "init", "--agents", "codex")

	data, err := os.ReadFile(filepath.Join(dir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("init --agents codex did not generate .codex/hooks.json: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"PreToolUse"`, `"PostToolUse"`, `"Stop"`, "forge hook task-guard"} {
		if !strings.Contains(content, want) {
			t.Errorf("codex hooks.json missing %q", want)
		}
	}
}
