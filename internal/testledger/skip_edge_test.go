package testledger_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/testledger"
)

// TestBuildMergesSubtestRecords：go test -json 的记录里子测试叫 TestFoo/sub。
// 账本按父测试归并，否则每个 subtest 都会被当成"声明里没有的 phantom"。
func TestBuildMergesSubtestRecords(t *testing.T) {
	now := time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC)
	decls := []testledger.Decl{{Test: "TestFoo", Package: "p/a", File: "a_test.go", Line: 3}}
	recs := []testledger.Record{
		{Package: "p/a", Test: "TestFoo/sub", Action: "pass", At: now.Add(-time.Minute)},
		{Package: "p/a", Test: "TestFoo", Action: "pass", At: now.Add(-2 * time.Minute)},
	}
	l := testledger.Build(decls, recs, testledger.Options{MaxAge: time.Hour, Now: now})
	if got := l.Counts[testledger.StatePhantom]; got != 0 {
		t.Fatalf("子测试不该产生 phantom：got %d，entries=%+v", got, l.Entries)
	}
	if got := l.Counts[testledger.StateLive]; got != 1 {
		t.Fatalf("父测试应为 live：got %d", got)
	}
}

// TestSkipInClosureAndSkipNow：t.Run 闭包里的 Skip 要能看见（按条件跳过），
// SkipNow 不接受理由参数，不能被当成"缺理由"。
func TestSkipInClosureAndSkipNow(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "internal", "x")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package x\n\nimport \"testing\"\n\n" +
		"func TestClosure(t *testing.T) {\n\tt.Run(\"sub\", func(t *testing.T) {\n\t\tif true {\n\t\t\tt.Skip(\"闭包里的闸门\")\n\t\t}\n\t})\n}\n\n" +
		"func TestNow(t *testing.T) {\n\tt.SkipNow()\n}\n"
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
	if d := got["TestClosure"]; !d.HasSkip || !d.SkipGuarded {
		t.Errorf("闭包里的 if Skip 应判为条件跳过：%+v", d)
	}
	if d := got["TestNow"]; !d.HasSkip || d.SkipGuarded || d.SkipReason == "" {
		t.Errorf("SkipNow 应记为无条件跳过且带占位理由：%+v", d)
	}
}

// TestHelperGuardedPropagates：helper 里是无条件 Skip 时，调用它的测试不能
// 被洗成"条件跳过"；helper 的 Skip 在 if 里时才按条件跳过记。
func TestHelperGuardedPropagates(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "internal", "y")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package y\n\nimport \"testing\"\n\n" +
		"func skipHard(t *testing.T) {\n\tt.Skip(\"永远不跑\")\n}\n\n" +
		"func skipSoft(t *testing.T) {\n\tif testing.Short() {\n\t\tt.Skip(\"短模式\")\n\t}\n}\n\n" +
		"func TestHard(t *testing.T) {\n\tskipHard(t)\n}\n\n" +
		"func TestSoft(t *testing.T) {\n\tskipSoft(t)\n}\n"
	if err := os.WriteFile(filepath.Join(src, "y_test.go"), []byte(body), 0o644); err != nil {
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
	if d := got["TestHard"]; !d.HasSkip || d.SkipGuarded {
		t.Errorf("helper 无条件 Skip 应传播为无条件：%+v", d)
	}
	if d := got["TestSoft"]; !d.HasSkip || !d.SkipGuarded {
		t.Errorf("helper 条件 Skip 应传播为条件跳过：%+v", d)
	}
}
