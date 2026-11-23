package testledger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/testledger"
)

// TestClassifyFourStates 用一份手写的声明+记录清单，钉住四态判定的边界：
// 有声明有新鲜记录=live；有声明无记录=orphaned；有 t.Skip*=skipped；
// 记录里存在但源码已无声明=phantom。
func TestClassifyFourStates(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	decls := []testledger.Decl{
		{Test: "TestLive", Package: "example.com/m/internal/a", File: "internal/a/a_test.go", Line: 10},
		{Test: "TestOrphan", Package: "example.com/m/internal/a", File: "internal/a/a_test.go", Line: 20},
		{Test: "TestSkipKnown", Package: "example.com/m/internal/b", File: "internal/b/b_test.go", Line: 30, HasSkip: true, SkipReason: "需要真实设备"},
		{Test: "TestSkipBare", Package: "example.com/m/internal/b", File: "internal/b/b_test.go", Line: 40, HasSkip: true},
	}
	recs := []testledger.Record{
		{Package: "example.com/m/internal/a", Test: "TestLive", Action: "pass", At: now.Add(-2 * time.Hour)},
		{Package: "example.com/m/internal/b", Test: "TestSkipKnown", Action: "skip", At: now.Add(-2 * time.Hour)},
		{Package: "example.com/m/internal/c", Test: "TestGone", Action: "pass", At: now.Add(-2 * time.Hour)},
	}
	l := testledger.Build(decls, recs, testledger.Options{
		MaxAge: 24 * time.Hour,
		Now:    now,
		Approvals: map[string]testledger.SkipApproval{
			"example.com/m/internal/b.TestSkipKnown": {Test: "example.com/m/internal/b.TestSkipKnown", Reason: "需要真实设备", ApprovedBy: "lead"},
		},
	})
	got := map[string]testledger.State{}
	for _, e := range l.Entries {
		got[e.Package+"."+e.Test] = e.State
	}
	want := map[string]testledger.State{
		"example.com/m/internal/a.TestLive":      testledger.StateLive,
		"example.com/m/internal/a.TestOrphan":    testledger.StateOrphaned,
		"example.com/m/internal/b.TestSkipKnown": testledger.StateSkipped,
		"example.com/m/internal/b.TestSkipBare":  testledger.StateSkipped,
		"example.com/m/internal/c.TestGone":      testledger.StatePhantom,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: state=%q want %q", k, got[k], w)
		}
	}
	if l.Counts[testledger.StateLive] != 1 || l.Counts[testledger.StateOrphaned] != 1 || l.Counts[testledger.StateSkipped] != 2 || l.Counts[testledger.StatePhantom] != 1 {
		t.Errorf("counts = %v", l.Counts)
	}
	// 记录过时（超过 MaxAge）不算 live：真相源是「最近跑过」，不是「历史上跑过」。
	old := []testledger.Record{{Package: "example.com/m/internal/a", Test: "TestLive", Action: "pass", At: now.Add(-48 * time.Hour)}}
	l2 := testledger.Build(decls[:1], old, testledger.Options{MaxAge: 24 * time.Hour, Now: now})
	if l2.Entries[0].State != testledger.StateOrphaned {
		t.Errorf("过期记录应判 orphaned，得到 %q", l2.Entries[0].State)
	}
}

// TestLedgerViolations：缺理由、未审批、phantom 三条都要进违规清单；
// 有理由且有审批的 Skip 不违规；live/orphaned 不违规（orphaned 的单列报告）。
func TestLedgerViolations(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	decls := []testledger.Decl{
		{Test: "TestApproved", Package: "p/a", File: "a_test.go", Line: 1, HasSkip: true, SkipReason: "只在 CI 跑"},
		{Test: "TestUnapproved", Package: "p/a", File: "a_test.go", Line: 2, HasSkip: true, SkipReason: "懒得修"},
		{Test: "TestBare", Package: "p/a", File: "a_test.go", Line: 3, HasSkip: true},
		{Test: "TestFine", Package: "p/a", File: "a_test.go", Line: 4},
	}
	recs := []testledger.Record{
		{Package: "p/a", Test: "TestFine", Action: "pass", At: now},
		{Package: "p/a", Test: "TestDeleted", Action: "pass", At: now},
	}
	l := testledger.Build(decls, recs, testledger.Options{
		MaxAge: time.Hour, Now: now,
		Approvals: map[string]testledger.SkipApproval{"p/a.TestApproved": {Test: "p/a.TestApproved", Reason: "只在 CI 跑", ApprovedBy: "lead"}},
	})
	v := strings.Join(l.Violations(), "\n")
	if !strings.Contains(v, "TestBare") || !strings.Contains(v, "缺理由") {
		t.Errorf("缺理由的 Skip 未进违规清单：%s", v)
	}
	if !strings.Contains(v, "TestUnapproved") || !strings.Contains(v, "未审批") {
		t.Errorf("未审批的 Skip 未进违规清单：%s", v)
	}
	if !strings.Contains(v, "TestDeleted") || !strings.Contains(v, "phantom") {
		t.Errorf("phantom 未进违规清单：%s", v)
	}
	if strings.Contains(v, "TestApproved") || strings.Contains(v, "TestFine") {
		t.Errorf("批准过的 Skip / 正常测试不应违规：%s", v)
	}
}

// TestScanDeclsAndApprovals 用临时目录钉住 AST 扫描与审批清单解析，
// 确保「有没有理由」取决于 Skip 参数是否非空，而不是有没有写 Skip。
func TestScanDeclsAndApprovals(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "internal", "x")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package x\n\nimport \"testing\"\n\n" +
		"func TestWithReason(t *testing.T) { t.Skip(\"需要真实设备\") }\n\n" +
		"func TestBareSkip(t *testing.T) { t.Skip() }\n\n" +
		"func TestPlain(t *testing.T) {}\n"
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
	if !got["TestWithReason"].HasSkip || got["TestWithReason"].SkipReason != "需要真实设备" {
		t.Errorf("带理由的 Skip 解析错：%+v", got["TestWithReason"])
	}
	if !got["TestBareSkip"].HasSkip || got["TestBareSkip"].SkipReason != "" {
		t.Errorf("无理由的 Skip 解析错：%+v", got["TestBareSkip"])
	}
	if got["TestPlain"].HasSkip {
		t.Errorf("普通测试被误判为 Skip：%+v", got["TestPlain"])
	}
	yamlPath := filepath.Join(dir, "test-skips.yaml")
	if err := os.WriteFile(yamlPath, []byte("skips:\n  - test: p/a.TestApproved\n    reason: 只在 CI 跑\n    approved_by: lead\n    at: 2026-11-23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ap, err := testledger.LoadSkipApprovals(yamlPath)
	if err != nil {
		t.Fatalf("LoadSkipApprovals: %v", err)
	}
	if ap["p/a.TestApproved"].ApprovedBy != "lead" {
		t.Errorf("审批清单解析错：%+v", ap)
	}
}
