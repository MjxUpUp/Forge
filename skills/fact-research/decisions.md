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

## [d-p2-merge-20260815] accept

> ⚠️ 本 skill 已于 2026-08-15（任务 feat/skill-hitrate-optimization，P2 研究族合并）整体并入 **research-workflow**——本文件是合并墓碑，后续维护与决策记录在 `skills/research-workflow/decisions.md`。

- 诊断: fact-research 与 research-workflow 职责重叠（轻量事实核查 vs 深度调研），三层调研量级路由表需要单一真相源，两 skill 并存导致 dev-lookup/证据链引用摇摆
- 修订: 整体并入 research-workflow「量级判定与轻量档（Phase L）」节（三层量级表+4步协议）；SKILL.md 删除；本决策历史归档至 research-workflow/decisions.md
- 证据: cross-skill 引用（evidence-based-proposal/design-artifact-standards/adversarial-verification/dev-lookup）已全部改指 research-workflow Phase L；引用完整性 grep 零残留
- 结果: accept（合并落地）
