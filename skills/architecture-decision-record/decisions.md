# architecture-decision-record — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771b89205b8dc-ad20ca5b] accept

- **Skill**: architecture-decision-record
- **DecidedAt**: 2026-07-31T18:00:14Z

### Diagnosis

整体审查发现 frontmatter steps: 7 与正文不符（计数漂移）

### Revision

正文阶段 1 实际为步骤 1-5 共 5 步，steps 改为 5

### Evidence

grep 章节标题确认正文仅 步骤 1：研究备选方案 … 步骤 5：建立索引 共 5 步
