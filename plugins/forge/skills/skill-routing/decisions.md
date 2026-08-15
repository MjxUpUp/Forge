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
