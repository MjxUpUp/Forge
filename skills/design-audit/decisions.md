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
