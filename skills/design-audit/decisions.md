# design-audit — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18d253700098c82c-3c24a3d9] accept

- **Skill**: design-audit
- **DecidedAt**: 2026-09-05T04:49:02Z

### Diagnosis

功能聚焦批次一线2：按 skills 价值审计（docs/skills-value-audit-2026-08-02.md）与聚焦决策（docs/plans/feature-focus-2026-09.md）执行拆包/瘦身/引用清理

### Revision

拆包至 plugins/forge-design（设计族 12 个）或教科书瘦身/死机制清理（详见 96e0182 提交）

### Evidence

docs/plans/feature-focus-2026-09.md 决策表 + 审计逐项建议 + 96e0182/b967906 提交

## [d-18ddc214b98cd738-7dd2e4b7] accept

- **Skill**: design-audit
- **DecidedAt**: 2026-10-12T10:40:21Z
- **By**: claude-code

### Diagnosis

设计文档→代码落地三态审计与前端无关，却随设计族拆入 forge-design pack，核心 project-acceptance 只能标「未安装则忽略」

### Revision

整体收回核心 skills/，project-acceptance 维度 2 直接引用

### Prediction

project-acceptance 维度 2 结论带 LANDED/PARTIAL/MISSING 判决的比例上升

### Evidence

调研 2026-10-12（R2ABench 2026 / IFScale / ETH AGENTS.md ICLR26 / Spec Kit·Kiro·OpenSpec 模板 / CHI25 Plan-Then-Execute）+ 提案 docs/plans/design-stage-hardening-2026-10-proposal.md；forge skills validate 39/39

### Rationale

设计阶段硬化 W1/W5.3/D2 落地；advisory 起步，按数据窗口复核
