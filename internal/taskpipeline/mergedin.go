package taskpipeline

import (
	"os/exec"
	"strconv"
	"strings"
)

// mergedin.go — 改动集剔除「从另一分支合并带入」的文件。
//
// 任务分支中途合并 main（或任一基线分支）后，`git diff from..HEAD` 会把合并进来的
// 基线改动也算成本任务的改动：scope-drift 误报方案外改动（复发升硬项目里是 HARD
// 阻断）、test-coverage 与 cheat-scan 扫到别人的文件、review 的 test-diff 披露别人
// 的测试改动（2026-10-22 实证：合并 #128 的 52 个文件后报「43 个源文件超出
// PlanScope」）。这里按第一父链区分「本分支自己的提交」与「合并带入的另一侧」。

// ExcludeMergedInFiles drops from committed the files that changed in from..HEAD only through the merged-in side of merge commits; files touched by this branch's own first-parent commits, and conflict-resolution files of merge commits, are kept. With no merge commit in the range, or on any git failure, committed is returned unchanged.
//
// ExcludeMergedInFiles 从 committed 中剔除在 from..HEAD 内仅经合并提交的「另一侧」
// 带入的文件；本分支第一父链上非合并提交触及的文件、以及合并提交里与所有父提交
// 都不同的冲突解决文件保留。区间内没有合并提交、或任何 git 调用失败时原样返回
// （保守：不能证明是外来的，就继续计入）。已知局限：任务尚无提交时快进合并基线，
// 没有合并提交可依据，无法区分，保持原行为。
func ExcludeMergedInFiles(root, from string, committed []string) []string {
	if from == "" || len(committed) == 0 {
		return committed
	}
	n, err := exec.Command("git", "-C", root, "rev-list", "--merges", "--count", from+"..HEAD").Output()
	if err != nil {
		return committed
	}
	if c, err := strconv.Atoi(strings.TrimSpace(string(n))); err != nil || c == 0 {
		return committed
	}
	own := map[string]bool{}
	for _, args := range [][]string{
		{"log", "--first-parent", "--no-merges", "--name-only", "--format=", from + "..HEAD"},
		{"log", "--first-parent", "--merges", "--cc", "--name-only", "--format=", from + "..HEAD"},
	} {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		if err != nil {
			return committed
		}
		for _, line := range strings.Split(string(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				own[line] = true
			}
		}
	}
	kept := make([]string, 0, len(committed))
	for _, f := range committed {
		if own[f] {
			kept = append(kept, f)
		}
	}
	return kept
}
