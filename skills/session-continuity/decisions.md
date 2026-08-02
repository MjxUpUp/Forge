# session-continuity — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771f424a2424c-fd540f44] accept

- **Skill**: session-continuity
- **DecidedAt**: 2026-07-31T18:04:30Z

### Diagnosis

整体审查发现两处事实问题：:121 把 ~/.forge/research/session-history.jsonl 描述为'稳定目录'但全仓无任何 forge 命令读写它；:171 'lark-cli docs +fetch --scope outline' 独有 flag 无第二出处（其他 5 处用法均为 --doc-format markdown），无法在本仓核实

### Revision

:121 明确标注'纯 agent 手写约定，无 forge 命令支撑'；:171 改保守表述（主用 --doc-format markdown，--scope outline 标注'若你的 lark-cli 版本支持'）

### Evidence

全仓 grep session-history 仅此 skill 两处；grep 'doc-format markdown|--scope outline' 显示 5 处 --doc-format markdown vs 1 处 --scope outline

## [d-18c7e5a69cff6514-ded2c7fc] accept

- **Skill**: session-continuity
- **DecidedAt**: 2026-08-02T05:24:41Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

删 session-history.jsonl 死约定(D7)；HANDOFF 格式拆 references/handoff-format.md；frontmatter 加适用前提

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b33f0e1e4-1fccda01] accept

- **Skill**: session-continuity
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
