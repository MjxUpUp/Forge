# prototype-confirmation — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771eb6a0d97e0-757a4364] accept

- **Skill**: prototype-confirmation
- **DecidedAt**: 2026-07-31T18:03:53Z

### Diagnosis

整体审查发现 frontmatter composes 用标量 'a, b' 写法，与 CONVENTIONS §4 及库内其他 skill 的 flow list 不一致

### Revision

composes 改 flow list [evidence-based-proposal, implementation-discipline]

### Evidence

库内多数 skill 用 [a, b] flow list；CONVENTIONS §4 规定该格式（另一 agent 同步修 CONVENTIONS）

## [d-18c7e620aa5fe038-216e6119] accept

- **Skill**: prototype-confirmation
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md
