package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustMkdir(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestCheckGlobalForge_RealLayout 钉住 refactor-data-home 后的真实布局：
// 只检查 <root>/projects/（per-project runtime state）。阶段 D 删掉 skills
// 分发后 skills-cache 不再由任何命令创建，不再检查（否则是永远修不好的 warning）。
func TestCheckGlobalForge_RealLayout(t *testing.T) {
	// 首段用「不存在的根」——t.TempDir() 本身已存在，Stat 会成功。
	forgeRoot := filepath.Join(t.TempDir(), "no-forge-root")

	// No forge root at all → 1 error.
	var e, w int
	checkGlobalForge(forgeRoot, &e, &w)
	if e != 1 || w != 0 {
		t.Fatalf("missing forge root: want err=1 warn=0, got err=%d warn=%d", e, w)
	}

	// Root exists but projects/ missing → 1 warning, no error.
	forgeRoot = t.TempDir()
	e, w = 0, 0
	checkGlobalForge(forgeRoot, &e, &w)
	if e != 0 || w != 1 {
		t.Fatalf("missing projects: want err=0 warn=1, got err=%d warn=%d", e, w)
	}

	// Full real layout → clean.
	mustMkdir(t, os.MkdirAll(filepath.Join(forgeRoot, "projects"), 0755))
	e, w = 0, 0
	checkGlobalForge(forgeRoot, &e, &w)
	if e != 0 || w != 0 {
		t.Fatalf("full layout: want clean, got err=%d warn=%d", e, w)
	}
}

// TestSystemStatus_ForgeDataHome 钉死 forge system 检查的是 FORGE_DATA_HOME 指向的
// 根而非硬编码 ~/.forge（2026-09 代码普查 R4：曾因 UserHomeDir 直拼让逃生舱口
// 失灵——用户设置 FORGE_DATA_HOME 后 forge system 检查错目录）。判别手段：捕获
// stdout，断言输出中出现自定义根路径；HOME 一并指到空目录，隔离 checkOrphanHooks
// 对真实 ~/.claude 的依赖。
func TestSystemStatus_ForgeDataHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", root)
	t.Setenv("HOME", t.TempDir())
	mustMkdir(t, os.MkdirAll(filepath.Join(root, "projects"), 0755))

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := runSystemStatus()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)

	if runErr != nil {
		t.Fatalf("runSystemStatus with FORGE_DATA_HOME root failed: %v\noutput:\n%s", runErr, out)
	}
	if !strings.Contains(string(out), root) {
		t.Errorf("forge system did not inspect FORGE_DATA_HOME root %q — output:\n%s", root, out)
	}
}
