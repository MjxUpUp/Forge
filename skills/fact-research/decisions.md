# fact-research — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e4296e9423b4-391dde48] accept

- **Skill**: fact-research
- **DecidedAt**: 2026-08-02T04:57:23Z

### Diagnosis

审计发现：通道状态表（HN ✅/Wikipedia ❌/Jina ❌）是作者本机网络实测写成通用事实，且对多个源断言「别用」过于绝对

### Revision

通道状态表移至 references/curl-sourcing.md（与 dev-lookup 共用），明确标注「特定网络环境实测，非通用结论」并补自检方法；「别用」软化为「该环境下不可用，自检后再定」；主文件改指针；三层调研量级权威表按裁决保留在本 skill「三层调研量级（路由依据）」节

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e62b275fc9b8-1f958a3f] accept

- **Skill**: fact-research
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
