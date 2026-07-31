package taskcontext

import (
	"os/exec"
	"strings"
	"time"
)

// Context represents the task context detected from the development environment.
//
// Context 表示从开发环境检测到的 task 上下文。
type Context struct {
	Source     string    `json:"source"`   // "explicit", "branch", "unknown"
	TaskRef    string    `json:"task_ref"` // e.g., "PROJ-123", "feature/login", ""
	Branch     string    `json:"branch"`   // current git branch
	Summary    string    `json:"summary"`  // human-readable task description
	DetectedAt time.Time `json:"detected_at"`
}

// IsSet returns true when task context is detected.
//
// IsSet 在检测到 task 上下文时返回 true。
func (c *Context) IsSet() bool {
	return c.Source != "unknown" && c.TaskRef != ""
}

// Detect attempts to infer task context from the environment.
// Currently detected via the git branch name.
//
// Detect 尝试从环境推断 task 上下文。
// 当前通过 git 分支名检测。
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

// ParseBranchName extracts the task reference and summary from a branch name.
//
// Recognized patterns:
//   - feature/login-flow → ref=feature/login-flow, summary=login-flow
//   - fix/PROJ-123-crash → ref=PROJ-123, summary=crash
//   - bugfix/TASK-456    → ref=TASK-456, summary=empty
//   - TASK-789           → ref=TASK-789, summary=empty
//   - PROJ-123-add-auth  → ref=PROJ-123, summary=add-auth
//
// ParseBranchName 从分支名中提取 task reference 与 summary。
//
// 识别的模式：
//   - feature/login-flow → ref=feature/login-flow, summary=login-flow
//   - fix/PROJ-123-crash → ref=PROJ-123, summary=crash
//   - bugfix/TASK-456    → ref=TASK-456, summary=空
//   - TASK-789           → ref=TASK-789, summary=空
//   - PROJ-123-add-auth  → ref=PROJ-123, summary=add-auth
//   - my-feature         → ref=my-feature, summary=my-feature
func ParseBranchName(branch string) (ref, summary string) {
	// Conventional branch prefix (based on Conventional Commits types).
	//
	// 约定式分支前缀（基于 Conventional Commits 类型）。
	for _, prefix := range []string{
		"feat/", "feature/", "fix/", "bugfix/", "hotfix/",
		"refactor/", "test/", "chore/", "docs/", "ci/",
		"perf/", "build/", "style/",
	} {
		if strings.HasPrefix(branch, prefix) {
			rest := strings.TrimPrefix(branch, prefix)
			// If rest contains a ticket ref, extract it.
			//
			// 若 rest 含 ticket ref，则提取出来
			if ticketRef, ticketSummary := extractTicketRef(rest); ticketSummary != rest {
				return ticketRef, ticketSummary
			}
			// Otherwise use prefix+rest as ref and rest as summary.
			//
			// 否则以 prefix+rest 作 ref、rest 作 summary
			return branch, rest
		}
	}

	// Try bare ticket pattern: TASK-123, PROJ-456.
	//
	// 尝试裸 ticket 模式：TASK-123、PROJ-456
	if ticketRef, ticketSummary := extractTicketRef(branch); ticketSummary != branch {
		return ticketRef, ticketSummary
	}

	// Fallback: whole branch name as ref.
	//
	// 兜底：整个分支名作 ref
	return branch, branch
}

// extractTicketRef attempts to find a ticket reference (e.g. PROJ-123) at the start of a string,
// returning that ref and the remaining description.
//
// extractTicketRef 尝试在字符串起首找 ticket reference（如 PROJ-123），
// 返回该 ref 与剩余描述。
func extractTicketRef(s string) (ref, summary string) {
	// Pattern: PROJ-123-description or PROJ-123.
	// Only matches when the prefix itself is already uppercase (does not force uppercase).
	//
	// 模式：PROJ-123-description 或 PROJ-123
	// 仅当前缀本身已全大写时才匹配（不强制定大写）。
	parts := strings.SplitN(s, "-", 3)
	if len(parts) >= 2 {
		// Check whether the prefix is already uppercase (a real project key), AND the second
		// segment starts with a digit — the documented pattern is PROJ-123 (uppercase key +
		// number). Without the digit check, branches like fix/API-crash or hotfix/UI-freeze
		// were misread as ticket refs and their summary was wiped (API-crash → ref with empty
		// summary).
		//
		// 检查前缀是否已是大写（真实 project key），且第二段以数字开头——注释承诺的
		// 模式是 PROJ-123（大写 key + 数字）。没有数字校验时，fix/API-crash、
		// hotfix/UI-freeze 这类分支被误判为 ticket ref，summary 被清空。
		if isProjectKey(parts[0]) && startsWithDigit(parts[1]) {
			ref = parts[0] + "-" + parts[1]
			if len(parts) >= 3 {
				summary = parts[2]
			}
			return ref, summary
		}
	}

	// No ticket ref found — the whole string becomes ref.
	//
	// 未找到 ticket ref——整串作 ref
	return s, s
}

// startsWithDigit reports whether s is non-empty and begins with an ASCII digit.
//
// startsWithDigit 判断 s 非空且以 ASCII 数字开头。
func startsWithDigit(s string) bool {
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
}

// isProjectKey decides whether the string looks like a project key (2-6 uppercase letters).
//
// isProjectKey 判断字符串是否像一个 project key（2-6 个大写字母）。
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

// isMainBranch returns true when the branch represents project-level work (not a personal task branch).
//
// isMainBranch 在分支代表项目级工作（非个人 task 分支）时返回 true。
func isMainBranch(branch string) bool {
	lower := strings.ToLower(branch)
	return lower == "main" || lower == "master" || lower == "develop" || lower == "trunk"
}

// currentBranch returns the current git branch name.
//
// currentBranch 返回当前 git 分支名。
func currentBranch(projectRoot string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SanitizeRef converts a branch name into a safe filename for task state storage.
//
// SanitizeRef 把分支名转换成安全文件名，用于 task state 存储。
func SanitizeRef(ref string) string {
	// Replace characters that are problematic in filenames.
	//
	// 替换文件名中易出问题的字符
	r := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		" ", "-",
	)
	return r.Replace(ref)
}
