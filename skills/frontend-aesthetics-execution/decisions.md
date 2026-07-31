# frontend-aesthetics-execution — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc8986349f58-c3561bd2] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/frontend-aesthetics-execution

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c77176f2b70394-a165908d] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-07-31T17:55:32Z

### Diagnosis

整体审查发现 SKILL.md:181 硬编码本机路径 E:\GitHubForkProject\awesome-design-md（分发后必断链），:202-213 的 74 品牌索引使正文臃肿

### Revision

:181 改可发现式（DESIGN_MD_ROOT 环境变量/工作区查找 + git clone 指引 + fallback 阶段 1 通用模板）；品牌索引下沉新建的 references/brand-index.md，正文只留指引

### Evidence

裸路径在非本机环境不存在；库内 skill 正文均只留指引 + references 下沉细则的既有惯例

## [d-18c7729c4f6c2e7c-bedb48c9] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-07-31T18:16:32Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50
