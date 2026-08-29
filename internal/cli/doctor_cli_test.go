package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/doctor"
)

// isolateDoctorEnv 把所有 agent-bridge 路径 helper + PATH 重定向进临时根，让
// `forge doctor` 密封运行：10 个 host 全部 missing，不探测任何真实二进制。
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

// TestDoctorCmd_Text human 输出：隔离环境下 10 host 全 missing、标题含版本、无 PATH 段
// （PATH 无 forge 可执行文件时不打印多副本警告）。
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
	for _, h := range []string{"claude-code", "codex", "cursor", "windsurf", "kimi", "reasonix", "codebuddy", "cline", "opencode", "zcode"} {
		if !strings.Contains(out, h) {
			t.Errorf("输出应含 host %s，实得 %q", h, out)
		}
	}
	if strings.Contains(out, "PATH 上有") {
		t.Errorf("隔离环境 PATH 无 forge，不应打印多副本段，实得 %q", out)
	}
}

// TestDoctorCmd_JSON --json 输出合法 JSON 且 Report 形状正确（self_version + 10 hosts）。
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
	if len(rep.Hosts) != 10 {
		t.Fatalf("应含 10 个 host，got %d", len(rep.Hosts))
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

// TestSkillsDriftProbe_GatesUninstalledTargets 钉死 M-3 修复：doctor 只审计 agent home
// 存在的目标。隔离环境里只有 ~/.claude 时，探针必须把 cursor/codex/copilot/agents
// 记入 Skipped（未安装——不是成墙的 missing），同时仍审计 claude（已安装 agent 的
// 真实缺口保持可见）。
func TestSkillsDriftProbe_GatesUninstalledTargets(t *testing.T) {
	root := isolateDoctorEnv(t)
	// Only claude's agent home exists; the other four targets are uninstalled.
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := skillsDriftProbe()
	if s == nil {
		t.Fatal("probe 返回 nil")
	}
	if s.Error != "" {
		t.Fatalf("探针不应报错: %s", s.Error)
	}
	wantSkipped := []string{"agents", "codex", "copilot", "cursor"}
	if !reflect.DeepEqual(s.Skipped, wantSkipped) {
		t.Errorf("Skipped = %v, want %v（claude 已装不得跳过）", s.Skipped, wantSkipped)
	}
	// claude audited: real gap visible (canonical skills vs ~/.claude without a skills
	// dir → missing > 0). Not asserting an exact count — canonical size varies.
	if s.Missing == 0 {
		t.Errorf("已安装的 claude 目标仍应被审计（~/.claude 无 skills 目录 → missing>0），实得 missing=%d", s.Missing)
	}
	for _, it := range s.Items {
		if it.Target != "claude" {
			t.Errorf("未安装目标 %s 不应产生条目（噪声门控被破坏）: %+v", it.Target, it)
		}
	}
}

// TestSkillsDriftProbe_DamagedHomeSurfaces pins the L-5 follow-up: an agent home that
// is a regular FILE (damage, not uninstalled) must surface in TargetErrors (⚠ line)
// — never silently folded into Skipped where the renderer would call it 未安装.
// Auditing it would produce a drift wall off a broken state, so it is skipped from
// the audit too — but as a visible warning, not a masquerading skip.
//
// TestSkillsDriftProbe_DamagedHomeSurfaces 钉死 L-5 后续：agent home 是普通文件
// （损坏态、非未安装）必须浮出到 TargetErrors（⚠ 行）——绝不静默折进 Skipped
// 被渲染器称作未安装。审计它会在破坏态上产出成墙 drift，所以同样不审计——
// 但作为可见警告而非伪装跳过。
func TestSkillsDriftProbe_DamagedHomeSurfaces(t *testing.T) {
	root := isolateDoctorEnv(t)
	// Only ~/.claude exists as a real dir; ~/.cursor is a regular FILE (damaged).
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cursor"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := skillsDriftProbe()
	if s == nil {
		t.Fatal("probe 返回 nil")
	}
	if s.Error != "" {
		t.Fatalf("探针不应报错: %s", s.Error)
	}
	// Damaged cursor must NOT be in Skipped (it is not 未安装)…
	for _, name := range s.Skipped {
		if name == "cursor" {
			t.Errorf("损坏态 cursor 不得进 Skipped（未安装语义），Skipped=%v", s.Skipped)
		}
	}
	// …it must surface as a TargetError naming the damage.
	found := false
	for _, e := range s.TargetErrors {
		if strings.Contains(e, "cursor") && strings.Contains(e, "损坏态") {
			found = true
		}
	}
	if !found {
		t.Errorf("损坏态 cursor 应产出指名损坏的 TargetError，实得 %v", s.TargetErrors)
	}
	// And it must produce no audit items (no drift wall off a broken state).
	for _, it := range s.Items {
		if it.Target == "cursor" {
			t.Errorf("损坏态目标不应被审计（不得产出条目）: %+v", it)
		}
	}
}

// TestPrintDoctorSkillsSection_SkippedStates 钉死 M-3 的渲染：全跳过是独立状态（零
// 覆盖绝不能渲染成绿色 in-sync——H-1 伪装模式）；部分跳过在正常判定旁展示 advisory 行。
func TestPrintDoctorSkillsSection_SkippedStates(t *testing.T) {
	// 全跳过：独立状态行，绝不渲染 in sync。
	var buf bytes.Buffer
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{
		Skipped: []string{"agents", "codex", "copilot", "cursor"},
	}})
	out := buf.String()
	if !strings.Contains(out, "无已安装目标可审计") {
		t.Errorf("全跳过应渲染独立状态行，实得 %q", out)
	}
	if strings.Contains(out, "in sync") {
		t.Errorf("全跳过（零覆盖）绝不能渲染 in sync，实得 %q", out)
	}

	// 部分跳过：正常判定照常 + 跳过行可见。
	buf.Reset()
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{
		Linked:  40,
		Skipped: []string{"codex", "cursor"},
	}})
	out = buf.String()
	if !strings.Contains(out, "in sync") {
		t.Errorf("部分跳过时已审计目标判定照常输出，实得 %q", out)
	}
	if !strings.Contains(out, "跳过未安装目标") || !strings.Contains(out, "codex, cursor") {
		t.Errorf("部分跳过应有跳过 advisory 行（含目标名），实得 %q", out)
	}

	// 全损坏（Skipped 空、TargetErrors 有损坏条目）：同样是「无可审计」独立态，
	// 绝不渲染绿色 in sync（H-1 伪装的 damaged-only 变体）。
	buf.Reset()
	doctorCmd.SetOut(&buf)
	printDoctor(doctorCmd, doctor.Report{Skills: &doctor.SkillsDriftSummary{
		TargetErrors: []string{"cursor 的 home 路径是普通文件而非目录（损坏态）——跳过审计，请检查该 agent 安装"},
	}})
	out = buf.String()
	if !strings.Contains(out, "无已安装目标可审计") {
		t.Errorf("全损坏应渲染独立状态行，实得 %q", out)
	}
	if strings.Contains(out, "in sync") {
		t.Errorf("全损坏（零覆盖）绝不能渲染 in sync，实得 %q", out)
	}
	if !strings.Contains(out, "⚠") || !strings.Contains(out, "cursor") {
		t.Errorf("全损坏应渲染 ⚠ 损坏行（含目标名），实得 %q", out)
	}
}
