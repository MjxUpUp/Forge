package scoring

import (
	"os/exec"
	"strings"
)

// CollectGitData gathers the git diff stat used by the scope dimension.
// Returns (diffStat, error); non-fatal if git is unavailable.
//
// Only scope needs git now — the testing dimension reads covered/total from a
// live CheckTestCoverage call (see scoreTesting) and assertion density from
// CollectAssertionDensity, so test-file diff content is not collected here.
func CollectGitData(root, branch, baseCommit string) (string, error) {
	base := resolveDiffBase(root, branch, baseCommit)
	diffStat, err := gitDiffStat(root, base)
	if err != nil {
		return "", nil
	}
	return diffStat, nil
}

// resolveDiffBase returns the diff base for scope/assertion collection.
// Prefers the task's recorded HeadCommit (captured at task start) so each task's
// scope counts only its own changes — without this, multiple tasks on one feature
// branch accumulate prior tasks' commits into master..HEAD, inflating scope.
// Empty baseCommit falls back to resolveBase (feature branch) or HEAD~1 (main).
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
func resolveBase(root string) string {
	for _, name := range []string{"main", "master", "origin/main", "origin/master"} {
		cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", name+"^{commit}")
		if err := cmd.Run(); err == nil {
			return name
		}
	}
	return "HEAD~1"
}

// changedFiles returns repo-relative paths changed since base (committed changes
// base..HEAD plus uncommitted working-tree changes vs HEAD). De-duplicated, order
// preserved. Used by CollectAssertionDensity — it needs only paths, not line counts.
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
	branchCmd := exec.Command("git", "-C", root, "diff", "--name-only", base+"..HEAD")
	if out, err := branchCmd.Output(); err == nil {
		add(out)
	}
	// 2. Uncommitted working-tree changes relative to HEAD
	workCmd := exec.Command("git", "-C", root, "diff", "--name-only", "HEAD")
	if out, err := workCmd.Output(); err == nil {
		add(out)
	}
	return files
}

// gitDiffStat returns the diff --numstat output for changes since base.
// numstat gives true per-file added/deleted counts ("added\tdeleted\tpath"),
// unlike --stat whose second column is the insertions+deletions total and whose
// "+/-" bar is a width-limited visualization. parseDiffStatLines sums the
// per-side counts.
//
// base is the task's HeadCommit (HEAD at task start). Two non-overlapping
// slices: committed changes during the task (base..HEAD) and uncommitted
// working-tree changes (HEAD).
func gitDiffStat(root, base string) (string, error) {
	var parts []string

	// 1. Committed changes during the task: base..HEAD
	branchCmd := exec.Command("git", "-C", root, "diff", "--numstat", base+"..HEAD")
	branchOut, branchErr := branchCmd.Output()
	if branchErr == nil {
		if s := strings.TrimSpace(string(branchOut)); s != "" {
			parts = append(parts, s)
		}
	}

	// 2. Uncommitted working-tree changes relative to HEAD
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
