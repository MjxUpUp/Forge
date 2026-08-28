# cross-tool-context — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e5a6b2ce9f54-89e6d691] accept

- **Skill**: cross-tool-context
- **DecidedAt**: 2026-08-02T05:24:41Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

重构定位: forge task 双向锚定为主路径，AI_CONTEXT.md 降为无 forge fallback 节；frontmatter 声明依赖梯度

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b3bb9977c-21587487] accept

- **Skill**: cross-tool-context
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad5496a5b4-c16170ab] accept

- **Skill**: cross-tool-context
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口

## [d-18d0a2f125306-e125306f4] accept

- **Skill**: cross-tool-context
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

正文整节的 forge 命令组（命令语法/机制说明）与通用方法论混排，skills-only 分发用户看到不可执行指令

### Revision

forge 操作细节下沉（新建 references/forge-integration.md 或收进「> Forge 项目」条件块），正文保留机制概述与降级路径

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令
