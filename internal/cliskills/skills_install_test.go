package cliskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
)

func TestPrintInstallReport_DriftSkipDetail(t *testing.T) {
	r := &skillsdist.InstallReport{
		Mode: skillsdist.ModeLink,
		Skills: []skillsdist.SkillInstallResult{{
			Name: "my-skill",
			Targets: []skillsdist.TargetResult{{
				Target: "claude", State: skillsdist.StateDrift, Action: "skipped",
			}},
		}},
	}
	out := captureStdout(t, func() { printInstallReport(r) })
	if !strings.Contains(out, "检测到本地改动，已保留未覆盖") {
		t.Fatalf("drift-skip 明细缺失: %s", out)
	}
	if !strings.Contains(out, "保留了你的本地改动") {
		t.Fatalf("同步提醒缺失: %s", out)
	}
}

// TestPrintInstallReport_BackupDetail: overwrite backup must print the backup path.
//
// TestPrintInstallReport_BackupDetail：overwrite 备份必须打印留底路径。
func TestPrintInstallReport_BackupDetail(t *testing.T) {
	r := &skillsdist.InstallReport{
		Mode: skillsdist.ModeLink,
		Skills: []skillsdist.SkillInstallResult{{
			Name: "my-skill",
			Targets: []skillsdist.TargetResult{{
				Target: "claude", Action: "linked", Backup: "/tmp/bk/my-skill",
			}},
		}},
	}
	out := captureStdout(t, func() { printInstallReport(r) })
	if !strings.Contains(out, "旧版本已备份") || !strings.Contains(out, "/tmp/bk/my-skill") {
		t.Fatalf("备份明细缺失: %s", out)
	}
}

// TestPrintInstallReport_NoDetailForSyncedSkip: a synced-state skip (StateLinked) must not print drift details, to avoid noise.
//
// TestPrintInstallReport_NoDetailForSyncedSkip：同步态 skip（StateLinked）不该打印 drift 明细，避免打扰。
func TestPrintInstallReport_NoDetailForSyncedSkip(t *testing.T) {
	r := &skillsdist.InstallReport{
		Mode: skillsdist.ModeLink,
		Skills: []skillsdist.SkillInstallResult{{
			Name: "my-skill",
			Targets: []skillsdist.TargetResult{{
				Target: "claude", State: skillsdist.StateLinked, Action: "skipped",
			}},
		}},
	}
	out := captureStdout(t, func() { printInstallReport(r) })
	if strings.Contains(out, "检测到本地改动") {
		t.Fatalf("同步 skip 不应打印 drift 明细: %s", out)
	}
}

// TestParseSkillTargets_CodexCopilot: parseSkillTargets must accept codex/copilot/all and reject unknown values.
//
// TestParseSkillTargets_CodexCopilot：parseSkillTargets 必须接受 codex/copilot/all 并拒绝未知值。
// 守护 --target codex|copilot 分发能力——case 漏写会让用户 --target codex 直接报错，
// 且 skills drift-check（复用本函数）的 codex/copilot 检测一并失效。
// loop engineering 多 agent 分发（Codex CLI + GitHub Copilot）依赖此解析。
func TestParseSkillTargets_CodexCopilot(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		wantLen int
		wantErr bool
	}{
		{"codex", []string{"codex"}, 1, false},
		{"copilot", []string{"copilot"}, 1, false},
		{"agents", []string{"agents"}, 1, false},
		{"codex+copilot", []string{"codex", "copilot"}, 2, false},
		{"all", []string{"all"}, 1, false},
		{"claude", []string{"claude"}, 1, false},
		{"cursor", []string{"cursor"}, 1, false},
		{"unknown", []string{"unknown-tool"}, 0, true},
		{"mixed valid+unknown rejects all", []string{"claude", "bogus"}, 0, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseSkillTargets(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseSkillTargets(%v) 应拒绝，实际成功 got=%v", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSkillTargets(%v) 意外错误: %v", c.in, err)
			}
			if len(got) != c.wantLen {
				t.Fatalf("ParseSkillTargets(%v) 返回 %d 个 target，want %d", c.in, len(got), c.wantLen)
			}
		})
	}
}

// TestParseSkillTargets_EmptyDefaultsClaude: empty input defaults to claude (the contract for the CLI --target default value).
//
// TestParseSkillTargets_EmptyDefaultsClaude：空入参默认 claude（CLI --target 默认值的契约）。
func TestParseSkillTargets_EmptyDefaultsClaude(t *testing.T) {
	got, err := ParseSkillTargets(nil)
	if err != nil {
		t.Fatalf("空入参不应报错: %v", err)
	}
	if len(got) != 1 || string(got[0]) != "claude" {
		t.Fatalf("空入参应默认 [claude]，got %v", got)
	}
}

