# design-audit — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc89997ff094-7483ad74] accept

- **Skill**: design-audit
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/design-audit

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c6bd16c7ea9c1c-487663d0] revise

- **Skill**: design-audit
- **DecidedAt**: 2026-07-29T10:50:07Z
- **By**: claude-code

### Diagnosis

code-reviewer复审发现composes:lark-doc击穿canonical自包含(lark-doc是外部飞书skill强依赖飞书API);守卫C盲区(只扫反引号不扫YAML-composes+加粗/裸文)致原纳入Evidence守卫C验证互引自洽假绿

### Revision

删metadata-composes:lark-doc(去外部强结构依赖);4处lark-doc由加粗或裸文改反引号;skillRefAllowlist加lark-doc标外部lark-skill(同lark-workflow-meeting-summary模式)

### Evidence

守卫C重跑全绿(lark-doc反引号被扫且allowlist放行);forge-skills-validate-R1-R11仍通过

## [d-18c77152d28ded18-676430d6] accept

- **Skill**: design-audit
- **DecidedAt**: 2026-07-31T17:52:57Z

### Diagnosis

整体审查发现 SKILL.md:71 引用不存在的 fullstack-feature（幽灵引用）

### Revision

MISSING 项实现指向中的 fullstack-feature 改为 backend-development

### Evidence

skills/ 目录无 fullstack-feature；backend-development 存在且后端实现属其职责

## [d-18c7e5a69442afd0-57e7a97a] accept

- **Skill**: design-audit
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

description/开篇点破通用价值(三态判决+file:line 证据)，飞书只是输入源之一；决策树压缩

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620c74fa994-5158c3fa] accept

- **Skill**: design-audit
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md
