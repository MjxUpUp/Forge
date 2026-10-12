# design-artifact-standards — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18d2536e778629b8-2d573067] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-09-05T04:48:55Z

### Diagnosis

功能聚焦批次一线2：按 skills 价值审计（docs/skills-value-audit-2026-08-02.md）与聚焦决策（docs/plans/feature-focus-2026-09.md）执行拆包/瘦身/引用清理

### Revision

拆包至 plugins/forge-design（设计族 12 个）或教科书瘦身/死机制清理（详见 96e0182 提交）

### Evidence

docs/plans/feature-focus-2026-09.md 决策表 + 审计逐项建议 + 96e0182/b967906 提交

## [d-18ddc214b43804b0-ffb0b7f8] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-10-12T10:40:20Z
- **By**: claude-code

### Diagnosis

通用方案设计资产（5/6 环节与前端无关）2026-09 随设计族按名字整体拆入 forge-design pack，拆包理由「Go 项目零触发」只对前端 skill 成立

### Revision

整体收回核心 skills/（phase-frontend 随 skill 保留，审查期按 design-artifact-standards/references/phase-<环节>.md 寻址不拆开）；路由表新增「技术方案/设计文档（跨环节）」行指向 doc-generator 三档模板

### Prediction

核心分发宿主上本 skill 与 code-review-gate phase 复核恒可用

### Evidence

调研 2026-10-12（R2ABench 2026 / IFScale / ETH AGENTS.md ICLR26 / Spec Kit·Kiro·OpenSpec 模板 / CHI25 Plan-Then-Execute）+ 提案 docs/plans/design-stage-hardening-2026-10-proposal.md；forge skills validate 39/39

### Rationale

设计阶段硬化 W1/W5.3/D2 落地；advisory 起步，按数据窗口复核
