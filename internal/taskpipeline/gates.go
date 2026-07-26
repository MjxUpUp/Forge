package taskpipeline

// DefaultGates returns the 3 standard task-level quality gates (v0.17: trimmed from 5). These gates
// are hard-coded — universal for any task. Order-sensitive: gates run in sequence.
//
// DefaultGates 返回 3 个标准 task 级质量 gate（v0.17：从 5 个精简而来）。这些 gate
// 是硬编码的——对任何 task 通用。顺序敏感：gate 按序执行。
func DefaultGates() []TaskGate {
	// v0.17: trimmed from 5 to 3. task-understand and task-design are now agent internal
	// workflow steps, no longer mandatory gates.
	//
	// v0.17：从 5 个精简到 3 个。task-understand 和 task-design 现在是 agent 的
	// 内部工作流步骤，不再是强制 gate。
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

// GateByID returns the gate by ID, or nil if not found.
//
// GateByID 按 ID 返回 gate，未找到返回 nil。
func GateByID(id string) *TaskGate {
	for _, g := range DefaultGates() {
		if g.ID == id {
			return &g
		}
	}
	return nil
}

// GateIDs returns the ordered list of gate IDs.
//
// GateIDs 返回有序的 gate ID 列表。
func GateIDs() []string {
	gates := DefaultGates()
	ids := make([]string, len(gates))
	for i, g := range gates {
		ids[i] = g.ID
	}
	return ids
}
