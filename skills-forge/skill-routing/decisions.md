# skill-routing — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e5a6b747a3f0-5f4cb2c1] accept

- **Skill**: skill-routing
- **DecidedAt**: 2026-08-02T05:24:41Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

routes.json 拆 canonical 11 条(逐个核对已分发)+个人覆盖层 examples/personal-overlay.example.json；新增 scripts/check-routes.sh 发布守卫并自测；措辞改软路由对齐机制上限；补未覆盖宿主说明

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b47826124-4b0b7d94] accept

- **Skill**: skill-routing
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18d0a2f132482-e132482f4] accept

- **Skill**: skill-routing
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

skill 本身描述 forge 命令族操作方法论，无法也不应工具中立化

### Revision

frontmatter 加 metadata.requires_forge: "true" 标记（CONVENTIONS §13 形态③），R18 按标记豁免、skills-only 分发按标记过滤

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令
