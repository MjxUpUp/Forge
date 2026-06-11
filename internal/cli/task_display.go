package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Harness/forge/internal/taskpipeline"
	"github.com/Harness/forge/internal/toolusage"
)

// printToolUsageSummary displays tool usage statistics for a scored task.
func printToolUsageSummary(state *taskpipeline.TaskState) {
	if state.ToolUsage == nil || state.ToolUsage.TotalCalls == 0 {
		return
	}

	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("Tool Usage:")

	// Build sorted display from ToolCounts map.
	type kv struct {
		tool  string
		count int
	}
	var entries []kv
	for k, v := range state.ToolUsage.ToolCounts {
		entries = append(entries, kv{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].tool < entries[j].tool
	})

	var parts []string
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf("%s(%d)", e.tool, e.count))
	}
	fmt.Printf("  Total: %d calls — %s\n", state.ToolUsage.TotalCalls, strings.Join(parts, " "))

	if len(state.ToolUsage.AntiPatterns) > 0 {
		major, minor := toolusage.CountBySeverity(state.ToolUsage.AntiPatterns)
		fmt.Printf("  Anti-patterns: %d (%d major, %d minor)\n",
			len(state.ToolUsage.AntiPatterns), major, minor)
		ruleCounts := toolusage.CountByRule(state.ToolUsage.AntiPatterns)
		for rule, cnt := range ruleCounts {
			for _, ap := range state.ToolUsage.AntiPatterns {
				if ap.RuleID == rule {
					fmt.Printf("    ⚠ %s (×%d)\n", ap.Detail, cnt)
					break
				}
			}
		}
	}

	if len(state.ToolUsage.SkillHits) > 0 {
		names := toolusage.UniqueSkillNames(state.ToolUsage.SkillHits)
		fmt.Printf("  Skills used: %s\n", strings.Join(names, ", "))
	}
}
