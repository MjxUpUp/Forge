package taskpipeline

import (
	"path/filepath"
	"regexp"
	"testing"
)

// 2026-08-29 审查轮回归钉：isTestFile 从子串匹配改为路径段整段匹配后，
// contest//latest//attest/ 等生产目录不得再被误判为测试目录（原形态构成
// "豁免测试义务 + 逃出 cheat/unused 扫描 + scope 评分不计改动量"的逃逸区）。
func TestIsTestFile_SegmentMatching(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"pkg/foo_test.go", true},
		{"pkg/foo.spec.ts", true},
		{"src/app.test.js", true},
		{"tests/helper.py", true},
		{"src/__tests__/a.ts", true},
		{"test/main.py", true},
		// 子串陷阱：目录名只是【包含】test/ 子串，整段不等于 test/tests。
		{"contest/foo.py", false},
		{"latest/config.go", false},
		{"attest/witness.rs", false},
		{"greatest/x.rb", false},
		{"unittest/main.go", false},
		// 文件名同理只看 base 的 _test./_spec./.test./.spec. 形态。
		{"pkg/contest.go", false},
		{"pkg/testify_mock.go", false},
	}
	for _, c := range cases {
		if got := isTestFile(filepath.FromSlash(c.path)); got != c.want {
			t.Errorf("isTestFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// 2026-08-29 审查轮回归钉：cheatscan 对注释体 catch / Go 错误丢弃 / Go nolint
// 的检测（原为功能探针实证的零成本绕过面）。
func TestCheatScan_ReviewRoundRegexes(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  []*regexp.Regexp
		line string
		want bool
	}{
		{"comment-catch-block", errorSwallowRe, "try { x() } catch (e) { /* ignore */ }", true},
		{"comment-catch-line", errorSwallowRe, "} catch (e) { // 忽略 }", true},
		{"empty-catch", errorSwallowRe, "try { x() } catch (e) {}", true},
		{"real-catch-not-flagged", errorSwallowRe, "try { x() } catch (e) { log(e) }", false},
		{"comment-catch-with-code-not-flagged", errorSwallowRe, "try { x() } catch (e) { log(e); /* done */ }", false},
		{"go-err-discard", errorSwallowRe, "\tvar _ = err", true},
		{"go-err-discard-plain", errorSwallowRe, "_ = err2", true},
		{"go-tuple-assign-not-flagged", errorSwallowRe, "_, err := f()", false},
		{"go-blank-import-not-flagged", errorSwallowRe, "\t_ \"embed\"", false},
		{"go-nolint", typeSuppressionRe, "\treturn nil //nolint:nilerr // reason", true},
		{"go-nolint-plain", typeSuppressionRe, "//nolint:gocritic", true},
		{"nolint-in-prose-not-flagged", typeSuppressionRe, "// we decided not to nolint here", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			matched := false
			for _, re := range tc.res {
				if re.MatchString(tc.line) {
					matched = true
					break
				}
			}
			if matched != tc.want {
				t.Errorf("line %q matched=%v, want %v", tc.line, matched, tc.want)
			}
		})
	}
}
