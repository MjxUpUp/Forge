package taskpipeline

// DefaultGates returns the 3 standard task-level quality gates (v0.17: reduced from 5).
// These are hardcoded — they apply universally to any task.
// Order matters: gates are executed sequentially.
func DefaultGates() []TaskGate {
	// v0.17: reduced from 5 to 3. task-understand and task-design are now
	// internal workflow steps for the agent, not mandatory gates.
	return []TaskGate{
		{
			ID:          "task-implement",
			Name:        "代码实现",
			Description: "代码已实现（编译/断言改 advisory，由 agent 自检）",
			Auto:        true, // v0.25 advisory: auto-compile.sh + assertion-check.sh 只提醒不阻塞
		},
		{
			ID:          "task-verify",
			Name:        "测试验证",
			Description: "测试伴随变更（advisory 提醒，由 agent 自检）",
			Auto:        false,
		},
		{
			ID:          "task-complete",
			Name:        "完成确认",
			Description: "端到端确认功能可用",
			Auto:        false,
		},
	}
}

// GateByID returns a gate by its ID, or nil if not found.
func GateByID(id string) *TaskGate {
	for _, g := range DefaultGates() {
		if g.ID == id {
			return &g
		}
	}
	return nil
}

// GateIDs returns the ordered list of gate IDs.
func GateIDs() []string {
	gates := DefaultGates()
	ids := make([]string, len(gates))
	for i, g := range gates {
		ids[i] = g.ID
	}
	return ids
}
