package criteria

import "testing"

// TestSelectorMatchedNothing 钉死「-run 命中不到任何测试」的识别口径：
// go test 退出 0 但只打印 [no tests to run] —— 判据指向不存在/写错的测试时
// 这不是通过，是没验。
func TestSelectorMatchedNothing(t *testing.T) {
	cases := []struct {
		name   string
		run    string
		output string
		want   bool
	}{
		{"-run 空匹配", "go test -run TestNoSuch ./...", "ok fixture 0.01s [no tests to run]", true},
		{"-run= 空匹配", "go test -run=TestNoSuch ./...", "testing: warning: no tests to run", true},
		{"真跑了测试", "go test -run TestPresent ./...", "--- PASS: TestPresent (0.00s)\nok fixture 0.02s", false},
		{"没有 -run", "go test ./...", "ok fixture 0.02s", false},
		{"-run 命中但失败", "go test -run TestBroken ./...", "--- FAIL: TestBroken (0.00s)", false},
	}
	for _, c := range cases {
		if got := SelectorMatchedNothing(c.run, c.output); got != c.want {
			t.Errorf("%s: SelectorMatchedNothing(%q) = %v, want %v", c.name, c.run, got, c.want)
		}
	}
}
