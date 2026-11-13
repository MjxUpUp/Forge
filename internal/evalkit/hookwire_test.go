package evalkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnableHooksInContract_RewritesTopLevelKeys：夹具 opt-in 必须按行改 YAML
// 顶层键，且不能命中模板里以 "# hooks:" 开头的注释行（阶段 A 的 e2e 就因
// 字符串替换先命中注释而静默失败过）。两条一起钉住。
func TestEnableHooksInContract_RewritesTopLevelKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "spec", "forge.yaml")
	body := "# hooks: off 表示只审计不执法\nversion: 1\nprofile: \"mini\"\nhooks: off\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := enableHooksInContract(dir); err != nil {
		t.Fatalf("enableHooksInContract: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "hooks: on\n") {
		t.Errorf("hooks 应被改为 on，got:\n%s", text)
	}
	if !strings.Contains(text, `profile: "standard"`) {
		t.Errorf("profile 应被改为 standard，got:\n%s", text)
	}
	if !strings.Contains(text, "# hooks: off 表示只审计不执法") {
		t.Errorf("注释行不应被改写，got:\n%s", text)
	}
}

// TestEnableHooksInContract_MissingFileIsNoop：桩 forge 夹具不写契约时视为宿主层
// 夹具，跳过 opt-in；但契约存在却缺键时必须报错（静默降级会造出假绿）。
func TestEnableHooksInContract_MissingFileIsNoop(t *testing.T) {
	if err := enableHooksInContract(t.TempDir()); err != nil {
		t.Fatalf("missing contract should be a no-op, got %v", err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec", "forge.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := enableHooksInContract(dir); err == nil {
		t.Fatal("contract without hooks/profile keys must fail loudly")
	}
}
