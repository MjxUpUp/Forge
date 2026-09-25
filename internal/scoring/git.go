package scoring

import (
	"fmt"
	"os/exec"
	"strings"
)

// CollectGitData collects the git diff stat used by the scope dimension.
//
// CollectGitData 采集 scope 维度使用的 git diff stat。
// 返回 (diffStat, error)；error 是真实的（两个 git diff 命令都失败，如非 git 目录或 base
// 不可达），调用方必须显式处理——空 diffStat 走 scope 中性 70 路径，但失败可观测，不再静默吞掉。
//
// 当前只有 scope 需要 git——testing 维度从实时 CheckTestCoverage 调用读 covered/total
// （见 scoreTesting），断言密度来自 CollectAssertionDensity，故此处不采集测试文件 diff 内容。
func CollectGitData(root, branch, baseCommit string) (string, error) {
	base := resolveDiffBase(root, branch, baseCommit)
	return gitDiffStat(root, base)
}

// resolveDiffBase 返回 scope/assertion 采集用的 diff 基线。
// 优先用任务记录的 HeadCommit（task start 时捕获），让每个任务的 scope 只算自己的改动——
// 否则一个 feature 分支上多任务会把先前任务的 commit 累加进 master..HEAD，scope 虚高。
// 空 baseCommit 回落 resolveBase（feature 分支）或 HEAD~1（main）。
func resolveDiffBase(root, branch, baseCommit string) string {
	if baseCommit != "" {
		return baseCommit
	}
	if branch != "" && branch != "main" && branch != "master" {
		return resolveBase(root)
	}
	return "HEAD~1"
}

// resolveBase 尝试常见 base 分支名，返回第一个存在的。
func resolveBase(root string) string {
	for _, name := range []string{"main", "master", "origin/main", "origin/master"} {
		cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", name+"^{commit}")
		if err := cmd.Run(); err == nil {
			return name
		}
	}
	return "HEAD~1"
}

// changedFiles 返回自 base 以来变更的 repo-relative 路径（committed 改动
// base..HEAD 加上相对 HEAD 的 uncommitted working-tree 改动，加上经
// git ls-files --others 纳入的未跟踪文件）。去重，保序。供
// CollectAssertionDensity 使用——只需路径，不要行数。
//
// 未跟踪纳入说明：`git diff` 不接受 --others（exit 129——那是 ls-files 的选项），
// 故未跟踪文件来自单独的 `git ls-files --others --exclude-standard` 探测，而非
// 追加到 diff 命令上。没有它，全新（从未 add）的测试文件对断言密度不可见，
// 假测试信号恰在最常见处失明。
//
// 错误契约（镜像 gitDiffStat，fix/cleanup-batch 2026-08-29）：单个探测失败可容忍
// （其余仍可能有数据）；全部探测失败返回真实 error（非 git 目录、base 不可达、
// git 不存在），让失败对调用方可观测、而非与「无变更」不可区分——
// CollectAssertionDensity 据此跳过假测试惩罚而非惩罚死探测。
func changedFiles(root, base string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)
	add := func(out []byte) {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			files = append(files, line)
		}
	}
	// 1. 任务期间 committed 改动：base..HEAD
	branchCmd := exec.Command("git", "-C", root, "diff", "--name-only", base+"..HEAD")
	branchOut, branchErr := branchCmd.Output()
	if branchErr == nil {
		add(branchOut)
	}
	// 2. 相对 HEAD 的 uncommitted working-tree 改动
	workCmd := exec.Command("git", "-C", root, "diff", "--name-only", "HEAD")
	workOut, workErr := workCmd.Output()
	if workErr == nil {
		add(workOut)
	}
	// 3. 未跟踪文件（经 ls-files 实现 --others --exclude-standard 语义——
	// git diff 不接受 --others，见函数注释）。
	othersCmd := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard")
	othersOut, othersErr := othersCmd.Output()
	if othersErr == nil {
		add(othersOut)
	}
	if branchErr != nil && workErr != nil && othersErr != nil {
		return nil, fmt.Errorf("git diff/ls-files failed (base..HEAD: %v; HEAD: %v; others: %v)", branchErr, workErr, othersErr)
	}
	return files, nil
}

// gitDiffStat 返回自 base 以来改动的 diff --numstat 输出。
// numstat 给出真实的 per-file added/deleted 计数（"added\tdeleted\tpath"），
// 不像 --stat 其第二列是 insertions+deletions 总和、其"+/-"条是宽度受限的可视化。
// parseDiffStatLines 把 per-side 计数求和。
//
// base 是任务的 HeadCommit（task start 时的 HEAD）。两段不重叠切片：任务期间 committed
// 改动（base..HEAD）与 uncommitted working-tree 改动（HEAD）。
//
// 错误契约：单个命令失败可容忍（另一段仍可能有数据）；两个命令都失败返回真实 error
// （非 git 目录、base 不可达、git 不存在），让失败对调用方可观测，而非与"无变更"不可区分。
func gitDiffStat(root, base string) (string, error) {
	var parts []string

	// 1. 任务期间 committed 改动：base..HEAD
	branchCmd := exec.Command("git", "-C", root, "diff", "--numstat", base+"..HEAD")
	branchOut, branchErr := branchCmd.Output()
	if branchErr == nil {
		if s := strings.TrimSpace(string(branchOut)); s != "" {
			parts = append(parts, s)
		}
	}

	// 2. 相对 HEAD 的 uncommitted working-tree 改动
	workCmd := exec.Command("git", "-C", root, "diff", "--numstat", "HEAD")
	workOut, workErr := workCmd.Output()
	if workErr == nil {
		if s := strings.TrimSpace(string(workOut)); s != "" {
			parts = append(parts, s)
		}
	}

	if branchErr != nil && workErr != nil {
		return "", fmt.Errorf("git diff failed (base..HEAD: %v; HEAD: %v)", branchErr, workErr)
	}
	return strings.Join(parts, "\n"), nil
}
