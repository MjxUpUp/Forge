package checklog

import "time"

// WorkActivity counts distinct tool invocation events for a task since the given time. A single Write/Edit
// produces multiple checklog entries (one per PreToolUse/PostToolUse hook), so we track the last timestamp
// per tool_name and skip entries within <500ms of the previous one to deduplicate.
//
// Returns the deduplicated count of tool invocations.
// Bash is excluded: gate commands also run via Bash.
//
// WorkActivity 统计一个 task 自给定时间起的不同 tool 调用事件。单次 Write/Edit 会
// 产生多条 checklog entry（每个 PreToolUse/PostToolUse hook 各一条），故按 tool_name
// 记录上次时间戳、跳过与上一条间隔 <500ms 的条目来去重。
//
// 返回去重后的 tool 调用数。
// Bash 不计入：gate 命令也走 Bash。
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

	// Dedup: consecutive entries with the same tool_name within <500ms are treated as one invocation. A single Write triggers
	// 4 hooks within ~500ms (task-guard, assertion-check, auto-compile).
	//
	// 去重：相同 tool_name 且间隔 <500ms 的连续条目视为同一次调用。单次 Write 在
	// ~500ms 内触发 4 个 hook（task-guard、assertion-check、auto-compile）。
	lastSeen := map[string]time.Time{}
	count := 0
	for _, e := range entries {
		if !e.RecordedAt.After(since) || !workTools[e.ToolName] {
			continue
		}
		// Skip entries belonging to other tasks.
		// Entries without task_ref (legacy or hooks run outside a task context) are still counted —
		// otherwise projects that haven't re-run `forge init` after the task_ref field was added would get WorkActivity=0.
		//
		// 跳过属于其他 task 的条目。
		// 无 task_ref 的条目（legacy 或在 task 上下文之外运行的 hook）仍然计入——
		// 否则对那些在 task_ref 字段加上后未重新 init forge 的项目，WorkActivity 会返回 0。
		if e.TaskRef != taskRef && e.TaskRef != "" {
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
