// Package mutprobe 是增量变异探针：只对一段 diff 命中的 Go 源文件做变异抽样，
// 复用 taskpipeline 的算子表与执行器。全仓变异是重活（也容易把预算烧完），
// 这里回答的是更窄的问题——"这次改的那几行，测试抓得住吗"。
//
// 增量口径刻意用 git diff：探针不依赖任务状态，因此 change verify、CI、人手工
// 跑都能用同一个入口；"改了什么"由 git 回答，不由 agent 自报。
package mutprobe

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// ChangedGoSources 列出 since..工作区 之间改动的非测试 Go 源文件（repo 相对
// slash 路径，已排序）。since 为空时按 HEAD（= 只看未提交改动）。
//
// 两个容易漏的口径：
//   - git diff 不含未跟踪文件：新建但还没 git add 的源文件必须用
//     ls-files --others 补进来，否则"新写的代码"恰好是探针唯一该看的东西却漏了；
//   - 路径用 -z（NUL 分隔）读：git 默认按 core.quotePath 给非 ASCII 路径加引号转义，
//     按行切会把 "\\344\\270\\255.go" 当路径，后缀过滤后静默丢弃。
//
// --diff-filter=ACMRT 保留新增/复制/修改/改名/类型变更，删除的文件不进样本
// （文件都不在了，没有可变异的位点）。
func ChangedGoSources(root, since string) ([]string, error) {
	if strings.TrimSpace(since) == "" {
		since = "HEAD"
	}
	names, err := gitPaths(root, "diff", "--name-only", "-z", "--diff-filter=ACMRT", since, "--", ".")
	if err != nil {
		return nil, fmt.Errorf("git diff %s: %w", since, err)
	}
	untracked, err := gitPaths(root, "ls-files", "--others", "--exclude-standard", "-z", "--", ".")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others: %w", err)
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range append(names, untracked...) {
		if !isGoSource(f) || seen[f] {
			continue
		}
		seen[f] = true
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// gitPaths 跑一条 git 查询并按 NUL 切分路径；stderr 单独读，避免告警混进路径。
func gitPaths(root string, args ...string) ([]string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%v: %s", err, msg)
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, filepath.ToSlash(p))
		}
	}
	return paths, nil
}

// isGoSource 判断路径是否是值得做变异抽样的非测试 Go 源文件。
func isGoSource(f string) bool {
	f = filepath.ToSlash(f)
	if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
		return false
	}
	if strings.HasPrefix(f, "vendor/") || strings.Contains(f, "/vendor/") {
		return false
	}
	return true
}

// RunOn 对给定文件做变异抽样（sample<=0 取引擎默认 3）。
// 返回的 MutationResult 与任务内抽样同构：killed / survived / invalid / score。
func RunOn(root string, files []string, sample int) (taskpipeline.MutationResult, error) {
	res, _, err := taskpipeline.RunMutationSamplingFiles(root, "", files, sample)
	return res, err
}
