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
// 判定刻意保守：命令里出现 -run（含 -run=）且输出里出现 "no tests to run" 才算。
// 这是启发式——不解析 go 的版本差异，也不假装能覆盖所有"没跑到"的情形（被
// t.Skip 全跳过、包内测试全被 build tag 排除都不在此列）。它只负责拦掉最常见的
// 那种：判据指向一个已经不存在或名字写错的测试。
func SelectorMatchedNothing(run, output string) bool {
	if !strings.Contains(run, "-run") {
		return false
	}
	return strings.Contains(output, "no tests to run")
}
