package taskpipeline

// DefaultGates returns the 5 standard task-level quality gates.
// These are hardcoded — they apply universally to any task.
// Order matters: gates are executed sequentially.
func DefaultGates() []TaskGate {
	return []TaskGate{
		{
			ID:          "task-understand",
			Name:        "理解任务",
			Description: "AI agent 已描述任务意图和范围",
			Auto:        false,
		},
		{
			ID:          "task-design",
			Name:        "方案设计",
			Description: "非简单改动有方案描述",
			Auto:        false,
		},
		{
			ID:          "task-implement",
			Name:        "代码实现",
			Description: "代码编译通过 + hooks 通过",
			Auto:        true, // checked by auto-compile.sh + assertion-check.sh
		},
		{
			ID:          "task-verify",
			Name:        "测试验证",
			Description: "变更有对应测试 + 测试通过",
			Auto:        false, // partially: tests need to pass, but AI ensures coverage
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
