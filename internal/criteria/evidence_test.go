package criteria_test

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/criteria"
)

// TestSelectorEmptySignals 钉住「-run 选择器空跑成功」的三类信号与两类豁免。
//
// 三类信号：
//   1. go test 明确说 no tests to run（选择器没匹配到测试）；
//   2. go test 明确说 [no test files]（包内根本没有测试文件，退出码仍是 0）；
//   3. -v 下输出里没有任何 === RUN——执行过的测试必然打印该行，没有就是没跑到。
//
// 两类豁免：
//   - (cached)：缓存命中的成功结果本来就没有 RUN 行，不能因此判负；
//   - 不像 go test 的输出（别的工具也有 -run 参数）：不猜，交给退出码。
func TestSelectorEmptySignals(t *testing.T) {
	cases := []struct {
		name string
		run  string
		out  string
		want bool
	}{
		{
			name: "no tests to run",
			run:  "go test -run TestGone ./internal/x -count=1",
			out:  "testing: warning: no tests to run\nPASS\nok  \tgithub.com/x/internal/x\t0.011s [no tests to run]\n",
			want: true,
		},
		{
			name: "no test files",
			run:  "go test -run TestGone ./internal/y -count=1",
			out:  "?   \tgithub.com/x/internal/y\t[no test files]\n",
			want: true,
		},
		{
			name: "verbose without any RUN line",
			run:  "go test -v -run TestGone ./internal/z -count=1",
			out:  "ok  \tgithub.com/x/internal/z\t0.020s\n",
			want: true,
		},
		{
			name: "verbose with RUN line",
			run:  "go test -v -run TestHere ./internal/z -count=1",
			out:  "=== RUN   TestHere\n--- PASS: TestHere (0.00s)\nPASS\nok  \tgithub.com/x/internal/z\t0.020s\n",
			want: false,
		},
		{
			name: "cached has no RUN but is a real pass",
			run:  "go test -v -run TestHere ./internal/z",
			out:  "ok  \tgithub.com/x/internal/z\t(cached)\n",
			want: false,
		},
		{
			name: "no -run selector: not our business",
			run:  "go test ./internal/y",
			out:  "?   \tgithub.com/x/internal/y\t[no test files]\n",
			want: false,
		},
		{
			name: "non go-test tool with -run flag",
			run:  "mytool -run nightly",
			out:  "started\nfinished\n",
			want: false,
		},
		{
			name: "failing test is exit code's business",
			run:  "go test -v -run TestBad ./internal/z",
			out:  "=== RUN   TestBad\n--- FAIL: TestBad (0.00s)\nFAIL\n",
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := criteria.SelectorMatchedNothing(c.run, c.out); got != c.want {
				t.Fatalf("SelectorMatchedNothing(%q, %q) = %v, want %v", c.run, c.out, got, c.want)
			}
		})
	}
}
