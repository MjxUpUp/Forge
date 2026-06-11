package scoring

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CollectGitData gathers git diff information for scoring.
// Returns (diffTest, diffStat, error). Non-fatal if git is unavailable.
func CollectGitData(root, branch string) (string, string, error) {
	// Determine base branch: try common names, default to HEAD~1.
	base := resolveBase(root)
	if branch != "" && branch != "main" && branch != "master" {
		// On a feature branch — use resolved base for comparison.
	} else {
		// Already on main/master — compare against HEAD~1.
		base = "HEAD~1"
	}

	diffTest, err := gitDiffTestFiles(root, base)
	if err != nil {
		return "", "", nil
	}

	diffStat, err := gitDiffStat(root, base)
	if err != nil {
		return diffTest, "", nil
	}

	return diffTest, diffStat, nil
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

// gitDiffTestFiles returns the diff content filtered to test files only.
// Checks working tree diff, committed branch diff, and untracked files.
func gitDiffTestFiles(root, base string) (string, error) {
	var parts []string

	// 1. Working tree + index vs base (uncommitted + staged changes)
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
	if s := strings.TrimSpace(string(out)); s != "" {
		parts = append(parts, s)
	}

	// 2. Committed changes on branch vs base (catches changes already committed)
	branchCmd := exec.Command("git", "-C", root, "diff", base+"..HEAD", "--", "*_test.*", "*_spec.*", "*.test.*", "*.spec.*", "test/", "tests/")
	branchOut, branchErr := branchCmd.Output()
	if branchErr == nil {
		if s := strings.TrimSpace(string(branchOut)); s != "" {
			parts = append(parts, s)
		}
	}

	// Also detect untracked test files (not in git yet).
	untrackedCmd := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard",
		"--", "*_test.*", "*_spec.*", "*.test.*", "*.spec.*")
	untrackedOut, untrackedErr := untrackedCmd.Output()
	if untrackedErr == nil {
		untracked := strings.TrimSpace(string(untrackedOut))
		if untracked != "" {
			// Read content of untracked test files so scoring sees the lines.
			for _, file := range strings.Split(untracked, "\n") {
				file = strings.TrimSpace(file)
				if file == "" {
					continue
				}
				content, readErr := os.ReadFile(filepath.Join(root, file))
				if readErr != nil {
					continue
				}
				// Synthesize a diff-like header so parseDiffStatLines can count lines.
				lines := strings.Split(string(content), "\n")
				parts = append(parts, fmt.Sprintf("diff --git a/%s b/%s", file, file))
				parts = append(parts, fmt.Sprintf("+++ b/%s", file))
				for _, line := range lines {
					parts = append(parts, "+"+line)
				}
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

// gitDiffStat returns the diff --stat output for both working tree and branch changes.
func gitDiffStat(root, base string) (string, error) {
	var parts []string

	// Working tree + index diff
	cmd := exec.Command("git", "-C", root, "diff", "--stat", base)
	out, err := cmd.Output()
	if err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			parts = append(parts, s)
		}
	}

	// Committed branch diff (catches changes already on the feature branch)
	branchCmd := exec.Command("git", "-C", root, "diff", "--stat", base+"..HEAD")
	branchOut, branchErr := branchCmd.Output()
	if branchErr == nil {
		if s := strings.TrimSpace(string(branchOut)); s != "" {
			parts = append(parts, s)
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n"), nil
}
