# docs-consistency-guard — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e620c158ed70-5f9c97fd] accept

- **Skill**: docs-consistency-guard
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad46a9c38c-f8ec2f87] accept

- **Skill**: docs-consistency-guard
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口
