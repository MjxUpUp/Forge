# dev-workflow — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7729c47497394-3fb15ac0] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-07-31T18:16:32Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a652b9e2e0-1c759957] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

forge task 教程下沉 references/forge-integration.md；pattern-guide.md 归位 skill-authoring-standard/references；去「每文件≤10行」机械硬指标

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620a1ab0008-459b07e3] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md
