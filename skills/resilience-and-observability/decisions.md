# resilience-and-observability — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77221e559aca0-edfd7a78] accept

- **Skill**: resilience-and-observability
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幻觉命令与幽灵指针：forge integration-test 不存在（internal/cli 无此命令），forge skills validate 被误当韧性静态检查，references/ 目录不存在，§2 标题节数写错（5 实为 7）

### Revision

提交前必跑删除 validate 与 integration-test 两行（静态检查改为靠 §4 自查清单人工核对）；删除 references/ 占位句；标题改为 7 路径规范

### Evidence

grep internal/cli 确认 Use 列表无 integration-test；SKILL.md 标题枚举确认 §2.1–2.7 共 7 节
