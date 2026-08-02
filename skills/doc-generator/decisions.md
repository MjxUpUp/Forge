# doc-generator — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e48bf1c158bc-c2a40a86] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-02T05:04:26Z

### Diagnosis

审计发现：按模板生成 PRD/周报已接近主流模型原生能力，skill 真正价值在防编造护栏而非生成能力，但正文未点破；历史存储路径寄居 research 命名空间语义不干净

### Revision

开篇加定位声明（不增强生成能力，价值=防编造护栏+结构一致性+增量记忆）；历史文件路径从 ~/.forge/research/doc-gen-history.jsonl 挪到独立目录 ~/.forge/doc-generator/history.jsonl（模板文件未引用该路径，无需同步改）

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e623067bd438-8887656c] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-02T05:33:35Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md
