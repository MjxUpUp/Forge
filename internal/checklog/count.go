package checklog

import "time"

// WorkActivity counts non-trivial tool usage (Read, Grep, Glob, Agent, Skill)
// for a specific task since a given time. Returns the count of activity entries.
// Bash is excluded because gate commands also use Bash, which doesn't indicate real work.
func WorkActivity(root string, taskRef string, since time.Time) (int, error) {
	entries, err := LoadAll(root)
	if err != nil {
		return 0, err
	}

	workTools := map[string]bool{
		"Read":   true,
		"Grep":   true,
		"Glob":   true,
		"Agent":  true,
		"Skill":  true,
		"Write":  true,
		"Edit":   true,
	}

	count := 0
	for _, e := range entries {
		if e.RecordedAt.After(since) && e.TaskRef == taskRef && workTools[e.ToolName] {
			count++
		}
	}
	return count, nil
}
