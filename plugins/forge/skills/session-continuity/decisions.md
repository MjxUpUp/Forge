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

## [d-18d0a2f109531-e109531f4] accept

- **Skill**: session-continuity
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

方法论正文夹杂 forge 操作句（未标 forge-only、缺降级说明），破坏工具中立性

### Revision

改为「> Forge 项目」条件引用块并补无 forge 降级行为（dev-workflow shell-free 段工具中立化等）

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令

## [d-18cffa78f8101ba8-aa2cb016] accept

- **Skill**: session-continuity
- **DecidedAt**: 2026-08-28T13:16:14Z
- **By**: claude-code

### Diagnosis

SKILL.md/references 含 forge 操作性引用（条件块/forge-integration.md/双路径/模板占位符）——违反 skills 零反向依赖契约（CONVENTIONS §13 R18 硬校验），存量豁免通道要求迁出

### Revision

forge 集成内容整体迁出至 forge 侧 internal/skillintegrate notes/（forge skills integration session-continuity 查看，skill-trigger 推荐块附指针）；正文改为工具中立方法论（降级路径升为主路径/宿主机制中性措辞）

### Evidence

forge skills validate 53/53 通过且 R18Grandfathered 清空；TestR18_Grandfathered_Exact 双向卡死通过

### Rationale

依赖单向化：方法论完整留在中立库，forge 增强完整在 forge 侧；forge 用户体验经集成笔记+触发指针承接
