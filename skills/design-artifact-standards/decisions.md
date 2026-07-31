# design-artifact-standards — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77808f2fa5868-ee7bbb99] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-07-31T19:55:57Z

### Diagnosis

批①将 AllDesignPhases 降私有为 allDesignPhases（零跨包调用），SKILL.md 两处引用未同步

### Revision

SKILL.md :41/:95 引用改为 allDesignPhases

### Evidence

grep 确认符号已改名，编译通过
