package bridgeverify

// bridgeverify_test.go — 静态检查器的判级契约。testdata 三个插件包：
// good-plugin（零 error，验收标准 #3 的实跑对象）、broken-plugin（#1884 四
// 模式各踩一半）、no-source（入口缺失）。断言按 Check ID + 关键子串定位，
// 不锁总数（加新检查不破旧测试）。

import (
	"strings"
	"testing"
)

func findings(r *Report, sev Severity, check string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == sev && f.Check == check {
			out = append(out, f)
		}
	}
	return out
}

func hasCheck(r *Report, sev Severity, check, msgSub string) bool {
	for _, f := range findings(r, sev, check) {
		if strings.Contains(f.Message, msgSub) {
			return true
		}
	}
	return false
}

func TestGoodPlugin_NoErrors(t *testing.T) {
	report, err := VerifyPluginDir("testdata/good-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if n := report.Errors(); n != 0 {
		t.Fatalf("good-plugin 应零 error（exit 0 语义），实得 %d: %+v", n, report.Findings)
	}
}

func TestBrokenPlugin_PatternFindings(t *testing.T) {
	report, err := VerifyPluginDir("testdata/broken-plugin")
	if err != nil {
		t.Fatal(err)
	}
	// #1884 模式 1：ctx.sessions 使用未声明 inject → warn（不是 error——静态
	// 分析无法证明该 key 不是动态取得，但必须浮出来）。
	if !hasCheck(report, SevWarn, "ctx-usage", "ctx.sessions") {
		t.Errorf("缺 ctx.sessions 未声明声明的 warn: %+v", report.Findings)
	}
	// 括号取用形态 ctx["network"] 与点取用同判（回检 86 的 P2 修复覆盖面）。
	if !hasCheck(report, SevWarn, "ctx-usage", "ctx.network") {
		t.Errorf("缺 ctx.network 括号取用 warn: %+v", report.Findings)
	}
	// 入口缺 name → error。
	if !hasCheck(report, SevError, "entry", "插件名") {
		t.Errorf("缺 name 应报 error: %+v", report.Findings)
	}
	// #1884 防御实践第一条：@deepseek-ai/* vendored 耦合 → warn。
	if !hasCheck(report, SevWarn, "vendored-import", "@deepseek-ai/cordis") {
		t.Errorf("缺 vendored-import warn: %+v", report.Findings)
	}
	// 事件拼写漂移 → info（静默不触发）。
	if !hasCheck(report, SevInfo, "unknown-event", "tools/prexecute") {
		t.Errorf("缺 unknown-event info: %+v", report.Findings)
	}
	// 外部依赖 → info（pnpm 安装摩擦）。
	if !hasCheck(report, SevInfo, "external-dep", "lodash-es") {
		t.Errorf("缺 external-dep info: %+v", report.Findings)
	}
	if report.Errors() == 0 {
		t.Errorf("broken-plugin 至少 1 个 error（缺 name），实得 0: %+v", report.Findings)
	}
}

func TestNoSourcePlugin_EntryError(t *testing.T) {
	report, err := VerifyPluginDir("testdata/no-source")
	if err != nil {
		t.Fatal(err)
	}
	if !hasCheck(report, SevError, "entry", "没有任何") {
		t.Errorf("缺源文件应报 entry error: %+v", report.Findings)
	}
}

func TestMissingDir_IsToolFault(t *testing.T) {
	if _, err := VerifyPluginDir("testdata/does-not-exist"); err == nil {
		t.Fatal("目录不存在应返回 error（工具故障，区别于检查结论）")
	}
}

func TestDupRegister_WarnAcrossFiles(t *testing.T) {
	report, err := VerifyPluginDir("testdata/dup-register")
	if err != nil {
		t.Fatal(err)
	}
	// #1884 模式 2：同包跨文件同名注册互相覆盖（a.js 与 extra.js 都注册
	// dup-tool）→ warn；index.js 的 sample-dup 不参与（导出名非注册名）。
	if !hasCheck(report, SevWarn, "dup-register", `"dup-tool"`) {
		t.Errorf("缺跨文件同名注册 warn: %+v", report.Findings)
	}
}

func TestReportDeterministic(t *testing.T) {
	a, err := VerifyPluginDir("testdata/broken-plugin")
	if err != nil {
		t.Fatal(err)
	}
	b, err := VerifyPluginDir("testdata/broken-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Findings) != len(b.Findings) {
		t.Fatalf("两次实算 finding 数不一致: %d vs %d", len(a.Findings), len(b.Findings))
	}
	for i := range a.Findings {
		if a.Findings[i] != b.Findings[i] {
			t.Fatalf("第 %d 条 finding 顺序不稳定:\n+%+v\n-%+v", i, a.Findings[i], b.Findings[i])
		}
	}
}
