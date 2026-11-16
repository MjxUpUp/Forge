package protocol

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// writeDataDirProtocol 把 content 写成用户级 DataDir 副本（<DataDir>/protocol.yml）。
func writeDataDirProtocol(t *testing.T, dir, content string) {
	t.Helper()
	p := filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeProjectProtocol 把 content 写成项目级覆盖（<dir>/.forge/protocol.yml）。
func writeProjectProtocol(t *testing.T, dir, content string) {
	t.Helper()
	p := filepath.Join(dir, ".forge", "protocol.yml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureStderr 捕获 fn 期间写到 os.Stderr 的内容。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	return <-done
}

func TestLoad_Missing(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("missing protocol.yml should error")
	} else if !strings.Contains(err.Error(), "protocol.yml not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_ValidProtocolSilent(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	writeDataDirProtocol(t, dir, "version: v1\nstandards:\n  - id: s1\n    name: S1\n    severity: warning\n    enabled: true\n")

	var p *Protocol
	out := captureStderr(t, func() {
		var err error
		p, err = Load(dir)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
	})
	if out != "" {
		t.Errorf("valid protocol should be silent, got stderr %q", out)
	}
	if p.Version != "v1" || len(p.Standards) != 1 || p.Standards[0].ID != "s1" {
		t.Errorf("unexpected protocol: %+v", p)
	}
}

func TestLoad_ProjectLevelOverrideWins(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	writeDataDirProtocol(t, dir, "version: data-dir\n")
	writeProjectProtocol(t, dir, "version: project-level\n")

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Version != "project-level" {
		t.Errorf("project-level override should win, got %q", p.Version)
	}
}

func TestLoad_SeverityNormalizedAndWarned(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	writeDataDirProtocol(t, dir, "version: v1\nstandards:\n  - id: s1\n    severity: ' ERROR '\n    enabled: true\n  - id: s2\n    severity: bogus\n    enabled: true\n")

	var p *Protocol
	out := captureStderr(t, func() {
		var err error
		p, err = Load(dir)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
	})
	if p.Standards[0].Severity != "error" {
		t.Errorf("severity should be normalized to error, got %q", p.Standards[0].Severity)
	}
	if p.Standards[1].Severity != "bogus" {
		t.Errorf("unknown severity should be kept verbatim, got %q", p.Standards[1].Severity)
	}
	if !strings.Contains(out, "已规范化为") || !strings.Contains(out, "不在 info/warning/error 集合内") {
		t.Errorf("expected normalization and unknown-severity warnings, got %q", out)
	}
}

func TestLoad_MissingEnabledWarns(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	writeDataDirProtocol(t, dir, "version: v1\nstandards:\n  - id: s1\n    severity: warning\n")

	out := captureStderr(t, func() {
		if _, err := Load(dir); err != nil {
			t.Fatalf("Load: %v", err)
		}
	})
	if !strings.Contains(out, "漏写 enabled") {
		t.Errorf("expected missing-enabled warning, got %q", out)
	}
}

func TestLoad_CrossRepoImpactField(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	writeDataDirProtocol(t, dir, "version: v1\ncross_repo_impact: required\n")

	p, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.CrossRepoImpact != "required" {
		t.Errorf("CrossRepoImpact = %q, want required", p.CrossRepoImpact)
	}
}

func TestConfigured(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	if Configured(dir) {
		t.Error("no protocol.yml anywhere should report false")
	}
	writeDataDirProtocol(t, dir, "version: v1\n")
	if !Configured(dir) {
		t.Error("DataDir copy should report true")
	}
	writeProjectProtocol(t, dir, "version: v2\n")
	if !Configured(dir) {
		t.Error("project-level override should report true")
	}
}

func TestDataDirUnresolvableFailFast(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, ``)
	// 全平台破坏 os.UserHomeDir：unix 读 HOME；Windows 读 USERPROFILE，再读
	// HOMEDRIVE+HOMEPATH。
	t.Setenv(`HOME`, ``)
	t.Setenv(`USERPROFILE`, ``)
	t.Setenv(`HOMEDRIVE`, ``)
	t.Setenv(`HOMEPATH`, ``)

	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("Load 在 DataDir 不可解析时必须报错，实得 nil")
	}
	if _, err := os.Stat(`protocol.yml`); err == nil {
		t.Fatal("不得在进程 cwd 写入相对路径 protocol.yml")
	}
}
