# transcript-forensics — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18cbf5f3f5cda294-8e5575c0] accept

- **Skill**: transcript-forensics
- **DecidedAt**: 2026-08-15T11:08:25Z

### Diagnosis

检查这个会话jsonl路径+症状假设的模式27次/3项目,session-retrospective管沉淀/systematic-debugging管活bug均不覆盖历史转录行为取证

### Revision

新增pipeline取证skill:症状分类表/嗅探先行/真人输入过滤(各host陷阱)/根因决策树/references转录格式速查

### Evidence

mine-claude 27次/3项目;kimi侧1-2次;本目标执行过程本身即实例
