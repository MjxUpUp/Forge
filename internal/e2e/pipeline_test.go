package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestE2E_InitGeneratesStructure verifies the Phase C init contract: repo-only
// products (spec/forge.yaml with hooks off, spec/changes/, AGENTS.md) and no
// user-level/DataDir writes; status operates on the result.
func TestE2E_InitGeneratesStructure(t *testing.T) {
	dir := t.TempDir()
	// 用户级反向断言要求本用例独占宿主目录（TestMain 的 CLAUDE_CONFIG_DIR
	// 全包共享，别的用例 integrations enable 会先写入）。
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	git(t, dir, "init", "-b", "master")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)
	forge(t, dir, "init")

	if !fileExists(t, dir, filepath.Join("spec", "forge.yaml")) {
		t.Fatal("forge init did not generate spec/forge.yaml")
	}
	if !fileExists(t, dir, filepath.Join("spec", "changes", "archive", ".gitkeep")) {
		t.Error("forge init did not generate spec/changes/archive/.gitkeep")
	}
	if !fileExists(t, dir, "AGENTS.md") {
		t.Error("forge init did not generate AGENTS.md")
	}

	// 反向断言：init 不再写 DataDir / 用户级宿主资产——宿主接线是
	// `forge integrations enable` 的显式 opt-in（阶段 C 判据）。
	dataDir := forgedata.DataDirFor(dir)
	for _, p := range []string{"hooks", ".sync-version", "protocol.yml"} {
		if fileExists(t, dataDir, p) {
			t.Errorf("forge init must not generate DataDir/%s (host wiring is opt-in)", p)
		}
	}
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if fileExists(t, claudeHome, "settings.json") {
		t.Error("forge init must not wire user-level settings.json")
	}
	if fileExists(t, claudeHome, "CLAUDE.md") {
		t.Error("forge init must not write user-level CLAUDE.md")
	}

	// 零项目写入 + 反向断言：废弃产物不再生成（防回归——曾有
	// pipeline.yml/state.json/forge-pipeline 与 .forge/.claude 项目资产）。
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

	// Phase C：init 只声明仓库契约，宿主接线是显式 opt-in（integrations enable）。
	// 两个后端一次接线，等价于旧的 init --agents claude-code,cursor。
	forge(t, dir, "init")
	forge(t, dir, "integrations", "enable", "claude-code", "cursor")

	// claude-code baseline: user-level settings.json wires the forge hooks
	// (CLAUDE_CONFIG_DIR is TestMain-isolated). cursor: user-level
	// ~/.cursor/hooks.json (HOME is TestMain-isolated) — the proof the explicit
	// cursor host ran through integrations enable → TranslateForAgents →
	// CursorTranslator.
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if settings := readFile(t, claudeHome, "settings.json"); !strings.Contains(settings, "forge hook") {
		t.Error("init --agents claude-code,cursor: user-level settings.json missing forge hooks")
	}
	cursorHooks := readFile(t, os.Getenv("HOME"), ".cursor/hooks.json")
	// W0.2 batch 接线：cursor 渠道收敛为每事件一条 batch 单入口。
	if !strings.Contains(cursorHooks, "forge hook batch --event") {
		t.Error("init --agents claude-code,cursor: user-level cursor hooks.json missing forge wiring (batch entry)")
	}

	// Zero project writes for either backend.
	//
	// 两个后端都零项目写入。
	for _, p := range []string{".claude", ".cursor"} {
		if fileExists(t, dir, p) {
			t.Errorf("integrations enable must not write %s into the project (zero-project-write)", p)
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

	// Phase C：init 声明仓库契约 + integrations enable 显式接线 codex。
	forge(t, dir, "init")
	forge(t, dir, "integrations", "enable", "codex")

	data, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "hooks.json"))
	if err != nil {
		t.Fatalf("integrations enable codex did not generate user-level $CODEX_HOME/hooks.json: %v", err)
	}
	content := string(data)
	for _, want := range []string{`"PreToolUse"`, `"PostToolUse"`, `"Stop"`, "forge hook batch --event PreToolUse", "forge hook batch --event Stop"} {
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
