package cli

import (
	"bytes"
	"encoding/json"
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
