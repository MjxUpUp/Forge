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
// --diff-filter=ACMRT 保留新增/复制/修改/改名/类型变更，删除的文件不进样本
// （文件都不在了，没有可变异的位点）。
func ChangedGoSources(root, since string) ([]string, error) {
	if strings.TrimSpace(since) == "" {
		since = "HEAD"
	}
	cmd := exec.Command("git", "-C", root, "diff", "--name-only", "--diff-filter=ACMRT", since, "--", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff %s: %v\n%s", since, err, strings.TrimSpace(string(out)))
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.TrimSpace(line)
		if f == "" || !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		if strings.HasPrefix(f, "vendor/") || strings.Contains(f, "/vendor/") {
			continue
		}
		files = append(files, filepath.ToSlash(f))
	}
	sort.Strings(files)
	return files, nil
}

// RunOn 对给定文件做变异抽样（sample<=0 取引擎默认 3）。
// 返回的 MutationResult 与任务内抽样同构：killed / survived / invalid / score。
func RunOn(root string, files []string, sample int) (taskpipeline.MutationResult, error) {
	res, _, err := taskpipeline.RunMutationSamplingFiles(root, "", files, sample)
	return res, err
}
