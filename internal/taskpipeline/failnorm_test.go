package taskpipeline

import (
	"fmt"
	"strings"
	"testing"
)

// bulk 产出一段 >500 字节的前置输出，把真正的失败推到 500 字节截断窗之外——
// 指纹若在截断后的 Output 上算，噪音长度一变、切点就挪，必然不稳。
func bulk(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s line %d: ok\n", prefix, i)
	}
	return b.String()
}

// TestFailureFingerprintStableAcrossNoise 钉住三类测试框架的失败输出在耗时/时间戳/
// 地址/goroutine/临时目录噪音变化下指纹不变，且不同失败指纹不同（loop 契约 B1-1）。
func TestFailureFingerprintStableAcrossNoise(t *testing.T) {
	t.Setenv("TMPDIR", "/var/folders/zz/abc123/T")
	cases := []struct {
		name   string
		a, b   string
		differ string // 与 a 实质不同的失败
	}{
		{
			name: "go test",
			a: bulk("=== RUN   TestX", 40) + "--- FAIL: TestFoo (0.12s)\n    foo_test.go:42: want 3 got 2\n" +
				"panic: boom [recovered]\ngoroutine 17 [running]:\nmain.f(0xc000012345)\n2026-10-20T10:11:12.345+08:00 done\nFAIL\tpkg/x\t1.234s\n",
			b: bulk("=== RUN   TestX", 40) + "--- FAIL: TestFoo (1.09s)\n    foo_test.go:42: want 3 got 2\n" +
				"panic: boom [recovered]\ngoroutine 2041 [running]:\nmain.f(0xc0000abcde)\n2026-10-21T03:00:00.001+08:00 done\nFAIL\tpkg/x\t0.071s\n",
			differ: bulk("=== RUN   TestX", 40) + "--- FAIL: TestFoo (0.12s)\n    foo_test.go:42: want 4 got 2\nFAIL\tpkg/x\t1.234s\n",
		},
		{
			name:   "pytest",
			a:      bulk("test_a.py::test_", 30) + "FAILED tests/test_b.py::test_calc - assert 1 == 2\n1 failed, 29 passed in 0.34s\n",
			b:      bulk("test_a.py::test_", 30) + "FAILED tests/test_b.py::test_calc - assert 1 == 2\n1 failed, 29 passed in 12.80s\n",
			differ: bulk("test_a.py::test_", 30) + "FAILED tests/test_b.py::test_other - assert 1 == 3\n1 failed, 29 passed in 0.34s\n",
		},
		{
			name:   "jest",
			a:      bulk("  ✓ renders", 30) + "  ✕ submits form (12 ms)\n    Expected: 200 Received: 500\nTests: 1 failed, 30 passed\nTime: 3.21 s\nRan at /var/folders/zz/abc123/T/jest-9981/run\n",
			b:      bulk("  ✓ renders", 30) + "  ✕ submits form (987 ms)\n    Expected: 200 Received: 500\nTests: 1 failed, 30 passed\nTime: 0.44 s\nRan at /var/folders/zz/abc123/T/jest-1023/run\n",
			differ: bulk("  ✓ renders", 30) + "  ✕ submits form (12 ms)\n    Expected: 200 Received: 404\nTests: 1 failed, 30 passed\nTime: 3.21 s\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.a) <= 500 {
				t.Fatalf("样本须超过 500 字节截断窗，got %d", len(c.a))
			}
			fa, fb, fd := FailureFingerprint(c.a), FailureFingerprint(c.b), FailureFingerprint(c.differ)
			if fa != fb {
				t.Fatalf("同一失败带不同噪音应同指纹:\n%s\n---\n%s", FailureTail(NormalizeFailureOutput(c.a)), FailureTail(NormalizeFailureOutput(c.b)))
			}
			if fa == fd {
				t.Fatal("实质不同的失败不应同指纹")
			}
			// 对照：截断后的 Output 尾部在同样噪音下并不稳定——这正是指纹必须在完整输出上算的原因。
			if truncateAcceptanceOutput(c.a) == truncateAcceptanceOutput(c.b) && c.name == "go test" {
				t.Log("（本样本截断尾部恰好相同，不影响断言）")
			}
		})
	}
}

// TestNormalizeFailureOutputKeepsLines 钉住规范化保留行结构且只替换噪音。
func TestNormalizeFailureOutputKeepsLines(t *testing.T) {
	in := "a 0x1f2e\r\nb 12:34:56.789\r\nc goroutine 9 [running]\r\nd 15ms\n"
	got := NormalizeFailureOutput(in)
	want := "a <ADDR>\nb <TS>\nc goroutine <N> [running]\nd <DUR>\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if n := len(strings.Split(got, "\n")); n != 5 {
		t.Fatalf("行结构应保留（4 行 + 末尾空），got %d", n)
	}
}

// TestFailureTailTakesLastNonEmpty 钉住尾部取最后 20 个非空行、顺序保持。
func TestFailureTailTakesLastNonEmpty(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("l%d", i), "")
	}
	tail := FailureTail(strings.Join(lines, "\n"))
	got := strings.Split(tail, "\n")
	if len(got) != failureTailLines || got[0] != "l10" || got[len(got)-1] != "l29" {
		t.Fatalf("应取最后 %d 个非空行且保持顺序，got %v", failureTailLines, got)
	}
}
