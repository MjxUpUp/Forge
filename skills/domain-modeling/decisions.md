# domain-modeling — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c75f73b5cd9ed0-051344a0] accept

- **Skill**: domain-modeling
- **DecidedAt**: 2026-07-31T12:25:27Z

### Diagnosis

Forge canonical skill 库（当时 49 个）中无领域语言治理（grep 无 CONTEXT.md/glossary 纪律），AI_CONTEXT.md（cross-tool-context）是跨工具交接载体非术语表——需新增领域建模 skill 补齐该环节

### Revision

新增 domain-modeling skill：五条纪律（挑战/收敛/场景压测/代码印证/就地更新）+ CONTEXT.md 硬边界 + ADR 三条件联动 architecture-decision-record + references/CONTEXT-FORMAT.md

### Evidence

对比分析：当时 skill 库无 CONTEXT.md 术语表/领域语言治理类 skill 覆盖；ADR 撰写不重复造轮子，引用现有 architecture-decision-record skill

## [d-18c75f73b5cd9ed0-review-fix] accept

- **Skill**: domain-modeling
- **DecidedAt**: 2026-07-31T12:58:00Z

### Diagnosis

code-review-gate 子 agent 审查（无 Blocker/Major）指出 2 Minor + 2 可修 Nit：与 requirement-clarification 维度 6（模糊术语消歧）的产出归属未切开；多 context 下 ADR 分层指引在改写中丢失；composes 字段语义不符（联动非组合）；Evidence 中 skill 计数不准。

### Revision

1. description SKIP 补产出分界线（需求级一次性消歧→规格文件 vs 项目级长期术语→CONTEXT.md）；2. references/CONTEXT-FORMAT.md 补多 context ADR 分层一节；3. 删除 composes 字段；4. 修正 decisions 计数表述（49 个）。ADR 触发条件双 skill 口径对齐（Nit 4）留待 architecture-decision-record 侧另行处理。

### Evidence

explore 子 agent 审查报告（git diff main...HEAD 三文件，对照 CONVENTIONS.md 逐项核查 + 保真度比对）。
