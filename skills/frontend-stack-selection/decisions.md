# frontend-stack-selection — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc89816f25b0-6d9e4b46] accept

- **Skill**: frontend-stack-selection
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/frontend-stack-selection

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c7e5a67a0b7214-c3dbad57] accept

- **Skill**: frontend-stack-selection
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

动效选型表改指针到 frontend-aesthetics-execution；加数据快照日期 2026-08 标注；OKLCH/WCAG 改指针

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b10395060-b606fca4] accept

- **Skill**: frontend-stack-selection
- **DecidedAt**: 2026-08-02T05:34:09Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cc4bec127dc8a8-cfde6add] accept

- **Skill**: frontend-stack-selection
- **DecidedAt**: 2026-08-16T13:23:49Z

### Diagnosis

同UI族:前端选型请求无触发

### Revision

metadata.triggers新增UserPromptSubmit关键词(前端选型/选组件库/组件库选/Tauri 还是/Tauri vs/Electron 还是/CSS 方案/前端用什么/桌面端用什么/技术栈选型),cooldown 600

### Evidence

选型请求高频,触发覆盖缺口
