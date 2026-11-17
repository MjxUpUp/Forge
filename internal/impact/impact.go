// Package impact 从 git 工作区状态推导一次改动的影响面。
//
// 它刻意只做能由代码判定的部分：哪些文件改了、哪些文档要跟着同步、
// 哪些源码同目录没有测试。至于"改动会不会破坏某个调用方"这类需要
// 语义判断的问题，不在这里猜——判断不了就不猜。
package impact

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Report 是一次 diff 的影响面报告：只做能由代码判定的部分，
// 判断不了的不猜。
type Report struct {
	Changed            []string `json:"changed"`              // 相对仓库根的路径，已排序
	Docs               []string `json:"docs"`                 // 命中的文档
	SourcesWithoutTest []string `json:"sources_without_test"` // 改动且同目录没有 _test.go 的 .go 源文件
	SuggestedTests     []string `json:"suggested_tests"`      // 同目录已有 _test.go 的目录（去重、排序）
}

// Collect 读取 git 工作区状态（相对 HEAD 的改动 + 未跟踪文件），返回报告。
// 非 git 仓库返回错误；git 不可用也只返回错误，不 panic。
//
// 空仓库/无改动返回空 Report（不是 error）——"没有影响面"是一种正常结论，
// 不是失败。
func Collect(root string) (*Report, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git 不可用: %w", err)
	}

	// --untracked-files=all：默认模式下完全未跟踪的目录会被折叠成一条
	// "dir/" 记录，那不是文件，逐文件判定（同目录有没有测试）就无从谈起。
	// 展开后每条都是具体文件，下面的"只保留文件"过滤才有意义。
	out, err := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
				return nil, fmt.Errorf("git status 失败: %s", msg)
			}
		}
		return nil, fmt.Errorf("git status 失败: %w", err)
	}

	rep := &Report{
		Changed:            []string{},
		Docs:               []string{},
		SourcesWithoutTest: []string{},
		SuggestedTests:     []string{},
	}

	paths := parsePorcelainZ(string(out))
	sort.Strings(paths)
	suggested := map[string]bool{}
	for _, p := range paths {
		rep.Changed = append(rep.Changed, p)

		if isDoc(p) {
			rep.Docs = append(rep.Docs, p)
			continue
		}
		if !isSource(p) {
			continue
		}
		dir := path.Dir(p)
		if dirHasTest(root, dir) {
			suggested[dir] = true
			continue
		}
		rep.SourcesWithoutTest = append(rep.SourcesWithoutTest, p)
	}

	for dir := range suggested {
		rep.SuggestedTests = append(rep.SuggestedTests, dir)
	}
	sort.Strings(rep.SuggestedTests)
	return rep, nil
}

// parsePorcelainZ 解析 `git status --porcelain=v1 -z` 的输出，返回去重后的
// slash 相对路径（只含文件；删除项与目录项跳过）。
//
// -z 记录用 NUL 分隔，格式是 "XY path"；重命名/复制（R/C）会在记录之后
// 再跟一个 NUL 分隔的旧路径，必须吃掉，否则旧路径会被误当成一条改动。
func parsePorcelainZ(out string) []string {
	fields := strings.Split(out, "\x00")
	seen := map[string]bool{}
	paths := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		xy := f[:2]
		if strings.ContainsAny(xy, "RC") {
			i++ // 下一条是重命名/复制的旧路径
		}
		if strings.ContainsAny(xy, "D") {
			continue // 删除项：不算改动文件
		}
		p := f[3:] // "XY path"：XY 后恰好一个空格
		p = filepath.ToSlash(p)
		if p == "" || strings.HasSuffix(p, "/") {
			continue // 目录项（防御：-uall 下不应出现）
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths
}

// isDoc 判定一个路径是否算"文档"：docs/ 下、README* 一族，或不在
// spec/changes/ 下的 .md。spec/changes/ 是变更记录本身（goal/design 等），
// 出现在 diff 里很正常，不该反向要求"同步文档"。
func isDoc(p string) bool {
	if strings.HasPrefix(p, "spec/changes/") {
		return false
	}
	if strings.HasPrefix(p, "docs/") {
		return true
	}
	if strings.HasPrefix(path.Base(p), "README") {
		return true
	}
	return strings.HasSuffix(p, ".md")
}

// isSource 判定一个路径是否是需要测试的 Go 源文件（测试文件自身不算）。
func isSource(p string) bool {
	return strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go")
}

// dirHasTest 报告仓库内某个目录是否存在任一 *_test.go 文件。
// 目录读不到（已被删/权限问题）按"没有测试"处理：宁可多报一个需要
// 补测试的源码，也不静默漏掉它。
func dirHasTest(root, dir string) bool {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			return true
		}
	}
	return false
}

// SummaryLine 是一行给人看的影响面摘要（complete/next 的披露用）。
func SummaryLine(r *Report) string {
	if r == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("改动 %d 个文件", len(r.Changed))}
	if len(r.Docs) > 0 {
		parts = append(parts, fmt.Sprintf("文档 %d（%s）", len(r.Docs), strings.Join(r.Docs, "、")))
	}
	if len(r.SourcesWithoutTest) > 0 {
		parts = append(parts, fmt.Sprintf("改动源码但同目录无测试 %d", len(r.SourcesWithoutTest)))
	}
	if len(r.SuggestedTests) > 0 {
		parts = append(parts, fmt.Sprintf("可跑测试目录 %d", len(r.SuggestedTests)))
	}
	return strings.Join(parts, "；")
}
