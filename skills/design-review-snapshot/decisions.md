# design-review-snapshot — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc899d6841d4-3b7b0b37] accept

- **Skill**: design-review-snapshot
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/design-review-snapshot

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c7729c2f3477a4-5958c2ed] accept

- **Skill**: design-review-snapshot
- **DecidedAt**: 2026-07-31T18:16:32Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a68b9d2824-b0589701] accept

- **Skill**: design-review-snapshot
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

snapshot-script.ts 实为 markdown 改名 .md 并修引用；0★ pixso 仓库依赖加 fallback

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b1f87f56c-db6ba45d] accept

- **Skill**: design-review-snapshot
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
