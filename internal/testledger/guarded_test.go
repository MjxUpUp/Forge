package testledger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/testledger"
)

// TestGuardedSkipNotSilentDeletion：if 里的 t.Skip 是环境闸门，不是被关掉的测试——
// 它不该进 skipped 计数，也不该要求审批；无条件的 Skip 才需要。
func TestGuardedSkipNotSilentDeletion(t *testing.T) {
	now := time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)
	decls := []testledger.Decl{
		{Test: "TestGuarded", Package: "p/a", File: "a_test.go", Line: 1, HasSkip: true, SkipGuarded: true, SkipReason: "只在 Windows 上成立"},
		{Test: "TestBare", Package: "p/a", File: "a_test.go", Line: 2, HasSkip: true},
	}
	l := testledger.Build(decls, nil, testledger.Options{MaxAge: time.Hour, Now: now})
	if got := l.Counts[testledger.StateSkipped]; got != 1 {
		t.Fatalf("条件跳过不该算进 skipped：want 1（只剩无条件那条），got %d", got)
	}
	var guarded *testledger.Entry
	for i := range l.Entries {
		if l.Entries[i].Test == "TestGuarded" {
			guarded = &l.Entries[i]
		}
	}
	if guarded == nil || guarded.State != testledger.StateOrphaned || !guarded.GuardedSkip {
		t.Fatalf("条件跳过的测试应记为 orphaned + GuardedSkip：%+v", guarded)
	}
	v := strings.Join(l.Violations(), "\n")
	if strings.Contains(v, "TestGuarded") {
		t.Errorf("条件跳过不该进违规清单：%s", v)
	}
	if !strings.Contains(v, "TestBare") {
		t.Errorf("无条件 Skip 仍须进违规清单：%s", v)
	}
}

// TestScanDeclsMarksGuardedSkip 钉住 AST 判定：if 里的 Skip 是 guarded，
// 函数体开头无条件的 Skip 不是；同一函数里两者都有时整体不算 guarded。
func TestScanDeclsMarksGuardedSkip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "internal", "x")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package x\n\nimport (\n\t\"runtime\"\n\t\"testing\"\n)\n\n" +
		"func TestGuarded(t *testing.T) {\n\tif runtime.GOOS != \"windows\" {\n\t\tt.Skip(\"保留名只在 Windows 上成立\")\n\t}\n}\n\n" +
		"func TestBare(t *testing.T) {\n\tt.Skip(\"需要真实设备\")\n}\n\n" +
		"func TestMixed(t *testing.T) {\n\tt.Skip(\"先关掉\")\n\tif testing.Short() {\n\t\tt.Skip(\"短模式\")\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(src, "x_test.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	decls, err := testledger.ScanDecls(dir)
	if err != nil {
		t.Fatalf("ScanDecls: %v", err)
	}
	got := map[string]testledger.Decl{}
	for _, d := range decls {
		got[d.Test] = d
	}
	if !got["TestGuarded"].SkipGuarded {
		t.Errorf("if 里的 Skip 应判为条件跳过：%+v", got["TestGuarded"])
	}
	if got["TestBare"].SkipGuarded {
		t.Errorf("无条件 Skip 不该判为条件跳过：%+v", got["TestBare"])
	}
	if got["TestMixed"].SkipGuarded {
		t.Errorf("同一函数里有无条件 Skip 时整体不算条件跳过：%+v", got["TestMixed"])
	}
}
