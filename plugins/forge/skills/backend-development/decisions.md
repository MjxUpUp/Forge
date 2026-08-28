# backend-development — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7722205eb039c-1d8c6e38] accept

- **Skill**: backend-development
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幽灵指针与指错：references/ 目录不存在却有占位句；决策树 bug 修复分支写'此 §7'但 §7 是与其他 skill 的协作节，指错；多语言适配表末行半成品（三个空单元格）且'各自 stack-selection skill'复数不实（仓库只有 frontend-stack-selection）

### Revision

删除 references/ 占位句；决策树改为'（systematic-debugging 主导）'；删除半成品表格行，改为表下注释指向 frontend-stack-selection

### Evidence

ls skills/backend-development 确认无 references/；ls skills/ 确认 stack-selection skill 仅 frontend 一个；SKILL.md 标题枚举确认 §7 语义为协作

## [d-18c7e43de2da2674-13284593] accept

- **Skill**: backend-development
- **DecidedAt**: 2026-08-02T04:58:51Z

### Diagnosis

审计判决'改进（大幅瘦身）'：过半内容是强模型基线教科书；§2.5 可观测、§2.3 鉴权、§2.4 分层与 resilience-and-observability/secure-coding/system-architecture 三处双写；§4 '文件<200行''覆盖率≥80%' 一刀切数字

### Revision

§2.3 鉴权改单行指针→secure-coding、§2.4 分层改单行指针→system-architecture、§2.5 可观测改单行指针→resilience-and-observability；§2.1 API 7 步/§2.6 测试金字塔砍为决策点+反模式密度；§4 一刀切数字改按项目约定；保留负向约束/Gotchas/forge 集成等真增量

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e622c3df1ce8-4075ec60] accept

- **Skill**: backend-development
- **DecidedAt**: 2026-08-02T05:33:34Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad4191089c-73ff3374] accept

- **Skill**: backend-development
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口

## [d-18d0a2f113995-e113995f4] accept

- **Skill**: backend-development
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

checklist/bash 示例里的 forge review pass 顺带提及与 code-review-gate 盖章职责重复

### Revision

指针化：改指 code-review-gate 门控（其 forge 条件块负责盖章），正文零 forge 命令引用

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令
