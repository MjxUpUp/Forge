package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestE2E_InitGeneratesStructure verifies forge init scaffolds the expected
// layout (user-level-assets era: zero project writes; assets live in the
// user-level DataDir + agent config homes) and that status operates on it.
// This is the init entry point (0→1) — regression protection for the core
// init contract documented in README.
func TestE2E_InitGeneratesStructure(t *testing.T) {
	dir := freshProject(t) // git init + go project + forge init

	// New contract: hook reference copies + protocol.yml + sync stamp live in the
	// user-level DataDir (freshProject pins FORGE_DATA_HOME); claude integration
	// (settings.json hooks, CLAUDE.md forge section, quality skill) lives under
	// CLAUDE_CONFIG_DIR (TestMain-isolated).
	//
	// 新契约：hook 参考副本 + protocol.yml + sync 戳在用户级 DataDir（freshProject
	// 已钉 FORGE_DATA_HOME）；claude 集成（settings.json hooks、CLAUDE.md forge 段、
	// 质量 skill）在 CLAUDE_CONFIG_DIR 下（TestMain 已隔离）。
	dataDir := forgedata.DataDirFor(dir)
	for _, p := range []string{
		"hooks",
		".sync-version",
		"protocol.yml",
	} {
		if !fileExists(t, dataDir, p) {
			t.Errorf("forge init did not generate DataDir/%s", p)
		}
	}
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if settings := readFile(t, claudeHome, "settings.json"); !strings.Contains(settings, "forge hook") {
		t.Error("user-level settings.json should wire forge hooks")
	}
	if claudeMD := readFile(t, claudeHome, "CLAUDE.md"); !strings.Contains(claudeMD, "FORGE:START") {
		t.Error("user-level CLAUDE.md should carry the forge section")
	}
	if !fileExists(t, claudeHome, "skills/forge-quality/SKILL.md") {
		t.Error("user-level forge-quality SKILL.md missing")
	}

	// Zero project writes + reverse assertion: removed artifacts no longer
	// generated (regression guard — previously pipeline.yml/state.json/
	// forge-pipeline existed).
	//
	// 零项目写入 + 反向断言：废弃产物不再生成（防回归——曾有
	// pipeline.yml/state.json/forge-pipeline）。
	for _, p := range []string{
		".forge",
		".claude",
		".forge/pipeline.yml",
		".forge/state.json",
		".claude/skills/forge-pipeline",
	} {
		if fileExists(t, dir, p) {
			t.Errorf("forge init must not generate project-level artifact %s", p)
		}
	}

	// status always prints the project header (even with no active task) so
	// the user can confirm forge is in place.
	//
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
// explicit --agents list must wire quality configs for every listed backend in
// a single init run. agentbridge unit tests cover each translator and
// ParseAgentFlag in isolation; this guards the CLI integration contract
// (init.go → ParseAgentFlag → TranslateForAgents) that nothing else exercises.
// After user-level-assets, the proof is USER-level: cursor's hooks.json under
// the isolated HOME, claude's settings.json under CLAUDE_CONFIG_DIR — and zero
// project-level writes.
func TestE2E_InitMultiAgent(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)

	// One init naming two backends explicitly (not auto-detect).
	forge(t, dir, "init", "--agents", "claude-code,cursor")

	// claude-code baseline: user-level settings.json wires the forge hooks
	// (CLAUDE_CONFIG_DIR is TestMain-isolated). cursor: user-level
	// ~/.cursor/hooks.json (HOME is TestMain-isolated) — the proof the explicit
	// cursor agent ran through ParseAgentFlag → TranslateForAgents →
	// CursorTranslator.
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if settings := readFile(t, claudeHome, "settings.json"); !strings.Contains(settings, "forge hook") {
		t.Error("init --agents claude-code,cursor: user-level settings.json missing forge hooks")
	}
	cursorHooks := readFile(t, os.Getenv("HOME"), ".cursor/hooks.json")
	if !strings.Contains(cursorHooks, "forge hook task-guard") {
		t.Error("init --agents claude-code,cursor: user-level cursor hooks.json missing forge wiring")
	}

	// Zero project writes for either backend.
	//
	// 两个后端都零项目写入。
	for _, p := range []string{".claude", ".cursor"} {
		if fileExists(t, dir, p) {
			t.Errorf("init --agents must not write %s into the project (zero-project-write)", p)
		}
	}
}

// TestE2E_InitCodex verifies init --agents codex generates a user-level Codex
// hooks.json ($CODEX_HOME/hooks.json) that mirrors the Claude Code wiring. This
// guards the integration contract that codex, alongside claude-code, actually
// enforces the Forge gates (the only two agents whose translator emits real hook
// commands rather than guidance text). Zero project-level writes
// (user-level-assets).
func TestE2E_InitCodex(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)

	forge(t, dir, "init", "--agents", "codex")

	data, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "hooks.json"))
	if err != nil {
		t.Fatalf("init --agents codex did not generate user-level $CODEX_HOME/hooks.json: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"PreToolUse"`, `"PostToolUse"`, `"Stop"`, "forge hook task-guard"} {
		if !strings.Contains(content, want) {
			t.Errorf("codex hooks.json missing %q", want)
		}
	}

	// Zero project writes.
	//
	// 零项目写入。
	if fileExists(t, dir, ".codex") {
		t.Error("init --agents codex must not write .codex into the project (zero-project-write)")
	}
}
