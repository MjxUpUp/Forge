package scoring

import (
	"fmt"
	"os/exec"
	"strings"
)

// CollectGitData gathers git diff information for scoring.
// Returns (diffTest, diffStat, error). Non-fatal if git is unavailable.
func CollectGitData(root, branch string) (string, string, error) {
	// Use merge-base to compare against the main branch
	base := "main"
	if branch != "" && branch != "main" && branch != "master" {
		base = "main"
	} else {
		// Already on main — compare against HEAD~1
		base = "HEAD~1"
	}

	diffTest, err := gitDiffTestFiles(root, base)
	if err != nil {
		// Non-fatal — return what we have
		return "", "", nil
	}

	diffStat, err := gitDiffStat(root, base)
	if err != nil {
		return diffTest, "", nil
	}

	return diffTest, diffStat, nil
}

// gitDiffTestFiles returns the diff content filtered to test files only.
func gitDiffTestFiles(root, base string) (string, error) {
	// Try merge-base first, fall back to simple diff
	cmd := exec.Command("git", "-C", root, "diff", base, "--", "*_test.*", "*_spec.*", "*.test.*", "*.spec.*", "test/", "tests/")
	out, err := cmd.Output()
	if err != nil {
		// merge-base might fail — try simpler diff
		cmd = exec.Command("git", "-C", root, "diff", "HEAD", "--", "*_test.*", "*_spec.*", "test/", "tests/")
		out, err = cmd.Output()
		if err != nil {
			return "", fmt.Errorf("git diff test files: %w", err)
		}
	}
	return strings.TrimSpace(string(out)), nil
}

// gitDiffStat returns the diff --stat output.
func gitDiffStat(root, base string) (string, error) {
	cmd := exec.Command("git", "-C", root, "diff", "--stat", base)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff stat: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
