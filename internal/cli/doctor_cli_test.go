package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/doctor"
)

// isolateDoctorEnv redirects every agent-bridge path helper + PATH into a temp root so
// `forge doctor` runs hermetically: all 9 hosts report missing, no real binary is probed.
//
// isolateDoctorEnv 把所有 agent-bridge 路径 helper + PATH 重定向进临时根，让
// `forge doctor` 密封运行：9 个 host 全部 missing，不探测任何真实二进制。
func isolateDoctorEnv(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("APPDATA", filepath.Join(root, "AppData", "Roaming"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, ".codex"))
	t.Setenv("KIMI_CODE_HOME", filepath.Join(root, ".kimi-code"))
	t.Setenv("REASONIX_HOME", filepath.Join(root, "AppData", "Roaming", "reasonix"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("WORKBUDDY_CONFIG_DIR", filepath.Join(root, ".workbuddy"))
	t.Setenv("PATH", filepath.Join(root, "bin"))
	return root
}

// TestDoctorCmd_Text human 输出：隔离环境下 9 host 全 missing、标题含版本、无 PATH 段
// （PATH 无 forge 可执行文件时不打印多副本警告）。
//
// TestDoctorCmd_Text human output: isolated env → 9 hosts all missing, header carries
// the version, no PATH section (no multi-copy warning when PATH holds no forge binary).
func TestDoctorCmd_Text(t *testing.T) {
	isolateDoctorEnv(t)
	var buf bytes.Buffer
	doctorCmd.SetOut(&buf)
	doctorCmd.SetArgs([]string{})
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctor RunE: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "hosts:") {
		t.Errorf("输出应含 hosts: 段，实得 %q", out)
	}
	for _, h := range []string{"claude-code", "codex", "cursor", "windsurf", "kimi", "reasonix", "codebuddy", "cline", "opencode"} {
		if !strings.Contains(out, h) {
			t.Errorf("输出应含 host %s，实得 %q", h, out)
		}
	}
	if strings.Contains(out, "PATH 上有") {
		t.Errorf("隔离环境 PATH 无 forge，不应打印多副本段，实得 %q", out)
	}
}

// TestDoctorCmd_JSON --json 输出合法 JSON 且 Report 形状正确（self_version + 9 hosts）。
//
// TestDoctorCmd_JSON --json emits valid JSON with the right Report shape
// (self_version + 9 hosts).
func TestDoctorCmd_JSON(t *testing.T) {
	isolateDoctorEnv(t)
	var buf bytes.Buffer
	doctorCmd.SetOut(&buf)
	doctorCmd.SetArgs([]string{})
	if err := doctorCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := doctorCmd.Flags().Set("json", "false"); err != nil {
			t.Fatal(err)
		}
	}()
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctor RunE: %v", err)
	}
	var rep doctor.Report
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatalf("--json 输出应为合法 Report JSON: %v\n%s", err, buf.String())
	}
	if len(rep.Hosts) != 9 {
		t.Fatalf("应含 9 个 host，got %d", len(rep.Hosts))
	}
	for _, h := range rep.Hosts {
		if h.Status != doctor.StatusMissing {
			t.Fatalf("隔离环境下 %s 应为 missing，got %q", h.Host, h.Status)
		}
	}
}

