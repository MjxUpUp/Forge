# reverse-engineering-patterns — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c5ec633075c854-9ddf6dc2] accept

- **Skill**: reverse-engineering-patterns
- **DecidedAt**: 2026-07-26T19:05:38Z
- **By**: claude-code

### Diagnosis

注释卫生任务要求去除"借鉴/抄"等字眼，skill 原文多处用"借鉴"表述参考行为

### Revision

SKILL.md 7 处"借鉴"→"参考"（阶段3标题借鉴分析→参考分析、Why值得借鉴→值得参考、借鉴原则→参考原则、不借鉴看起来优雅→不参考），references/claude-code-example.md 同步 4 行

### Evidence

grep 确认 SKILL.md + references 无"借鉴/抄袭/抄来"残留

### Rationale

措辞中性化，避免暗示抄袭，符合注释卫生要求
