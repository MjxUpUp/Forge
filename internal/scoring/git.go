package scoring

import (
	"os/exec"
	"strings"
)

// CollectGitData collects the git diff stat used by the scope dimension.
// Returns (diffStat, error); git being unavailable is non-fatal.
//
// Only scope needs git today — the testing dimension reads covered/total from the live CheckTestCoverage call
// (see scoreTesting), and assertion density comes from CollectAssertionDensity, hence test-file diff content is not collected here.
//
// CollectGitData 采集 scope 维度使用的 git diff stat。
// 返回 (diffStat, error)；git 不可用时非致命。
//
// 当前只有 scope 需要 git——testing 维度从实时 CheckTestCoverage 调用读 covered/total
// （见 scoreTesting），断言密度来自 CollectAssertionDensity，故此处不采集测试文件 diff 内容。
func CollectGitData(root, branch, baseCommit string) (string, error) {
	base := resolveDiffBase(root, branch, baseCommit)
	diffStat, err := gitDiffStat(root, base)
	if err != nil {
		return "", nil
	}
	return diffStat, nil
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
// gitDiffStat 返回自 base 以来改动的 diff --numstat 输出。
// numstat 给出真实的 per-file added/deleted 计数（"added\tdeleted\tpath"），
// 不像 --stat 其第二列是 insertions+deletions 总和、其"+/-"条是宽度受限的可视化。
// parseDiffStatLines 把 per-side 计数求和。
//
// base 是任务的 HeadCommit（task start 时的 HEAD）。两段不重叠切片：任务期间 committed
// 改动（base..HEAD）与 uncommitted working-tree 改动（HEAD）。
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

	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n"), nil
}