// TestPrintDoctorSkillsSection 守卫 doctor 人类可读输出的 skills 分发节：健康态
// 压缩一行（in sync）；missing/drift 态展开条目并给修复命令。2026-08 审计根因：
// canonical 新增 skill 后目标静默缺失（subagent-orchestration 缺 5 host）无任何
// 输出面——doctor 是运营级检查入口，该缝必须在其输出可见。
//
// TestPrintDoctorSkillsSection guards the skills-distribution section of doctor's
// human output: healthy state collapses to one line (in sync); missing/drift
// expands items with the fix command. 2026-08 audit root cause: skills silently
// missing from targets after canonical additions (subagent-orchestration absent
// from 5 hosts) had no output surface — doctor is the ops-grade check, the seam
// must be visible there.
func TestPrintDoctorSkillsSection(t *testing.T) {
	// 健康态：一行 in sync，不展开条目、不给修复行。
	var buf bytes.Buffer
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{Linked: 40, CopySync: 3}})
	out := buf.String()
	if !strings.Contains(out, "skills distribution:") {
		t.Errorf("健康态也应输出 skills distribution: 段，实得 %q", out)
	}
	if !strings.Contains(out, "in sync") {
		t.Errorf("健康态应压缩为 in sync 行，实得 %q", out)
	}
	if strings.Contains(out, "forge skills install") {
		t.Errorf("健康态不应给修复命令，实得 %q", out)
	}

	// 缺口态：条目展开 + 修复命令。
	buf.Reset()
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{
		Linked:  40,
		Missing: 2,
		Drifted: 1,
		Items: []doctor.SkillsDriftItem{
			{Skill: "subagent-orchestration", Target: "claude", State: "missing"},
			{Skill: "old-skill", Target: "codex", State: "drift"},
		},
	}})
	out = buf.String()
	if !strings.Contains(out, "missing=2 drift=1") {
		t.Errorf("缺口态应输出计数 missing=2 drift=1，实得 %q", out)
	}
	if !strings.Contains(out, "subagent-orchestration") || !strings.Contains(out, "[claude]") {
		t.Errorf("缺口态应展开条目（skill+target），实得 %q", out)
	}
	if !strings.Contains(out, "forge skills install --global --target all") {
		t.Errorf("缺口态应给修复命令，实得 %q", out)
	}

	// 错误态（审计没跑成）：绝不渲染 in sync——死探针伪装健康是本节要消灭的
	// 静默缺口本身（复审 H-1）。
	buf.Reset()
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{
		Error: "canonical resolve: no skills found",
	}})
	out = buf.String()
	if !strings.Contains(out, "审计失败") {
		t.Errorf("错误态应输出审计失败行，实得 %q", out)
	}
	if strings.Contains(out, "in sync") {
		t.Errorf("错误态绝不能渲染 in sync 健康断言，实得 %q", out)
	}

	// 部分失败态（审计跑完但 per-target 报错）：计数照常 + ⚠ 警示可见（M-2）。
	buf.Reset()
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{
		Linked:       40,
		TargetErrors: []string{"target cursor: ReadDir permission denied"},
	}})
	out = buf.String()
	if !strings.Contains(out, "in sync") {
		t.Errorf("部分失败态计数仍应输出 in sync 总览，实得 %q", out)
	}
	if !strings.Contains(out, "⚠ target cursor: ReadDir permission denied") {
		t.Errorf("TargetErrors 部分失败应可见，实得 %q", out)
	}

	// 截断标记：>20 条时显示 (+N more)，不被当成全部（复审 L-2）。
	buf.Reset()
	doctorCmd.SetOut(&buf)
	items := make([]doctor.SkillsDriftItem, 25)
	for i := range items {
		items[i] = doctor.SkillsDriftItem{Skill: fmt.Sprintf("skill-%02d", i), Target: "claude", State: "missing"}
	}
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{Missing: 25, Items: items}})
	out = buf.String()
	if !strings.Contains(out, "(+5 more") {
		t.Errorf("截断应显示 (+5 more) 标记，实得 %q", strings.SplitN(out, "\n", 8)[7])
	}
	if !strings.Contains(out, "skill-19") || strings.Contains(out, "skill-20") {
		t.Errorf("截断应保留前 20 条、隐藏第 21+ 条，实得 %q", out)
	}

	// 摘要缺席（probe nil / 老调用方）：整节不输出。
	buf.Reset()
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{})
	if strings.Contains(buf.String(), "skills distribution:") {
		t.Errorf("Skills 为 nil 时不应输出 skills 节，实得 %q", buf.String())
	}
}
