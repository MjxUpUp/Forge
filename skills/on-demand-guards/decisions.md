# on-demand-guards — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7718fe9b06088-ed1e2873] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-07-31T17:57:20Z

### Diagnosis

整体审查发现分工表引用 delivery-gate，非本仓库 canonical skill，未显式标注来源

### Revision

显式标注'非本仓库 canonical skill，仅部分 agent 以扩展形式提供'

### Evidence

ls skills/ 无 delivery-gate；原表述'部分 agent 以扩展形式提供'含糊，易误当本库 skill
