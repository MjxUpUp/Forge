# maintainability-and-readability — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c5ec5c22cd6e98-c9b99947] accept

- **Skill**: maintainability-and-readability
- **DecidedAt**: 2026-07-26T19:05:07Z
- **By**: claude-code

### Diagnosis

注释卫生任务要求 Go godoc 双语并存（英文段+中文段），skill 此前缺注释规范条款，agent 无可循标准

### Revision

SKILL.md 新增 §2.7 注释规范：形式 A（英文文档段→空//行→中文段），中文不删不单语；同步铁律（行为变更同步生成器）；范围限定（独立 // 注释双语，行内尾注与字符串字面量不动）

### Evidence

全量双语化 352 个 Go 文件，自检 c1.awk（未双语化 CJK 块计数）=0，c2.awk（重复英文段）空，go build exit 0

### Rationale

注释双语是本任务核心交付，规范须沉淀进 skill 避免下轮 agent 重复探索
