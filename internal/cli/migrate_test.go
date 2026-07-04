package cli

// migrate_test.go —— forge migrate 命令接线守卫（cobra RunE + 输出 + flag）。
// 核心迁移逻辑的单测在 internal/forgedata/migrate_test.go；本文件只钉死命令胶水：
// findProject 接线、Moved/DataDir 输出、--dry-run 不动文件、非 forge 项目报错。
// 中文字符串 raw string 规避 Windows 引号腐蚀。

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// writeMigrateFixture 在 root/.forge/ 下种 runtime（checklog/throttle）+ config（state.json）。
func writeMigrateFixture(t *testing.T, root string) {
	t.Helper()
	for _, rel := range []string{
		filepath.Join(`.forge`, `checklog.jsonl`),
		filepath.Join(`.forge`, `.task-verify-throttle.last`),
		filepath.Join(`.forge`, `state.json`), // config，应留 .forge/
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf(`mkdir %s: %v`, filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(`x`), 0644); err != nil {
			t.Fatalf(`write %s: %v`, path, err)
		}
	}
}

// TestMigrateCmd_PrintsMovedAndDataDir：cobra 接线 + 输出含迁移条目 + DataDir 路径。
func TestMigrateCmd_PrintsMovedAndDataDir(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	writeMigrateFixture(t, root)
	chdirAndRestore(t, root)

	var buf bytes.Buffer
	migrateCmd.SetOut(&buf)
	migrateCmd.SetArgs([]string{})
	if err := migrateCmd.RunE(migrateCmd, nil); err != nil {
		t.Fatalf(`migrate RunE: %v`, err)
	}
	out := buf.String()
	if !strings.Contains(out, `checklog.jsonl`) {
		t.Errorf(`输出应含迁移的 checklog.jsonl，实得 %q`, out)
	}
	if !strings.Contains(out, `DataDir:`) {
		t.Errorf(`输出应含 DataDir 路径，实得 %q`, out)
	}
	// runtime 已迁走，config 留
	if _, err := os.Stat(filepath.Join(root, `.forge`, `checklog.jsonl`)); err == nil {
		t.Errorf(`checklog.jsonl 应已从 .forge/ 迁走`)
	}
	if _, err := os.Stat(filepath.Join(root, `.forge`, `state.json`)); err != nil {
		t.Errorf(`state.json（配置）应留 .forge/，实得 stat err=%v`, err)
	}
}

// TestMigrateCmd_DryRunNoMove：--dry-run 输出标记但源文件仍在 .forge/。
func TestMigrateCmd_DryRunNoMove(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	writeMigrateFixture(t, root)
	chdirAndRestore(t, root)

	migrateDryRun = true
	t.Cleanup(func() { migrateDryRun = false })

	var buf bytes.Buffer
	migrateCmd.SetOut(&buf)
	if err := migrateCmd.RunE(migrateCmd, nil); err != nil {
		t.Fatalf(`migrate RunE: %v`, err)
	}
	out := buf.String()
	if !strings.Contains(out, `dry-run`) {
		t.Errorf(`dry-run 输出应含标记，实得 %q`, out)
	}
	if _, err := os.Stat(filepath.Join(root, `.forge`, `checklog.jsonl`)); err != nil {
		t.Errorf(`dry-run 不应移动源文件：%v`, err)
	}
}

// TestMigrateCmd_NotInForgeProject：非 forge 项目（无 .git/.forge）findProject 报错。
func TestMigrateCmd_NotInForgeProject(t *testing.T) {
	tmp := t.TempDir() // 无 .git 无 .forge
	chdirAndRestore(t, tmp)

	err := migrateCmd.RunE(migrateCmd, nil)
	if err == nil {
		t.Fatal(`非 forge 项目应返错（findProject 失败）`)
	}
}
