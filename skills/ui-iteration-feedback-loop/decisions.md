# ui-iteration-feedback-loop — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18cbf5f4036a4f88-e5948d23] accept

- **Skill**: ui-iteration-feedback-loop
- **DecidedAt**: 2026-08-15T11:08:25Z

### Diagnosis

pi侧35条UI反馈+191条编号反馈+决策码批量回选,用户亲口要求将前端优化过程沉淀为skills

### Revision

新增pipeline交互协议skill:交付带编号决策点(≤5)+双主题变体,反馈逐条映射处置表零静默丢弃

### Evidence

mine-pi跨2项目(DevWorkbench/HarmonyProject);用户元请求:将前端优化重构过程沉淀为skills
