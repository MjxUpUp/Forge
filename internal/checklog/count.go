package checklog

import "time"

// WorkActivity counts distinct tool usage events for a specific task since a
// given time. A single Write/Edit creates multiple checklog entries (one per
// PreToolUse/PostToolUse hook), so we deduplicate by tracking the last-seen
// timestamp per tool_name and skipping entries within 500ms of the previous one.
//
// Returns the count of deduplicated tool invocations.
// Bash is excluded because gate commands also use Bash.
func WorkActivity(root string, taskRef string, since time.Time) (int, error) {
	entries, err := LoadAll(root)
	if err != nil {
		return 0, err
	}

	workTools := map[string]bool{
		"Read":  true,
		"Grep":  true,
		"Glob":  true,
		"Agent": true,
		"Skill": true,
		"Write": true,
		"Edit":  true,
	}

	// Deduplicate: consecutive entries with same tool_name within 500ms
	// are considered the same invocation. A single Write triggers 4 hooks
	// (task-guard, assertion-check, experience-check, auto-compile) within ~500ms.
	lastSeen := map[string]time.Time{}
	count := 0
	for _, e := range entries {
		if !e.RecordedAt.After(since) || e.TaskRef != taskRef || !workTools[e.ToolName] {
			continue
		}
		if last, ok := lastSeen[e.ToolName]; ok && e.RecordedAt.Sub(last) < 500*time.Millisecond {
			continue
		}
		lastSeen[e.ToolName] = e.RecordedAt
		count++
	}
	return count, nil
}
