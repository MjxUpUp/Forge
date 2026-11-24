package criteria

import "strings"

// SelectorMatchedNothing 判断一条 go test 判据是否因为 -run 选择器没匹配到任何
// 测试而"空跑成功"。
//
// 判定必须按包读输出：go test ./... -run X 里没匹配到的包会单独打印
// "ok <pkg> [no tests to run]"，而其他包可能真的跑了测试。只看是否出现该子串，
// 会把"一部分包跑了"误判成整体空跑（假红）；只看退出码，又会被"一个包都没匹配"
// 骗过（假绿）。这里的口径是：
//  1. 单包/整体 warning "testing: warning: no tests to run" —— go 明确说选择器
//     一个测试都没匹配上；
//  2. 所有包结果行（ok / FAIL / ?）都带 [no tests to run] 或 [no test files] ——
//     本次运行没有任何一个包真正执行了测试；
//  3. -v 下输出里没有任何 "=== RUN" 或 "--- " 用例行 —— 执行过的测试必然打印
//     这两类行，没有就是没跑到（要求输出确实像 go test，避免误伤别的工具的 -run）。
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
	if strings.Contains(output, "(cached)") {
		return false
	}
	results := packageResultLines(output)
	if !looksLikeGoTestOutput(output, results) {
		return false
	}
	// 1. go 的显式整体告警：选择器一条测试都没匹配上。
	if strings.Contains(output, "testing: warning: no tests to run") {
		return true
	}
	// 2. 逐包结果行：只有"每个包都没跑测试"才算空跑。
	if len(results) > 0 {
		allEmpty := true
		for _, l := range results {
			if !strings.Contains(l, "[no tests to run]") && !strings.Contains(l, "[no test files]") {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			return true
		}
	}
	// 3. -v 但没有任何执行痕迹（"=== RUN" / "--- PASS" / "--- FAIL"）。
	if verboseGoTest(run) && !strings.Contains(output, "=== RUN") && !strings.Contains(output, "--- ") {
		return true
	}
	return false
}

// packageResultLines 取 go test 的包结果行：以 ok / FAIL / ? 开头并跟空白。
func packageResultLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		for _, p := range []string{"ok", "FAIL", "?"} {
			if strings.HasPrefix(t, p) && len(t) > len(p) {
				if c := t[len(p)]; c == ' ' || c == '\t' {
					lines = append(lines, t)
					break
				}
			}
		}
	}
	return lines
}

// verboseGoTest 报告命令是否带 -v（含 -test.v 及其 =true/1 形式；=false/0 不算）。
func verboseGoTest(run string) bool {
	for _, f := range strings.Fields(run) {
		switch {
		case f == "-v":
			return true
		case f == "-test.v":
			return true
		case strings.HasPrefix(f, "-test.v="):
			v := strings.TrimPrefix(f, "-test.v=")
			return v == "true" || v == "1"
		}
	}
	return false
}

// looksLikeGoTestOutput 判断"这是不是 go test 在说话"：有包结果行（ok/FAIL/?），
// 或出现 PASS / 用例行前缀 "--- "。别的工具的 -run 不会产出这些形状。
func looksLikeGoTestOutput(out string, results []string) bool {
	if len(results) > 0 {
		return true
	}
	for _, m := range []string{"PASS", "--- ", "=== RUN", "no tests to run", "[no test files]"} {
		if strings.Contains(out, m) {
			return true
		}
	}
	return false
}
