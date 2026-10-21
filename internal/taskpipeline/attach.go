package taskpipeline

// AttachSession anchors session sid (running under tool) to task ref: SessionLinks gains the session and the node lease moves to this machine. Returns attached=true only when a link was actually added; an empty sid is a no-op.
//
// AttachSession 把 session sid（运行于 tool）锚定到任务 ref：SessionLinks 增加该 session，
// 节点租约认领到本机。仅当确实新增了链接时 attached=true；sid 为空 no-op。tool 为空时回落
// 项目标记弱信号（detectAgentType，同 EnsureHookSession）。在 per-task 锁内重载再改——调用方
// 手上的 state 可能已过期（其他 worktree 的并发 attach/decide）。
//
// 只写 SessionLinks 与租约：不改 CurrentGate、不碰产物链、不输出任何东西（按需拉取方案
// INV-4）。`forge task resume --ref` 的显式锚定与 task-guard 的首次写入懒挂接共用本函数。
func AttachSession(root, ref, sid, tool string) (bool, error) {
	if sid == "" {
		return false, nil
	}
	if tool == "" {
		tool = detectAgentType(root)
	}
	attached := false
	err := MutateTaskState(root, ref, func(s *TaskState) error {
		if s.HasSession(sid) {
			return nil
		}
		s.AddSession(sid, tool)
		// 新锚定 = 本机接手工作——认领节点租约，让 advisory 追踪真正在干活的机器
		// 而非任务创建者（fail-open、仅 advisory；sync-convergence.md §4）。
		ClaimLeaseForCurrentNode(s)
		attached = true
		return nil
	})
	return attached, err
}
