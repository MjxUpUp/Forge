package checklog

import "time"

// WorkActivity counts distinct tool invocation events for a task since the given time.
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

	// 去重：相同 tool_name 且间隔 <500ms 的连续条目视为同一次调用。单次 Write 在
	// ~500ms 内触发 4 个 hook（task-guard、assertion-check、auto-compile）。
	lastSeen := map[string]time.Time{}
	count := 0
	for _, e := range entries {
		if !e.RecordedAt.After(since) || !workTools[e.ToolName] {
			continue
		}
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
