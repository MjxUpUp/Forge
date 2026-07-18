package taskcontext

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Context represents detected task context from the development environment.
type Context struct {
	Source     string    `json:"source"`   // "explicit", "branch", "unknown"
	TaskRef    string    `json:"task_ref"` // e.g., "PROJ-123", "feature/login", ""
	Branch     string    `json:"branch"`   // current git branch
	Summary    string    `json:"summary"`  // human-readable task description
	DetectedAt time.Time `json:"detected_at"`
}

// IsSet returns true if a task context was detected.
func (c *Context) IsSet() bool {
	return c.Source != "unknown" && c.TaskRef != ""
}

// Detect attempts to infer task context from the environment.
// Currently uses git branch name detection.
func Detect(projectRoot string) *Context {
	now := time.Now()
	branch := currentBranch(projectRoot)

	if branch == "" || isMainBranch(branch) {
		return &Context{
			Source:     "unknown",
			TaskRef:    "",
			Branch:     branch,
			Summary:    "",
			DetectedAt: now,
		}
	}

	ref, summary := ParseBranchName(branch)
	source := "branch"

	return &Context{
		Source:     source,
		TaskRef:    ref,
		Branch:     branch,
		Summary:    summary,
		DetectedAt: now,
	}
}

// ParseBranchName extracts task references and summaries from branch names.
//
// Recognized patterns:
//   - feature/login-flow → ref="feature/login-flow", summary="login-flow"
//   - fix/PROJ-123-crash → ref="PROJ-123", summary="crash"
//   - bugfix/TASK-456    → ref="TASK-456", summary=""
//   - TASK-789           → ref="TASK-789", summary=""
//   - PROJ-123-add-auth  → ref="PROJ-123", summary="add-auth"
//   - my-feature         → ref="my-feature", summary="my-feature"
func ParseBranchName(branch string) (ref, summary string) {
	// Conventional branch prefixes (based on Conventional Commits types).
	for _, prefix := range []string{
		"feat/", "feature/", "fix/", "bugfix/", "hotfix/",
		"refactor/", "test/", "chore/", "docs/", "ci/",
		"perf/", "build/", "style/",
	} {
		if strings.HasPrefix(branch, prefix) {
			rest := strings.TrimPrefix(branch, prefix)
			// If rest contains a ticket ref, extract it
			if ticketRef, ticketSummary := extractTicketRef(rest); ticketSummary != rest {
				return ticketRef, ticketSummary
			}
			// Otherwise, use prefix+rest as ref, rest as summary
			return branch, rest
		}
	}

	// Try bare ticket patterns: TASK-123, PROJ-456
	if ticketRef, ticketSummary := extractTicketRef(branch); ticketSummary != branch {
		return ticketRef, ticketSummary
	}

	// Fallback: use the whole branch name as ref
	return branch, branch
}

// extractTicketRef tries to find a ticket reference (e.g., PROJ-123) at the start
// of the string, returning the ref and the remaining description.
func extractTicketRef(s string) (ref, summary string) {
	// Pattern: PROJ-123-description or PROJ-123
	// Only match if the prefix is ALREADY all-uppercase (not forced).
	parts := strings.SplitN(s, "-", 3)
	if len(parts) >= 2 {
		// Check if the prefix is already uppercase (real project key)
		if isProjectKey(parts[0]) {
			ref = parts[0] + "-" + parts[1]
			if len(parts) >= 3 {
				summary = parts[2]
			}
			return ref, summary
		}
	}

	// No ticket ref found — use full string as ref
	return s, s
}

// isProjectKey checks if a string looks like a project key (2-6 uppercase letters).
func isProjectKey(s string) bool {
	if len(s) < 2 || len(s) > 6 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// isMainBranch returns true for branches that represent project-level work.
func isMainBranch(branch string) bool {
	lower := strings.ToLower(branch)
	return lower == "main" || lower == "master" || lower == "develop" || lower == "trunk"
}

// currentBranch returns the current git branch name.
func currentBranch(projectRoot string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SanitizeRef converts a branch name to a safe filename for task state storage.
func SanitizeRef(ref string) string {
	// Replace chars that are problematic in filenames
	r := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		" ", "-",
	)
	return r.Replace(ref)
}

// FormatContext returns a human-readable summary of the task context.
func FormatContext(ctx *Context) string {
	if !ctx.IsSet() {
		if ctx.Branch != "" {
			return fmt.Sprintf("Branch: %s (no task context detected)", ctx.Branch)
		}
		return "No task context detected"
	}
	return fmt.Sprintf("Task: %s (from %s, branch: %s)", ctx.TaskRef, ctx.Source, ctx.Branch)
}
