# design-system-workflow — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc89a4b9453c-4b3274ce] accept

- **Skill**: design-system-workflow
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/design-system-workflow

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c6be45b0ac15c8-97fd53c4] revise

- **Skill**: design-system-workflow
- **DecidedAt**: 2026-07-29T11:11:48Z
- **By**: claude-code

### Diagnosis

description 513 rune 触发 R4 advisory 偏长(>500)；R4 用 utf8.RuneCountInString 计 rune 非字节，中文 1 字=1 rune

### Revision

精简到 446 rune：合并 what 段与 Use when 段对 Figma/Pixso/PenPot 的重复列举、压缩 Use when/SKIP 表述，保留 OKLCH/shadcn registry/composite token/DTCG/Style Dictionary 等关键 what 语义；另将 SKIP「纯视觉审美执行」从无路由（原括号仅说明文字，非 skill 引用）升级为显式指向 frontend-aesthetics-execution（路由表 4→5 条，改进路由完整性）

### Evidence

forge skills validate 原 design-system-workflow description 偏长(513>500) advisory 消失，现 446 rune ≤500

## [d-18c7729c3f7a5df4-15365136] accept

- **Skill**: design-system-workflow
- **DecidedAt**: 2026-07-31T18:16:32Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a687593898-650e43d1] accept

- **Skill**: design-system-workflow
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

Stitch DESIGN.md 长段压缩改指针；token-only 铁律/OKLCH gamut 警告标唯一权威版本

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b1ba7b6f8-d3d2da29] accept

- **Skill**: design-system-workflow
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
