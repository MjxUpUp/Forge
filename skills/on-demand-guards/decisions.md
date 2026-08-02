# on-demand-guards — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7718fe9b06088-ed1e2873] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-07-31T17:57:20Z

### Diagnosis

整体审查发现分工表引用 delivery-gate，非本仓库 canonical skill，未显式标注来源

### Revision

显式标注'非本仓库 canonical skill，仅部分 agent 以扩展形式提供'

### Evidence

ls skills/ 无 delivery-gate；原表述'部分 agent 以扩展形式提供'含糊，易误当本库 skill

## [d-18c7e45aa5b5f724-b249e2be] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-08-02T05:00:55Z

### Diagnosis

审计发现：/freeze 目录锁定的可靠性=agent 每回合记得自检，prompt 型护栏在长会话/压缩后必漂移，恰是它防的场景（机制先天不可靠）；护栏覆盖模式很少（仅 3 条），价值密度偏低

### Revision

按契约 D5 改造为 UX 层：/freeze 主路径改为 forge freeze <path> 激活 / --off 解除 / --status 查看（freeze-guard PreToolUse Write|Edit 真 hook 硬阻断，hook 由 forge 侧并行任务实现）；原 prompt 型护栏降级为「无 forge 环境的 fallback」并保留可靠性上限诚实声明（长会话/压缩后必漂移，能装 forge 就不要依赖 fallback）；/careful 补 hazard-guard 未覆盖模式清单：git clean -fd / npm publish / ssh 生产机 / > 覆盖已有文件；激活状态记忆节区分 forge 持有状态与自检状态；Gotchas/Red Flags 同步更新

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计（on-demand-guards 详评 + 改进清单项 12）

## [d-18c7e620be749168-309fe30c] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md
