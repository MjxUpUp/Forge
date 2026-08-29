package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// golden_cheat_test.go —— 检测器黄金用例集（feat/detector-golden-set，2026-08-29 审查轮）。
// 把功能层审查实证过的作弊样本矩阵与测试配对矩阵固化为表驱动回归：直接调用内部
// 规则函数（detect* / isTestFile / hasMatchingTest / ScanUnusedSymbols），逐样本断言
// 命中/不命中——此后任何一轮 review 对同一矩阵的重新采样若发现漂移，这里先红。
//
// golden_cheat_test.go — the detector golden set (feat/detector-golden-set, 2026-08-29
// review round). Freezes the cheat-sample matrix and the test-pairing matrix validated
// by functional review into table-driven regressions: internal rule functions are
// called directly (detect* / isTestFile / hasMatchingTest / ScanUnusedSymbols) with
// per-sample hit/miss assertions — if a future re-sample of the same matrix drifts,
// this file goes red first.

// goldenRunDetectors mirrors the ScanCheatPatterns routing for a single production
// line: type-suppression sees ALL lines (directives live in comments/annotations),
// error-swallow / dead-branch see only non-comment code lines. Returns the hit counts
// per pattern.
//
// goldenRunDetectors 镜像 ScanCheatPatterns 对单条生产行的路由：type-suppression 看
// 全部行（指令活在注释/注解里），error-swallow / dead-branch 只看非注释代码行。
// 返回按模式计的命中数。
func goldenRunDetectors(prod []addedLine) map[CheatPattern]int {
	var code []addedLine
	for _, a := range prod {
		if !isCommentOrBlank(a.text) {
			code = append(code, a)
		}
	}
	m := make(map[CheatPattern]int)
	for _, f := range detectTypeSuppression(prod) {
		m[f.Pattern]++
	}
	for _, f := range detectErrorSwallow(code) {
		m[f.Pattern]++
	}
	for _, f := range detectDeadBranch(code) {
		m[f.Pattern]++
	}
	return m
}

// TestGolden_CheatSampleMatrix is the cheat-sample golden matrix: every sample that
// functional review proved MUST hit (empty catch / comment-body catch / Go error
// discard / nolint / always-false branch / ts-ignore / except:pass / mypy type: ignore)
// and every near-miss that MUST NOT hit (tuple assign / real catch / string-literal
// "fenced" mention).
//
// TestGolden_CheatSampleMatrix 是作弊样本黄金矩阵：功能审查实证【必须命中】的样本
// （空 catch / 注释体 catch / Go 错误丢弃 / nolint / 永假分支 / ts-ignore /
// except:pass / mypy type: ignore）与【不得命中】的近似样本（元组赋值 / 正常 catch /
// 字符串字面量「围栏」内提及）。
func TestGolden_CheatSampleMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		file    string
		line    string
		wantHit bool
		pattern CheatPattern // wantHit=true 时期望的模式
	}{
		// --- 应命中 / must hit ---
		{"empty-catch", "a.ts", "try { run() } catch (e) {}", true, CheatErrorSwallow},
		{"comment-body-catch", "a.ts", "try { run() } catch (e) { /* ignore */ }", true, CheatErrorSwallow},
		{"go-err-discard-var", "a.go", "\tvar _ = err", true, CheatErrorSwallow},
		{"go-err-discard-plain", "a.go", "\t_ = errRetry", true, CheatErrorSwallow},
		{"python-except-pass", "a.py", "\texcept ValueError: pass", true, CheatErrorSwallow},
		{"if-false-paren", "a.ts", "\tif (false) { handleEdge() }", true, CheatDeadBranch},
		{"if-false-go", "a.go", "\tif false {", true, CheatDeadBranch},
		{"while-false", "a.ts", "\twhile (0) { retry() }", true, CheatDeadBranch},
		{"ts-ignore-comment", "a.ts", "\t// @ts-ignore", true, CheatTypeSuppression},
		{"ts-nocheck", "a.ts", "// @ts-nocheck", true, CheatTypeSuppression},
		{"go-nolint", "a.go", "\treturn nil //nolint:nilerr // reason", true, CheatTypeSuppression},
		{"mypy-type-ignore", "a.py", "\tx = calc()  # type: ignore", true, CheatTypeSuppression},
		{"java-suppress-warnings", "A.java", "\t@SuppressWarnings(\"unchecked\")", true, CheatTypeSuppression},
		// --- 不得命中 / must NOT hit ---
		{"go-tuple-assign", "a.go", "\t_, err := f()", false, ""},
		{"real-catch", "a.ts", "try { run() } catch (e) { report(e) }", false, ""},
		{"catch-with-comment-plus-code", "a.ts", "} catch (e) { report(e); /* done */ }", false, ""},
		{"if-real-condition", "a.ts", "\tif (count === 0) { reset() }", false, ""},
		// 围栏内提及：字符串字面量里的指令名是命名/描述（inStringLiteral 排除），非真抑制。
		//
		// Fenced mention: a directive name inside a string literal is naming/description
		// (excluded via inStringLiteral), not a real suppression.
		{"directive-in-string-literal", "a.go", "\tre := regexp.MustCompile(\"@ts-ignore\")", false, ""},
		{"type-ignore-without-hash", "a.py", "\t# the phrase type: ignore in prose is not a directive", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := goldenRunDetectors([]addedLine{{file: tc.file, lineNo: 1, text: tc.line}})
			total := 0
			for _, n := range hits {
				total += n
			}
			if !tc.wantHit {
				if total != 0 {
					t.Errorf("line %q: want NO detector hit, got %+v", tc.line, hits)
				}
				return
			}
			if hits[tc.pattern] == 0 {
				t.Errorf("line %q: want %s hit, got %+v", tc.line, tc.pattern, hits)
			}
		})
	}
}

