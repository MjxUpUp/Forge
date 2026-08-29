package taskpipeline

import (
	"path/filepath"
	"testing"
)

// golden_cheat_test.go —— 检测器黄金用例集（feat/detector-golden-set，2026-08-29 审查轮）。
// 把功能层审查实证过的作弊样本矩阵与测试配对矩阵固化为表驱动回归：直接调用内部
// 规则函数（detect* / isTestFile / hasMatchingTest），逐样本断言命中/不命中——此后任何
// 一轮 review 对同一矩阵的重新采样若发现漂移，这里先红。2026-08-30 测试瘦身：原
// detectors_review_test.go 的审查轮反例已并入两张矩阵（phantom-import 的黄金对见
// cheatscan_test.go TestDetectPhantomImport，unused-export 的 Orphan/Wired 见
// unusedscan_test.go TestScanUnusedSymbols_RealGitDiff——样本更全，此处不重复）。

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

// TestGolden_CheatSampleMatrix is the cheat-sample golden matrix: every sample that functional review proved MUST hit (empty catch / comment-body catch / Go error discard / nolint / always-false branch / ts-ignore / except:pass / mypy type: ignore) and every near-miss that MUST NOT hit (tuple assign / real catch / string-literal "fenced" mention).
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
		// 2026-08-29 审查轮正则钉（原 detectors_review_test.go 并入）。
		{"comment-catch-line", "a.ts", "} catch (e) { // 忽略 }", true, CheatErrorSwallow},
		{"go-nolint-plain", "a.go", "//nolint:gocritic", true, CheatTypeSuppression},
		{"nolint-in-prose-not-flagged", "a.go", "// we decided not to nolint here", false, ""},
		{"go-blank-import-not-flagged", "a.go", "\t_ \"embed\"", false, ""},
		// --- 不得命中 / must NOT hit ---
		{"go-tuple-assign", "a.go", "\t_, err := f()", false, ""},
		{"real-catch", "a.ts", "try { run() } catch (e) { report(e) }", false, ""},
		{"catch-with-comment-plus-code", "a.ts", "} catch (e) { report(e); /* done */ }", false, ""},
		{"if-real-condition", "a.ts", "\tif (count === 0) { reset() }", false, ""},
		// 围栏内提及：字符串字面量里的指令名是命名/描述（inStringLiteral 排除），非真抑制。
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

// TestGolden_IsTestFileMatrix pins the test-file-name golden forms added/fixed in this round: the "test_" prefix (root test_root.py acceptance — previously entered the missing list), and the Java camelCase Test/Tests/IT suffixes (MainTest.java acceptance).
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
		// 2026-08-29 审查轮回归钉（原 detectors_review_test.go 并入）：isTestFile 从
		// 子串匹配改为路径段整段匹配后，contest/latest/attest 等生产目录与
		// test 子串文件名不得再被误判为测试目录（原形态构成「豁免测试义务 +
		// 逃出 cheat/unused 扫描 + scope 评分不计改动量」的逃逸区）。
		{"src/app.test.js", true},
		{"tests/helper.py", true},
		{"src/__tests__/a.ts", true},
		{"test/main.py", true},
		{"contest/foo.py", false},
		{"latest/config.go", false},
		{"attest/witness.rs", false},
		{"greatest/x.rb", false},
		{"unittest/main.go", false},
		{"pkg/contest.go", false},
		{"pkg/testify_mock.go", false},
	} {
		if got := isTestFile(filepath.FromSlash(tc.path)); got != tc.want {
			t.Errorf("isTestFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestGolden_TestPairingMatrix pins the hasMatchingTest golden matrix, including this round's rewrite of the java/rb/zig/nim default branch: precise same-directory candidates only.
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

// TestGolden_CheckTestCoverage_AcceptancePairs drives the full gate (git fixture) over the two acceptance pairs of this round: Main.java+MainTest.java commits must yield ok=true with EMPTY missing (the paired MainTest.java is itself a test and must not re-enter missing), and root root.py+test_root.py likewise.
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

			// 预计算列表变体（executor 两个 gate 现用）对同一 changed 集必须与公开入口一致。
			ok2, missing2, total2 := checkTestCoverageChanged(dir, state, taskChangedFiles(dir, state))
			if ok2 != ok || len(missing2) != len(missing) || total2 != total {
				t.Errorf("variant disagrees: got (%v,%v,%d), want (%v,%v,%d)", ok2, missing2, total2, ok, missing, total)
			}
		})
	}
}

// TestGolden_CheckTestCoverageChanged_EscapeHatchPreserved pins that the executor's new direct call path (checkTestCoverageChanged instead of CheckTestCoverage) keeps the escape-hatch semantics: FORGE_TEST_COVERAGE=disable must still return ok=true — the audit entry is recorded inside the variant, not only in the public wrapper.
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
