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
