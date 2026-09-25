# integration-test-architecture — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e5a6aa17b92c-cfbe1d88] accept

- **Skill**: integration-test-architecture
- **DecidedAt**: 2026-08-02T05:24:41Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

如实标注单一项目来源；语言无关规则留正文 Go 专属下沉 references(110→54 行)；RemoteAddr 案例 canonical 全文；pattern 改 reference(配合 taxonomy 扩展)

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e622ad842ab0-35b8ea34] accept

- **Skill**: integration-test-architecture
- **DecidedAt**: 2026-08-02T05:33:33Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad6fba9788-13ec3472] accept

- **Skill**: integration-test-architecture
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口