// TestGolden_PhantomImport pins the phantom-import golden pair: a relative import that
// resolves to a real sibling file passes; one pointing at a non-existent file (the
// hallucination shape) hits with severity=high.
//
// TestGolden_PhantomImport 钉相对路径幻觉 import 黄金对：解析到真实兄弟文件的相对
// import 放行；指向不存在文件（幻觉形态）的命中且 severity=high。
func TestGolden_PhantomImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "util.ts"), []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := detectPhantomImport(dir, []addedLine{
		{file: "src/app.ts", lineNo: 1, text: `import { x } from "./util";`},
		{file: "src/app.ts", lineNo: 2, text: `import { y } from "./hallucinated";`},
	})
	if len(hits) != 1 {
		t.Fatalf("want exactly 1 phantom-import finding (only the hallucinated specifier), got %+v", hits)
	}
	if hits[0].Pattern != CheatPhantomImport || hits[0].Severity != "high" || hits[0].Line != 2 {
		t.Errorf("finding shape mismatch: %+v", hits[0])
	}
}

// TestGolden_UnusedExport pins the unreferenced-export golden pair: a newly added
// exported symbol referenced by another production line stays clean; one referenced
// nowhere in production (doc comment mentions do not count) hits — the "implemented
// but never wired" shape. Needs the git fixture because the deterministic entry point
// (ScanUnusedSymbols) derives its line set from the task diff.
//
// TestGolden_UnusedExport 钉未引用导出黄金对：新增导出符号被另一条生产行引用则干净；
// 生产零引用（doc comment 提及不算）则命中——「实现了但没接线」形态。需要 git fixture：
// 确定性入口（ScanUnusedSymbols）的任务行集来自任务 diff。
func TestGolden_UnusedExport(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"widget.go": "package main\n" +
			"\n" +
			"// Wired is called from main.\n" +
			"func Wired() int { return 1 }\n" +
			"\n" +
			"// Orphan is never called in production.\n" +
			"func Orphan() int { return 2 }\n" +
			"\n" +
			"func main() { _ = Wired() }\n",
	}, "add widget")
	state := newVerifyState(t, dir, "golden-unused")
	findings := ScanUnusedSymbols(dir, state)
	if len(findings) != 1 {
		t.Fatalf("want exactly 1 unused-export finding (Orphan), got %+v", findings)
	}
	if findings[0].Symbol != "Orphan" || findings[0].Pattern != UnusedExport {
		t.Errorf("finding shape mismatch: %+v", findings[0])
	}
}

