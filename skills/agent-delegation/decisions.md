# agent-delegation — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7718fe0b00cb8-1a7e2245] accept

- **Skill**: agent-delegation
- **DecidedAt**: 2026-07-31T17:57:20Z

### Diagnosis

整体审查发现 SKILL.md:106 引用不存在的 verification-before-completion（幻觉 skill 名）

### Revision

改为库内真实存在的 verification-driver

### Evidence

skills/ 目录无 verification-before-completion；verification-driver 存在且语义（声称前必有新鲜证据）吻合
