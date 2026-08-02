# design-system-migration — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc89a0f6b218-62501d48] accept

- **Skill**: design-system-migration
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/design-system-migration

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c771b895565118-d161014b] accept

- **Skill**: design-system-migration
- **DecidedAt**: 2026-07-31T18:00:14Z

### Diagnosis

整体审查发现 frontmatter steps: 7 与正文 Phase 0/1/2/3-N/N 结构不符（流程本非固定步数）

### Revision

删除 steps 字段

### Evidence

grep 章节标题确认正文为 Phase 0/1/2/3-N/N 开放结构，无固定步数可填

## [d-18c7729c37576b30-efb4e999] accept

- **Skill**: design-system-migration
- **DecidedAt**: 2026-07-31T18:16:32Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a6831463c0-5837dddb] accept

- **Skill**: design-system-migration
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

单一项目实录下沉 references/case-study.md，主文件 322→122 行只留可迁移方法论

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b17db942c-309a30cd] accept

- **Skill**: design-system-migration
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
