# dev-lookup — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e43204333f24-cec366c8] accept

- **Skill**: dev-lookup
- **DecidedAt**: 2026-08-02T04:58:00Z

### Diagnosis

审计发现：三层调研量级路由表为 4 份复制之一（drift 已开始）；curl 通道状态表与 fact-research 重复且同为环境实测

### Revision

三层量级表删除改指针（唯一真相源：fact-research「三层调研量级（路由依据）」节）；curl 通道状态表下沉至 fact-research references/curl-sourcing.md「技术单点检索通道（dev-lookup 用）」节并标注环境实测，主文件改指针、保留核心纪律

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e62b2b30dc30-59de81de] accept

- **Skill**: dev-lookup
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description SKIP 边界补全 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
