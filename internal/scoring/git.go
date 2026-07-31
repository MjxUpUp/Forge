package scoring

import (
	"fmt"
	"os/exec"
	"strings"
)

// CollectGitData collects the git diff stat used by the scope dimension.
// Returns (diffStat, error); the error is real (both git diff commands failed, e.g. non-git
// dir or unreachable base) and must be surfaced by the caller — scope degrades to the neutral
// 70 path on empty diffStat, but the failure stays observable instead of silently swallowed.
//
// Only scope needs git today — the testing dimension reads covered/total from the live CheckTestCoverage call
// (see scoreTesting), and assertion density comes from CollectAssertionDensity, hence test-file diff content is not collected here.
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

// resolveDiffBase returns the diff base used for scope/assertion collection.
// It prefers the task-recorded HeadCommit (captured at task start) so each task's scope counts only its own changes —
// otherwise multiple tasks on one feature branch accumulate prior tasks' commits into master..HEAD, inflating scope.
// An empty baseCommit falls back to resolveBase (feature branches) or HEAD~1 (main).
//
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

// resolveBase tries common base branch names and returns the first that exists.
//
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

// changedFiles returns repo-relative paths changed since base (committed changes base..HEAD plus
// uncommitted working-tree changes relative to HEAD). Deduplicated, order-preserving. Used by
// CollectAssertionDensity — needs only paths, not line counts.
//
// changedFiles 返回自 base 以来变更的 repo-relative 路径（committed 改动 base..HEAD 加上
// 相对 HEAD 的 uncommitted working-tree 改动）。去重，保序。供 CollectAssertionDensity
// 使用——只需路径，不要行数。
func changedFiles(root, base string) []string {
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
	// 1. Committed changes during the task: base..HEAD
	//
	// 1. 任务期间 committed 改动：base..HEAD
	branchCmd := exec.Command("git", "-C", root, "diff", "--name-only", base+"..HEAD")
	if out, err := branchCmd.Output(); err == nil {
		add(out)
	}
	// 2. Uncommitted working-tree changes relative to HEAD
	//
	// 2. 相对 HEAD 的 uncommitted working-tree 改动
	workCmd := exec.Command("git", "-C", root, "diff", "--name-only", "HEAD")
	if out, err := workCmd.Output(); err == nil {
		add(out)
	}
	return files
}

// gitDiffStat returns the diff --numstat output for changes since base.
// numstat gives true per-file added/deleted counts (added\tdeleted\tpath),
// unlike --stat whose second column is the insertions+deletions total and whose +/- bar is a width-limited visualization.
// parseDiffStatLines sums the per-side counts.
//
// base is the task's HeadCommit (HEAD at task start). Two non-overlapping slices: committed changes
// during the task (base..HEAD) and uncommitted working-tree changes (HEAD).
//
// Error contract: a single command failing is tolerated (the other slice may still yield data);
// BOTH commands failing returns the real error (non-git dir, unreachable base, git missing) so the
// failure is observable to the caller instead of being indistinguishable from "no changes".
//
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

	// 1. Committed changes during the task: base..HEAD
	//
	// 1. 任务期间 committed 改动：base..HEAD
	branchCmd := exec.Command("git", "-C", root, "diff", "--numstat", base+"..HEAD")
	branchOut, branchErr := branchCmd.Output()
	if branchErr == nil {
		if s := strings.TrimSpace(string(branchOut)); s != "" {
			parts = append(parts, s)
		}
	}

	// 2. Uncommitted working-tree changes relative to HEAD
	//
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
