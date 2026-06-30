package scoring

import (
	"os/exec"
	"strings"
)

// CollectGitData gathers the git diff stat used by the scope dimension.
// Returns (diffStat, error); non-fatal if git is unavailable.
//
// Only scope needs git now — the testing dimension reads the test-coverage
// gate verdict from checklog (see scoreTesting), so test-file diff content is
// no longer collected here.
func CollectGitData(root, branch, baseCommit string) (string, error) {
	// Base for diff comparison. Prefer the task's recorded HeadCommit (captured
	// at task start) so each task's scope counts only its own changes — without
	// this, multiple tasks on one feature branch accumulate prior tasks' commits
	// into master..HEAD, inflating the scope dimension. Empty baseCommit falls
	// back for old tasks started before HeadCommit was recorded.
	base := baseCommit
	if base == "" {
		if branch != "" && branch != "main" && branch != "master" {
			// On a feature branch — use resolved base for comparison.
			base = resolveBase(root)
		} else {
			// Already on main/master — compare against HEAD~1.
			base = "HEAD~1"
		}
	}

	diffStat, err := gitDiffStat(root, base)
	if err != nil {
		return "", nil
	}
	return diffStat, nil
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

// gitDiffStat returns the diff --numstat output for changes since base.
// numstat gives true per-file added/deleted counts ("added\tdeleted\tpath"),
// unlike --stat whose second column is the insertions+deletions total and whose
// "+/-" bar is a width-limited visualization. parseDiffStatLines sums the
// per-side counts.
//
// base is the task's HeadCommit (HEAD at task start). Two non-overlapping
// slices: committed changes during the task (base..HEAD) and uncommitted
// working-tree changes (HEAD). This replaces the prior `git diff base` +
// `git diff base..HEAD` pair which double-counted — the single-arg form
// already included the commits that the two-dot form re-added.
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
