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

## [d-18c7e5a67e4f1254-880708b8] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

拆 references(style-templates/brand-index/motion) 349→195 行；动效选型表/reduced-motion 标唯一权威版本

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b13fb1738-ce93d74f] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-08-02T05:34:09Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cc4bec07616178-366a080e] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-08-16T13:23:49Z

### Diagnosis

同UI族:前端美化/审美执行请求无触发

### Revision

metadata.triggers新增UserPromptSubmit关键词(做好看/美化/风格迁移/高级感/太丑/审美/micro-interaction/动效),cooldown 600

### Evidence

美化类请求高频,触发覆盖缺口

## [d-18cf01ed99b80478-377bfa21] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-08-25T09:21:37Z
- **By**: kimi-code

### Diagnosis

description 490 字符超新定 350 软上限：6 种风格全列名+十余个触发词堆叠，与正文阶段 1 风格清单重复

### Revision

压缩至 336 字符（-31%）：风格枚举下沉正文，触发词保留核心路由词（做好看/Linear·Apple·Vercel 风/品牌 DESIGN.md 还原/高级感动效/反 AI 同质化/Rive·Spline·Framer Motion/micro-interaction/页面太丑），SKIP 去"用"前缀

### Evidence

提示词评审实测 52 个 description 合计 16,486 字符（均值 ~317），本 skill 为最长；改后 forge skills validate 52/52 通过

## [d-18cf032b32fbf778-5294e9be] accept

- **Skill**: frontend-aesthetics-execution
- **DecidedAt**: 2026-08-25T09:44:21Z
- **By**: kimi-code

### Diagnosis

上轮 description 压缩删去的风格/品牌词中，Bento/Neo-brutalism/Stripe 有独立触发价值且 triggers 无兜底（Linear/Apple/Vercel 风等仍在 description 保留，不补）

### Revision

triggers keywords 增补 3 个：Bento、Neo-brutalism、Stripe

### Evidence

审查 minor 修复；forge skills validate 通过
