package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
)

// captureStdout temporarily redirects os.Stdout to capture the output of printInstallReport
// (output-layer unit test).
//
// captureStdout 临时重定向 os.Stdout 捕获 printInstallReport 的输出（输出层单测）。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// captureStderr temporarily redirects os.Stderr to capture warning output such as Warnings
// (warnings go to stderr, separated from normal output). defer is registered right after the
// assignment: even if fn panics, Stderr is restored and the pipe is closed (preventing
// pollution of later tests and a half-open pipe).
//
// captureStderr 临时重定向 os.Stderr 捕获 Warnings 等告警输出（告警走 stderr，与正常输出分离）。
// defer 在赋值后立即注册：fn panic 时也保证 Stderr 恢复 + pipe 关闭（防污染后续测试 + pipe 半挂）。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = old
		w.Close()
		r.Close()
	}()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	w.Close() // 触发 goroutine EOF
	<-done    // 等待读取完成
	return buf.String()
}

// TestPrintInstallReport_DriftSkipDetail: drift+skip must list details + give a sync reminder.
//
// TestPrintInstallReport_DriftSkipDetail：drift+skip 必须列明细 + 给出同步提醒。
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

// TestPrintInstallReport_NoDetailForSyncedSkip: a synced-state skip (StateLinked) must not
// print drift details, to avoid noise.
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

// TestParseSkillTargets_CodexCopilot: parseSkillTargets must accept codex/copilot/all and
// reject unknown values. Guards the --target codex|copilot dispatch capability — a missing
// case would make a user's --target codex error out directly, and the codex/copilot detection
// in skills drift-check (which reuses this function) would also fail. Loop-engineering
// multi-agent dispatch (Codex CLI + GitHub Copilot) relies on this parsing.
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
			got, err := parseSkillTargets(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseSkillTargets(%v) 应拒绝，实际成功 got=%v", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSkillTargets(%v) 意外错误: %v", c.in, err)
			}
			if len(got) != c.wantLen {
				t.Fatalf("parseSkillTargets(%v) 返回 %d 个 target，want %d", c.in, len(got), c.wantLen)
			}
		})
	}
}

// TestParseSkillTargets_EmptyDefaultsClaude: empty input defaults to claude (the contract
// for the CLI --target default value).
//
// TestParseSkillTargets_EmptyDefaultsClaude：空入参默认 claude（CLI --target 默认值的契约）。
func TestParseSkillTargets_EmptyDefaultsClaude(t *testing.T) {
	got, err := parseSkillTargets(nil)
	if err != nil {
		t.Fatalf("空入参不应报错: %v", err)
	}
	if len(got) != 1 || string(got[0]) != "claude" {
		t.Fatalf("空入参应默认 [claude]，got %v", got)
	}
}

// TestPrintInstallReport_Warnings: requires dependency warnings must go to stderr and be
// listed one by one. Guards enforce-hint visibility — in a single-install broken-link
// scenario the user must see the not-installed-together warning, otherwise cross-skill
// references break silently.
//
// TestPrintInstallReport_Warnings：requires 依赖警告必须走 stderr 且逐条列出。
// 守护 enforce 提示可见性——单装断链场景用户须看到「未同装」警告，否则跨 skill 引用静默断链。
func TestPrintInstallReport_Warnings(t *testing.T) {
	r := &skillsdist.InstallReport{
		Mode: skillsdist.ModeLink,
		Warnings: []string{
			`design-artifact-standards: requires code-review-gate 但本次未同装（跨 skill 引用可能断链）`,
			`foo: requires ghost 不在 canonical（requires 声明无效，可能笔误或目标 skill 已移除）`,
		},
	}
	out := captureStderr(t, func() { printInstallReport(r) })
	if !strings.Contains(out, `requires 依赖警告`) {
		t.Fatalf(`警告标题缺失: %s`, out)
	}
	if !strings.Contains(out, `design-artifact-standards: requires code-review-gate`) {
		t.Fatalf(`第一条警告缺失: %s`, out)
	}
	if !strings.Contains(out, `foo: requires ghost`) {
		t.Fatalf(`第二条警告缺失: %s`, out)
	}
}

// TestPrintInstallReport_NoWarnings: with no Warnings, stderr must not print the warning
// title (avoid false positives).
//
// TestPrintInstallReport_NoWarnings：无 Warnings 时 stderr 不打印警告标题（避免误报）。
func TestPrintInstallReport_NoWarnings(t *testing.T) {
	r := &skillsdist.InstallReport{Mode: skillsdist.ModeLink}
	out := captureStderr(t, func() { printInstallReport(r) })
	if strings.Contains(out, `requires 依赖警告`) {
		t.Fatalf(`空 Warnings 不应打印警告标题: %s`, out)
	}
}

// TestResolveInstallScope pins the shared --global/--project resolution:
// --project overrides --global; project scope outside a forge project errors
// with the actual flag combination in the message (not a hardcoded
// "--project") and wraps the resolution failure.
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

	// --global=false alone also means project scope; the error must name
	// --global=false (the flag the user actually passed).
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