// TestGolden_IsTestFileMatrix pins the test-file-name golden forms added/fixed in this
// round: the "test_" prefix (root test_root.py acceptance — previously entered the
// missing list), and the Java camelCase Test/Tests/IT suffixes (MainTest.java
// acceptance). Near-misses stay out: "testing.go" (5th char is not '_'), a bare
// "test_.py" (empty stem), "test.py", and the contest/ directory (segment-exact
// matching, no substring exemptions).
//
// TestGolden_IsTestFileMatrix 钉本轮新增/修复的测试文件名黄金形态：「test_」前缀
// （根目录 test_root.py 验收——此前仍进 missing 列表）与 Java 驼峰 Test/Tests/IT 后缀
// （MainTest.java 验收）。近似样本不得命中：testing.go（第 5 字符不是 '_'）、裸
// test_.py（空 stem）、test.py、contest/ 目录（路径段整段匹配，无子串豁免）。
func TestGolden_IsTestFileMatrix(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		// test_ 前缀形态（pytest/minitest）。
		{"test_root.py", true},
		{"pkg/test_helper.py", true},
		{"src/test_poller.rb", true},
		// Java 驼峰形态（JUnit/Maven）。
		{"src/MainTest.java", true},
		{"src/FooTests.java", true},
		{"src/FooIT.java", true},
		// 既有后缀形态不回归。
		{"pkg/foo_test.go", true},
		{"pkg/foo.spec.ts", true},
		{"contest/foo_test.py", true},
		// 近似不命中：前缀无真 stem / 非 test_ 前缀 / 驼峰非 .java。
		{"testing.go", false},
		{"test_.py", false},
		{"test.py", false},
		{"latest/MainTest.ts", false},
		// contest 目录不是测试目录（路径段整段匹配）。
		{"contest/solver.py", false},
		{"contest/Main.java", false},
	} {
		if got := isTestFile(filepath.FromSlash(tc.path)); got != tc.want {
			t.Errorf("isTestFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestGolden_TestPairingMatrix pins the hasMatchingTest golden matrix, including this
// round's rewrite of the java/rb/zig/nim default branch: precise same-directory
// candidates only. The two acceptance/regression anchors: Main.java+MainTest.java MUST
// pair (JUnit camelCase), and poller_daemon_spec.rb MUST NOT pair with poller.rb (the
// old over-broad prefix match did — a different source's spec is not this source's test).
//
// TestGolden_TestPairingMatrix 钉 hasMatchingTest 黄金矩阵，含本轮重写的
// java/rb/zig/nim default 分支：仅同目录精确候选。两个验收/回归锚点：
// Main.java+MainTest.java 必须配对（JUnit 驼峰）；poller_daemon_spec.rb 不得配对
// poller.rb（旧前缀匹配会命中——别的源文件的 spec 不是本源文件的测试）。
func TestGolden_TestPairingMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		changed []string
		want    bool
	}{
		// Go 正常形态（既有行为不回归）。
		{"go-normal", "internal/pkg/foo.go", []string{"internal/pkg/foo.go", "internal/pkg/foo_test.go"}, true},
		{"go-missing", "internal/pkg/foo.go", []string{"internal/pkg/foo.go"}, false},
		// Java：JUnit 驼峰 + 下划线形态（2026-08-29 验收用例）。
		{"java-test-suffix", "src/Main.java", []string{"src/Main.java", "src/MainTest.java"}, true},
		{"java-tests-suffix", "src/Foo.java", []string{"src/Foo.java", "src/FooTests.java"}, true},
		{"java-it-suffix", "src/Foo.java", []string{"src/Foo.java", "src/FooIT.java"}, true},
		{"java-underscore", "src/Foo.java", []string{"src/Foo.java", "src/Foo_test.java"}, true},
		{"java-missing", "src/Main.java", []string{"src/Main.java"}, false},
		// 假配对修复：兄弟源码的 spec 不得配对（旧 HasPrefix(f, "src/poller") 命中）。
		{"rb-daemon-spec-not-pair", "src/poller.rb", []string{"src/poller.rb", "src/poller_daemon_spec.rb"}, false},
		{"rb-spec", "src/poller.rb", []string{"src/poller.rb", "src/poller_spec.rb"}, true},
		{"rb-test-suffix", "src/poller.rb", []string{"src/poller.rb", "src/poller_test.rb"}, true},
		{"rb-minitest-prefix", "src/poller.rb", []string{"src/poller.rb", "src/test_poller.rb"}, true},
		// zig/nim：stem+_test.+ext（任意扩展族）。
		{"zig-test", "src/foo.zig", []string{"src/foo.zig", "src/foo_test.zig"}, true},
		{"nim-test", "src/foo.nim", []string{"src/foo.nim", "src/foo_test.nim"}, true},
		{"nim-missing", "src/foo.nim", []string{"src/foo.nim"}, false},
		// 根目录 python（.py 分支既有形态）。
		{"py-root-prefix", "calc.py", []string{"calc.py", "test_calc.py"}, true},
		{"py-root-suffix", "calc.py", []string{"calc.py", "calc_test.py"}, true},
		{"py-tests-dir", "calc.py", []string{"calc.py", "tests/test_calc.py"}, true},
		{"py-root-missing", "calc.py", []string{"calc.py"}, false},
		// 跨目录不配对：候选必须与源码同目录。
		{"java-cross-dir-not-pair", "src/Main.java", []string{"src/Main.java", "app/MainTest.java"}, false},
		{"zig-cross-dir-not-pair", "src/foo.zig", []string{"src/foo.zig", "lib/foo_test.zig"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := make(map[string]bool, len(tc.changed))
			for _, f := range tc.changed {
				set[f] = true
			}
			if got := hasMatchingTest(tc.src, set); got != tc.want {
				t.Errorf("hasMatchingTest(%q) = %v, want %v (changed=%v)", tc.src, got, tc.want, tc.changed)
			}
		})
	}
}

// TestGolden_CheckTestCoverage_AcceptancePairs drives the full gate (git fixture) over
// the two acceptance pairs of this round: Main.java+MainTest.java commits must yield
// ok=true with EMPTY missing (the paired MainTest.java is itself a test and must not
// re-enter missing), and root root.py+test_root.py likewise. Also pins the
// precomputed-list variant (checkTestCoverageChanged) against the public entry point.
//
// TestGolden_CheckTestCoverage_AcceptancePairs 用完整门禁（git fixture）跑本轮两个
// 验收对：提交 Main.java+MainTest.java 必须得 ok=true 且 missing 为空（配对成功的
// MainTest.java 自身是测试、不得再进 missing）；根目录 root.py+test_root.py 同理。
// 同时钉预计算列表变体（checkTestCoverageChanged）与公开入口的一致性。
func TestGolden_CheckTestCoverage_AcceptancePairs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"java-pair", map[string]string{
			"src/Main.java":     "public class Main { public static void main(String[] a) {} }\n",
			"src/MainTest.java": "import org.junit.Test;\npublic class MainTest { @Test public void parses() {} }\n",
		}},
		{"python-root-pair", map[string]string{
			"root.py":      "def root():\n    return 1\n",
			"test_root.py": "from root import root\n\ndef test_root():\n    assert root() == 1\n",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			initRepoWithMaster(t, dir)
			writeCommitSource(t, dir, tc.files, "golden acceptance")
			state := newVerifyState(t, dir, "golden-"+tc.name)

			ok, missing, total := CheckTestCoverage(dir, state)
			if !ok || len(missing) != 0 {
				t.Fatalf("acceptance pair: want ok=true missing=[], got ok=%v missing=%v", ok, missing)
			}
			if total != 1 {
				t.Errorf("acceptance pair: want total=1 (exactly one non-test source), got %d", total)
			}

			// The precomputed-list variant (now used by both executor gates) must agree
			// with the public entry point on the same changed set.
			//
			// 预计算列表变体（executor 两个 gate 现用）对同一 changed 集必须与公开入口一致。
			ok2, missing2, total2 := checkTestCoverageChanged(dir, state, taskChangedFiles(dir, state))
			if ok2 != ok || len(missing2) != len(missing) || total2 != total {
				t.Errorf("variant disagrees: got (%v,%v,%d), want (%v,%v,%d)", ok2, missing2, total2, ok, missing, total)
			}
		})
	}
}

// TestGolden_CheckTestCoverageChanged_EscapeHatchPreserved pins that the executor's
// new direct call path (checkTestCoverageChanged instead of CheckTestCoverage) keeps
// the escape-hatch semantics: FORGE_TEST_COVERAGE=disable must still return ok=true —
// the audit entry is recorded inside the variant, not only in the public wrapper.
//
// TestGolden_CheckTestCoverageChanged_EscapeHatchPreserved 钉 executor 新直连路径
// （改调 checkTestCoverageChanged 而非 CheckTestCoverage）不丢逃生舱语义：
// FORGE_TEST_COVERAGE=disable 仍须返回 ok=true——审计条目记录在变体内部，
// 不只在公开包装层。
func TestGolden_CheckTestCoverageChanged_EscapeHatchPreserved(t *testing.T) {
	t.Setenv("FORGE_TEST_COVERAGE", "disable")
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "no test here")
	state := newVerifyState(t, dir, "golden-escape")
	ok, missing, _ := checkTestCoverageChanged(dir, state, []string{"foo.go"})
	if !ok || len(missing) != 0 {
		t.Errorf("escape via variant: want ok=true missing=[], got ok=%v missing=%v", ok, missing)
	}
}
