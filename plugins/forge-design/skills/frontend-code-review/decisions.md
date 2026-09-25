# frontend-code-review — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18d2537c3e45c4d4-a9f3a8cb] accept

- **Skill**: frontend-code-review
- **DecidedAt**: 2026-09-05T04:49:55Z

### Diagnosis

功能聚焦批次一线2：按 skills 价值审计（docs/skills-value-audit-2026-08-02.md）与聚焦决策（docs/plans/feature-focus-2026-09.md）执行拆包/瘦身/引用清理

### Revision

拆包至 plugins/forge-design（设计族 12 个）或教科书瘦身/死机制清理（详见 96e0182 提交）

### Evidence

docs/plans/feature-focus-2026-09.md 决策表 + 审计逐项建议 + 96e0182/b967906 提交

## [d-18d25390ee33d480-869ff695] accept

- **Skill**: frontend-code-review
- **DecidedAt**: 2026-09-05T04:51:23Z

### Diagnosis

功能聚焦批次一线2：设计族拆包至 plugins/forge-design（git mv 零内容变更）

### Revision

迁移非改写；引用清理见 96e0182

### Evidence

docs/plans/feature-focus-2026-09.md §2.1
