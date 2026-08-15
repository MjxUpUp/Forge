package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/protocol"
)

// init_test.go — guards for the team-mode hooks double-registration fix and the
// team-mode → default-mode convergence path.
//
// init_test.go —— 团队模式 hooks 双注册修复与团队模式→默认模式收敛路径的守卫。

// setupInitEnv builds a real (git) project with isolated user-level homes, so
// runInitTeamMode/runInitUserLevel never touch the real home.
//
// setupInitEnv 构造真实（git）项目并隔离用户级 home，runInitTeamMode /
// runInitUserLevel 绝不碰真实 home。
func setupInitEnv(t *testing.T) (root, claudeHome string) {
	t.Helper()
	root, _ = forgedatatest.RealProject(t)
	claudeHome = t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("CODEX_HOME", t.TempDir())
	return root, claudeHome
}

// TestRunInitTeamMode_NoProjectLevelSettingsLocal pins the double-registration
// fix: team mode must NOT write a project-level .claude/settings.local.json —
// hooks are registered at USER level (hook commands invoke the forge binary, so
// teammates install forge anyway; a project-level copy would double-run every
// hook against autoSync's user-level registration).
//
// TestRunInitTeamMode_NoProjectLevelSettingsLocal 钉死双注册修复：团队模式不得
// 写项目级 .claude/settings.local.json——hooks 注册在用户级（hook 命令调用
// forge 二进制，队友反正要装 forge；项目级副本会与 autoSync 的用户级注册双跑
// 每条 hook）。
func TestRunInitTeamMode_NoProjectLevelSettingsLocal(t *testing.T) {
	root, claudeHome := setupInitEnv(t)

	if err := runInitTeamMode(root, nil, protocol.DefaultProtocol()); err != nil {
		t.Fatalf("runInitTeamMode: %v", err)
	}

	// Team-mode marker written (exempts the stripper).
	if _, err := os.Stat(filepath.Join(root, ".forge", teamModeMarker)); err != nil {
		t.Errorf("team-mode marker not written: %v", err)
	}
	// No project-level settings.local.json.
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Error("team mode must not write project-level .claude/settings.local.json (double-run)")
	}
	// Hooks registered at USER level instead.
	data, err := os.ReadFile(filepath.Join(claudeHome, "settings.json"))
	if err != nil {
		t.Fatalf("user-level settings.json not written: %v", err)
	}
	if !strings.Contains(string(data), "forge hook task-guard") {
		t.Error("user-level settings.json missing forge hooks")
	}
	// The git-shared instruction layer is still written to the project.
	if _, err := os.Stat(filepath.Join(root, ".claude", "CLAUDE.md")); err != nil {
		t.Error("team mode must still write project-level .claude/CLAUDE.md (git-shared)")
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Error("team mode must still write project-level AGENTS.md (git-shared)")
	}
}

// TestRunInitUserLevel_ConvergesTeamModeProject pins the convergence fix: a
// plain `forge init` on a team-mode project is an explicit switch back to the
// default (zero-project-write) mode — the team-mode marker is removed so the
// stripper can converge project-level residue. Without the removal the marker
// permanently exempted the project.
//
// TestRunInitUserLevel_ConvergesTeamModeProject 钉死收敛修复：在团队模式项目上
// 跑普通 `forge init` = 明确切回默认（零项目写入）模式——team-mode 标记被
// 删除，stripper 才能收敛项目级残留。不删标记则项目被永久豁免。
func TestRunInitUserLevel_ConvergesTeamModeProject(t *testing.T) {
	root, claudeHome := setupInitEnv(t)

	// Seed team-mode state: marker + project-level forge residue.
	forgeDir := filepath.Join(root, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(forgeDir, teamModeMarker)
	if err := os.WriteFile(marker, []byte("v0.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	settingsLocal := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge hook bash-guard"}]}]}}`
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.local.json"), []byte(settingsLocal), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runInitUserLevel(root, nil, protocol.DefaultProtocol()); err != nil {
		t.Fatalf("runInitUserLevel: %v", err)
	}

	// Marker removed — the project opted back into default mode.
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("team-mode marker must be removed by plain forge init")
	}
	// Project-level forge hooks converged (settings.local.json stripped to the
	// {} shell by the keepEmpty stripper).
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("settings.local.json shell should be kept: %v", err)
	}
	if strings.Contains(string(data), "forge hook") {
		t.Errorf("project-level forge hooks not converged, got: %s", data)
	}
	// User-level hooks registered.
	userData, err := os.ReadFile(filepath.Join(claudeHome, "settings.json"))
	if err != nil {
		t.Fatalf("user-level settings.json not written: %v", err)
	}
	if !strings.Contains(string(userData), "forge hook task-guard") {
		t.Error("user-level settings.json missing forge hooks")
	}
}

// TestInitCmd_HelpDemotesManualInit pins the Phase-3 help demotion: `forge init`'s
// Long help must tell plugin users they normally do NOT need to run it manually
// (init-suggest auto-takeover covers git projects), and position manual init as
// repair / non-plugin / team-mode. Guards help-vs-behavior drift: the hook gained
// the auto-takeover branch (feat/auto-takeover-init), so a stale help that still
// frames init as the required per-project step would send plugin users through a
// redundant manual ritual.
//
// TestInitCmd_HelpDemotesManualInit 钉死 Phase 3 的 help 降级：`forge init` 的
// Long 帮助须告知 plugin 用户通常无需手动跑（init-suggest 自动接管覆盖 git
// 项目），并把手动 init 定位为修复/非 plugin/团队模式。守护 help 与行为不
// 漂移：hook 已加自动接管分支（feat/auto-takeover-init），过时帮助仍把 init
// 描述成每项目必做步骤，会让 plugin 用户走多余的重复仪式。
func TestInitCmd_HelpDemotesManualInit(t *testing.T) {
	for _, sub := range []string{
		`无需手动跑本命令`,
		`init-suggest`,
		`plugin`,
		`forge suggest decline`,
		`修复`,
		`团队模式`,
	} {
		if !strings.Contains(initCmd.Long, sub) {
			t.Errorf("initCmd.Long 应含 %q（plugin 用户免手动 init 的降级文案），实得:\n%s", sub, initCmd.Long)
		}
	}
}
