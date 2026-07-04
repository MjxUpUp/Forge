package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// chdirAndRestore 切 cwd 到 dir 并在测试结束恢复。forge data-dir 依赖 os.Getwd
// 解析 DataDir，故测试必须切到构造的临时目录跑。
func chdirAndRestore(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// TestDataDirCmd_NonGitFallback 验证 forge data-dir 在非 git 目录回退 <cwd>/.forge。
// 这是 hook bash 的 fallback 契约基础：TaskVerifyHook / file-sentinel 在非 git 项目
// 仍要把 runtime state（checklog/throttle/quarantine）写到 <cwd>/.forge，与 forgedata
// 的 DataDirFor 非 git 回退语义一致——hook 和 Go store 必须落在同一路径，否则断链。
func TestDataDirCmd_NonGitFallback(t *testing.T) {
	dir := t.TempDir()
	chdirAndRestore(t, dir)

	var buf bytes.Buffer
	dataDirCmd.SetOut(&buf)
	if err := dataDirCmd.RunE(dataDirCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	want := filepath.Join(dir, ".forge")
	if got != want {
		t.Errorf("data-dir non-git = %q, want %q", got, want)
	}
}

// TestDataDirCmd_GitProject 验证 git 项目输出用户级 DataDir（refactor-data-home）。
// TaskVerifyHook / file-sentinel 调 forge data-dir 算 checklog/throttle/quarantine 路径——
// git 项目必须落到 ~/.forge/projects/<key>/，与 Go store（checklog.LoadForTask 等）一致。
// 非 git 回退会与 <cwd>/.forge 巧合重合掩盖分叉，故 mux 断言：DataDir 必须与 <dir>/.forge
// 分叉，否则测试无意义（git init 未生效或 Key 退化）。
func TestDataDirCmd_GitProject(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	chdirAndRestore(t, dir)

	var buf bytes.Buffer
	dataDirCmd.SetOut(&buf)
	if err := dataDirCmd.RunE(dataDirCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got == filepath.Join(dir, ".forge") {
		t.Fatalf("data-dir fell back to <dir>/.forge; git init did not make it diverge — test is moot")
	}
	if !strings.Contains(got, filepath.Join("projects")) {
		t.Errorf("data-dir git = %q, want under <home>/projects/<key>/", got)
	}
	// 与 forgedata.DataDirFor 一致（单一真相源——hook 和 Go store 都派生自它）。
	want := forgedata.DataDirFor(dir)
	if got != want {
		t.Errorf("data-dir = %q, want DataDirFor = %q", got, want)
	}
}
