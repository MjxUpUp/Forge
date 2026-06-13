// Package toolusage records AI agent tool invocations for quality scoring.
package toolusage

import "time"

// ToolCall records a single tool invocation by the AI agent.
// Stored in .forge/toollog.jsonl — one JSON object per line.
type ToolCall struct {
	ToolName  string    `json:"tool_name"`
	ToolInput string    `json:"tool_input,omitempty"`  // truncated to 500 chars
	TaskRef   string    `json:"task_ref,omitempty"`
	SessionID string    `json:"session_id,omitempty"`  // Claude Code session — isolates concurrent sessions
	Timestamp time.Time `json:"timestamp"`
}

// AntiPattern defines a suboptimal tool choice rule.
type AntiPattern struct {
	ID          string // e.g., "bash-cat-vs-read"
	BadTool     string // regex for tool_name, e.g., "^Bash$"
	BadPattern  string // regex for tool_input, e.g., `(?i)\bcat\s+\S`
	PreferTool  string // e.g., "Read"
	Severity    string // "major" or "minor"
	Description string // human-readable explanation
}

// AntiPatternViolation records one detected anti-pattern occurrence.
type AntiPatternViolation struct {
	RuleID    string `json:"rule_id"`
	ToolName  string `json:"tool_name"`
	PreferTool string `json:"prefer_tool"`
	Severity  string `json:"severity"`
	Detail    string `json:"detail"`
}

// SkillHit records a detected skill invocation during task execution.
type SkillHit struct {
	SkillName string    `json:"skill_name"`
	Source    string    `json:"source"` // "skill-tool" or "forge-cli"
	TaskRef   string    `json:"task_ref,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ToolUsageSummary is stored in TaskState alongside the score.
type ToolUsageSummary struct {
	TotalCalls  int                       `json:"total_calls"`
	ToolCounts  map[string]int            `json:"tool_counts"`
	AntiPatterns []AntiPatternViolation   `json:"anti_patterns,omitempty"`
	SkillHits   []SkillHit               `json:"skill_hits,omitempty"`
}

// maxToolInputLen is the truncation limit for tool_input storage.
const maxToolInputLen = 500
