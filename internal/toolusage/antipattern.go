package toolusage

import "regexp"

// DefaultAntiPatterns defines known suboptimal tool choice rules.
// Each rule specifies a "bad" tool usage pattern and the preferred alternative.
// Detection is purely string matching — no AI needed.
var DefaultAntiPatterns = []AntiPattern{
	{
		ID: "bash-cat-vs-read", BadTool: "^Bash$", BadPattern: `(?i)\bcat\s+\S`,
		PreferTool: "Read", Severity: "major",
		Description: "Use Read instead of Bash cat",
	},
	{
		ID: "bash-grep-vs-grep", BadTool: "^Bash$", BadPattern: `(?i)\bgrep\s`,
		PreferTool: "Grep", Severity: "major",
		Description: "Use Grep instead of Bash grep",
	},
	{
		ID: "bash-find-vs-glob", BadTool: "^Bash$", BadPattern: `(?i)\bfind\s+`,
		PreferTool: "Glob", Severity: "major",
		Description: "Use Glob instead of Bash find",
	},
	{
		ID: "bash-head-tail-vs-read", BadTool: "^Bash$", BadPattern: `(?i)\b(head|tail)\s+`,
		PreferTool: "Read", Severity: "minor",
		Description: "Use Read with offset/limit instead of Bash head/tail",
	},
	{
		ID: "bash-ls-vs-glob", BadTool: "^Bash$", BadPattern: `(?i)\bls\s+`,
		PreferTool: "Glob", Severity: "minor",
		Description: "Use Glob to list files instead of Bash ls",
	},
	{
		ID: "bash-wc-l", BadTool: "^Bash$", BadPattern: `(?i)\bwc\s+-l\b`,
		PreferTool: "Grep", Severity: "minor",
		Description: "Use Grep with count mode instead of Bash wc -l",
	},
	{
		ID: "bash-sed-vs-edit", BadTool: "^Bash$", BadPattern: `(?i)\bsed\s+-i\b`,
		PreferTool: "Edit", Severity: "major",
		Description: "Use Edit instead of Bash sed for file modifications",
	},
}

// DetectAntiPatterns scans tool calls and returns violations.
func DetectAntiPatterns(calls []ToolCall, patterns []AntiPattern) []AntiPatternViolation {
	var violations []AntiPatternViolation

	for _, call := range calls {
		for _, rule := range patterns {
			toolMatched, err := regexp.MatchString(rule.BadTool, call.ToolName)
			if err != nil || !toolMatched {
				continue
			}
			inputMatched, err := regexp.MatchString(rule.BadPattern, call.ToolInput)
			if err != nil || !inputMatched {
				continue
			}
			violations = append(violations, AntiPatternViolation{
				RuleID:     rule.ID,
				ToolName:   call.ToolName,
				PreferTool: rule.PreferTool,
				Severity:   rule.Severity,
				Detail:     rule.Description,
			})
		}
	}

	return violations
}

// CountBySeverity returns the number of major and minor violations.
func CountBySeverity(violations []AntiPatternViolation) (major, minor int) {
	for _, v := range violations {
		switch v.Severity {
		case "major":
			major++
		case "minor":
			minor++
		}
	}
	return
}

// CountByRule returns a map of rule_id -> occurrence count.
func CountByRule(violations []AntiPatternViolation) map[string]int {
	counts := make(map[string]int)
	for _, v := range violations {
		counts[v.RuleID]++
	}
	return counts
}
