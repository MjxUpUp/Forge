// Package criteria 收集"一条判据的退出码 0 到底证明了什么"的共同判定。
//
// change verify（change 侧 acceptance）与 task verify-acceptance（task 侧判据）
// 跑的是同一类命令，也踩过同一个坑：go test 带了 -run 选择器却没匹配到任何测试时
// 退出 0 并打印 "no tests to run"。只看退出码的判据会把"测试被改名/删除/换包"
// 封印成通过。两边必须用同一份判定，否则修了一边另一边继续假绿（B1 只修了 task
// 侧，change 侧漏了整整一批）。
package criteria

import "strings"

// SelectorMatchedNothing 判断一条 go test 判据是否因为 -run 选择器没匹配到任何
// 测试而"空跑成功"。
//
// 三类信号，前两类是 go 的显式输出，第三类是结构证据：
//  1. "no tests to run" —— 选择器在某个包里一个测试都没匹配上；
//  2. "no test files" —— 那个包根本没有测试文件（退出码同样是 0，判据等于没跑）；
//  3. -v 下却没有任何 "=== RUN" —— go test -v 对每个执行过的测试都打印该行，
//     没有就是没跑到（要求输出确实像 go test，避免误伤别的工具的 -run 参数）。
//
// 两类豁免：
//   - "(cached)" —— 缓存命中的成功结果本来就没有 RUN 行，不能因此判负；
//   - 不像 go test 的输出 —— 别的工具也有 -run，退出码 0 就认，不猜。
//
// 这是启发式，不是沙箱：被 build tag 全排除的包、测试内部 t.Skip、以及自造输出
// 的包装脚本仍可能漏判或误判。它只负责把"判据指向不存在/写错的测试"这一类
// 最常见的假绿按住。
func SelectorMatchedNothing(run, output string) bool {
	if !strings.Contains(run, "-run") {
		return false
	}
	if strings.Contains(output, "no tests to run") || strings.Contains(output, "no test files") {
		return true
	}
	if strings.Contains(output, "(cached)") {
		return false
	}
	if !verboseGoTest(run) || !looksLikeGoTestOutput(output) {
		return false
	}
	return !strings.Contains(output, "=== RUN")
}

// verboseGoTest 报告命令是否带 -v（含 -test.v 及其 =true/1 形式）。
func verboseGoTest(run string) bool {
	for _, f := range strings.Fields(run) {
		if f == "-v" || strings.HasPrefix(f, "-test.v") {
			return true
		}
	}
	return false
}

// looksLikeGoTestOutput 用 go test 的输出标记判断"这是不是 go test 在说话"：
// 成功行 "ok  "、无测试文件 "?   "、PASS 行、用例行前缀 "--- "。
func looksLikeGoTestOutput(out string) bool {
	for _, m := range []string{"ok  ", "ok\t", "?   ", "PASS", "--- "} {
		if strings.Contains(out, m) {
			return true
		}
	}
	return false
}
