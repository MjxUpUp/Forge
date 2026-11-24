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

// TestSelectorMultiPackage 钉住"按包判定"：一部分包空跑、另一部分真跑了，不能
// 因为出现 [no tests to run] 就把整条判据判负（假红）；全部包都空跑才算空跑。
func TestSelectorMultiPackage(t *testing.T) {
	cases := []struct {
		name   string
		run    string
		output string
		want   bool
	}{
		{
			name:   "mixed: one package ran",
			run:    "go test -run TestPresent ./...",
			output: "ok  \tfixture/a\t0.01s [no tests to run]\nok  \tfixture/b\t0.02s\n",
			want:   false,
		},
		{
			name:   "all packages empty",
			run:    "go test -run TestGone ./...",
			output: "ok  \tfixture/a\t0.01s [no tests to run]\nok  \tfixture/b\t0.02s [no tests to run]\n",
			want:   true,
		},
		{
			name:   "mix of no-test-files and empty selectors",
			run:    "go test -run TestGone ./...",
			output: "?   \tfixture/a\t[no test files]\nok  \tfixture/b\t0.02s [no tests to run]\n",
			want:   true,
		},
		{
			name:   "test.v=false is not verbose",
			run:    "go test -test.v=false -run TestGone ./internal/z",
			output: "testing: warning: no tests to run\nPASS\nok  \tfixture/z\t0.01s\n",
			want:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SelectorMatchedNothing(c.run, c.output); got != c.want {
				t.Fatalf("SelectorMatchedNothing(%q, %q) = %v, want %v", c.run, c.output, got, c.want)
			}
		})
	}
}
