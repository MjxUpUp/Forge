package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
)

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

// TestPrintInstallReport_Warnings：requires 依赖警告必须走 stderr 且逐条列出。
// 守护 enforce 提示可见性——单装断链场景用户须看到"未同装"警告，否则跨 skill 引用静默断链。
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

// TestPrintInstallReport_NoWarnings：无 Warnings 时 stderr 不打印警告标题（避免误报）。
func TestPrintInstallReport_NoWarnings(t *testing.T) {
	r := &skillsdist.InstallReport{Mode: skillsdist.ModeLink}
	out := captureStderr(t, func() { printInstallReport(r) })
	if strings.Contains(out, `requires 依赖警告`) {
		t.Fatalf(`空 Warnings 不应打印警告标题: %s`, out)
	}
}
