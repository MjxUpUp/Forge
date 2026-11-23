package taskpipeline

import "testing"

// TestRunSelectorNoMatchJudgedFail 钉住 B1：`go test -run` 一条测试都没匹配上时
// go 仍退出 0 并打印 "[no tests to run]"——判据若只看退出码，测试名拼错、测试被删、
// 包改名都会假绿封印。
func TestRunSelectorNoMatchJudgedFail(t *testing.T) {
	cases := []struct {
		name, run, out string
		want           bool
	}{
		{"-run 无匹配", "go test ./internal/x -run TestFoo", "ok  \tgithub.com/m/x\t0.01s [no tests to run]", true},
		{"-run= 无匹配", "go test -run=TestFoo ./internal/x", "ok  \tgithub.com/m/x\t0.01s [no tests to run]", true},
		{"有测试真跑过", "go test ./internal/x -run TestFoo", "ok  \tgithub.com/m/x\t0.02s", false},
		{"无 -run 选择器", "go test ./internal/x", "ok  \tgithub.com/m/x\t0.01s [no tests to run]", false},
		{"-run 匹配上但失败", "go test ./internal/x -run TestFoo", "--- FAIL: TestFoo (0.00s)", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RunSelectorMatchedNothing(c.run, c.out); got != c.want {
				t.Fatalf("RunSelectorMatchedNothing(%q, %q) = %v, want %v", c.run, c.out, got, c.want)
			}
		})
	}
}
