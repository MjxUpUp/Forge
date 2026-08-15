# review-batch — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771eb6ec32d2c-9abcf750] accept

- **Skill**: review-batch
- **DecidedAt**: 2026-07-31T18:03:53Z

### Diagnosis

整体审查发现 frontmatter composes 用标量 'a, b' 写法，与 CONVENTIONS §4 及库内其他 skill 的 flow list 不一致

### Revision

composes 改 flow list [agent-delegation, code-review-gate]

### Evidence

库内多数 skill 用 [a, b] flow list；CONVENTIONS §4 规定该格式

## [d-18c7e5a65bd21154-059362b7] accept

- **Skill**: review-batch
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

grep 规则细节改指针(D4)，编排层 changed-symbols 注入保留

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620ccfa1884-c1bdf93b] accept

- **Skill**: review-batch
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description SKIP 补 project-acceptance/release-readiness 边界(撞车点);新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad62264284-d01aaf01] accept

- **Skill**: review-batch
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口