// TestPrintInstallReport_Warnings: requires dependency warnings must go to stderr and be listed one by one.
//
// TestPrintInstallReport_Warnings：requires 依赖警告必须走 stderr 且逐条列出。
// 守护 enforce 提示可见性——单装断链场景用户须看到「未同装」警告，否则跨 skill 引用静默断链。
func TestPrintInstallReport_Warnings(t *testing.T) {
	r := &skillsdist.InstallReport{
		Mode: skillsdist.ModeLink,
		Warnings: []string{
			`design-artifact-standards: requires doc-review 但本次未同装（跨 skill 引用可能断链）`,
			`foo: requires ghost 不在 canonical（requires 声明无效，可能笔误或目标 skill 已移除）`,
		},
	}
	out := captureStderr(t, func() { printInstallReport(r) })
	if !strings.Contains(out, `requires 依赖警告`) {
		t.Fatalf(`警告标题缺失: %s`, out)
	}
	if !strings.Contains(out, `design-artifact-standards: requires doc-review`) {
		t.Fatalf(`第一条警告缺失: %s`, out)
	}
	if !strings.Contains(out, `foo: requires ghost`) {
		t.Fatalf(`第二条警告缺失: %s`, out)
	}
}

// TestPrintInstallReport_NoWarnings: with no Warnings, stderr must not print the warning title (avoid false positives).
//
// TestPrintInstallReport_NoWarnings：无 Warnings 时 stderr 不打印警告标题（避免误报）。
func TestPrintInstallReport_NoWarnings(t *testing.T) {
	r := &skillsdist.InstallReport{Mode: skillsdist.ModeLink}
	out := captureStderr(t, func() { printInstallReport(r) })
	if strings.Contains(out, `requires 依赖警告`) {
		t.Fatalf(`空 Warnings 不应打印警告标题: %s`, out)
	}
}

// TestResolveInstallScope pins the shared --global/--project resolution: --project overrides --global; project scope outside a forge project errors with the actual flag combination in the message (not a hardcoded "--project") and wraps the resolution failure.
//
// TestResolveInstallScope 钉住共享的 --global/--project 解析：--project 覆盖
// --global；非 forge 项目内的 project scope 报错，文案含用户实际传的 flag 组合
// （不写死 "--project"），并包裹解析失败 error。
func TestResolveInstallScope(t *testing.T) {
	// Global scope resolves without any project.
	g, dir, err := resolveInstallScope(true, false)
	if err != nil || !g || dir != "" {
		t.Fatalf("global: got (%v, %q, %v), want (true, \"\", nil)", g, dir, err)
	}

	// 需要空注册表——理由同 TestStatusWithoutInit。
	// 单 --global=false 同样指 project scope；报错必须点名 --global=false（用户实际传的 flag）。
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	_, _, err = resolveInstallScope(false, false)
	if err == nil {
		t.Fatal("--global=false outside forge project: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--global=false") {
		t.Fatalf("error should name --global=false, got: %v", err)
	}

	// --project outside a forge project: error names --project.
	_, _, err = resolveInstallScope(true, true)
	if err == nil {
		t.Fatal("--project outside forge project: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--project") {
		t.Fatalf("error should name --project, got: %v", err)
	}

	// --project inside a forge project: project skills dir under .claude/skills.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	g, dir, err = resolveInstallScope(true, true)
	if err != nil || g {
		t.Fatalf("project in forge project: got (%v, %q, %v), want global=false", g, dir, err)
	}
	if dir != filepath.Join(root, ".claude", "skills") {
		t.Fatalf("project dir = %q, want %q", dir, filepath.Join(root, ".claude", "skills"))
	}
}

// TestLoadProjectProfile: global scope → nil regardless of files; project scope without .forge/skills-profile → nil (full set); with profile → allowlist; malformed profile → hard error (silently falling back to full distribution would defeat the trimming intent).
//
// TestLoadProjectProfile：全局范围 → 恒 nil；项目范围无 .forge/skills-profile → nil（全量）；
// 有画像 → 白名单；画像格式错 → 硬错误（静默回退全量会让裁剪落空）。
func TestLoadProjectProfile(t *testing.T) {
	// 全局范围：即便项目内有画像文件也返回 nil。
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge", "skills-profile"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	prof, err := loadProjectProfile(true)
	if err != nil || prof != nil {
		t.Fatalf("global: got (%v, %v), want (nil, nil)", prof, err)
	}

	// 项目范围，无画像文件 → nil。
	noproj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(noproj, ".forge"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(noproj)
	prof, err = loadProjectProfile(false)
	if err != nil || prof != nil {
		t.Fatalf("project no profile: got (%v, %v), want (nil, nil)", prof, err)
	}

	// 项目范围有画像 → 白名单内容。
	t.Chdir(root)
	prof, err = loadProjectProfile(false)
	if err != nil {
		t.Fatalf("project with profile: %v", err)
	}
	if len(prof) != 1 || prof[0] != "alpha" {
		t.Fatalf("profile = %v, want [alpha]", prof)
	}

	// 画像格式错 → 硬错误。
	bad := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bad, ".forge"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, ".forge", "skills-profile"), []byte("has space\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(bad)
	if _, err := loadProjectProfile(false); err == nil {
		t.Fatal("malformed profile: want error, got nil")
	}
}
