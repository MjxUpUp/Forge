# frontend-feature-development — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc898e892bc4-939af085] accept

- **Skill**: frontend-feature-development
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/frontend-feature-development

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c7714f1d9de3c0-3a0d7fe7] accept

- **Skill**: frontend-feature-development
- **DecidedAt**: 2026-07-31T17:52:41Z

### Diagnosis

整体审查发现 SKILL.md 两处引用 fullstack-feature，但该 skill 不存在（幽灵引用）

### Revision

description SKIP 段与分工节中 fullstack-feature 指向改为 dev-workflow 编排 + backend-development 的真实组合

### Evidence

skills/ 目录无 fullstack-feature；ls skills/ 确认 backend-development/dev-workflow 存在

## [d-18c7717bca5fd768-e5d64a11] accept

- **Skill**: frontend-feature-development
- **DecidedAt**: 2026-07-31T17:55:53Z

### Diagnosis

整体审查发现 SKILL.md:19 硬编码本机路径 E:\GitHubForkProject\awesome-design-md（分发后必断链）

### Revision

阶段 0 第 2 条改为相对引用 awesome-design-md 仓库 design-md/<slug>/DESIGN.md，发现方式与 fallback 委托 frontend-aesthetics-execution 阶段 1.5

### Evidence

frontend-aesthetics-execution 阶段 1.5 已定义 DESIGN_MD_ROOT/git clone/fallback 的可发现式约定，此处只需指向它避免双写漂移

## [d-18c7729c5f662ad0-36d09564] accept

- **Skill**: frontend-feature-development
- **DecidedAt**: 2026-07-31T18:16:33Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a675b8a768-5bdee667] accept

- **Skill**: frontend-feature-development
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

数据型论据改指针(inline 体积/container query/OKLCH)；token-only/reduced-motion 加单源指针

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b0bbb5574-b76f7e4d] accept

- **Skill**: frontend-feature-development
- **DecidedAt**: 2026-08-02T05:34:09Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
