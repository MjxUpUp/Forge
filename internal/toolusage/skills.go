package toolusage

import (
	"encoding/json"
	"regexp"
)

// DetectSkills scans tool calls for skill invocations.
// Skills can be invoked via:
// 1. The Skill tool (tool_name == "Skill") — full credit
// 2. Bash commands running forge CLI commands — partial credit (forge-cli source)
func DetectSkills(calls []ToolCall) []SkillHit {
	var hits []SkillHit

	for _, call := range calls {
		// Direct Skill tool invocation
		if call.ToolName == "Skill" {
			name := parseSkillName(call.ToolInput)
			if name != "" {
				hits = append(hits, SkillHit{
					SkillName: name,
					Source:    "skill-tool",
				})
			}
		}

		// Forge CLI commands via Bash (partial credit for protocol adherence)
		if call.ToolName == "Bash" {
			name := detectForgeCLI(call.ToolInput)
			if name != "" {
				hits = append(hits, SkillHit{
					SkillName: name,
					Source:    "forge-cli",
				})
			}
		}
	}

	return hits
}

// parseSkillName extracts the skill name from a Skill tool's input JSON.
// Input format: {"skill": "forge-pipeline", "args": "..."}
func parseSkillName(input string) string {
	var parsed struct {
		Skill string `json:"skill"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return ""
	}
	return parsed.Skill
}

// forgeCLIPatterns maps bash command patterns to skill names.
var forgeCLIPatterns = []struct {
	Pattern   *regexp.Regexp
	SkillName string
}{
	{regexp.MustCompile(`(?i)\bforge\s+(task|gate|pipeline)\b`), "forge-pipeline"},
	{regexp.MustCompile(`(?i)\bforge\s+(experience|knowledge)\b`), "forge-quality"},
	{regexp.MustCompile(`(?i)\bforge\s+verify\b`), "verify"},
	{regexp.MustCompile(`(?i)\bforge\s+(init|status|validate)\b`), "forge-pipeline"},
}

// detectForgeCLI checks if a Bash command is a forge CLI invocation.
func detectForgeCLI(input string) string {
	for _, p := range forgeCLIPatterns {
		if p.Pattern.MatchString(input) {
			return p.SkillName
		}
	}
	return ""
}

// UniqueSkillNames returns deduplicated skill names from hits.
func UniqueSkillNames(hits []SkillHit) []string {
	seen := make(map[string]bool)
	var names []string
	for _, h := range hits {
		if !seen[h.SkillName] {
			seen[h.SkillName] = true
			names = append(names, h.SkillName)
		}
	}
	return names
}

// SkillHitCount returns the number of unique skills hit (deduped by name).
func SkillHitCount(hits []SkillHit) int {
	return len(UniqueSkillNames(hits))
}
