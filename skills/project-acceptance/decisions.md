# project-acceptance — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771dc2a936dd8-62958c67] accept

- **Skill**: project-acceptance
- **DecidedAt**: 2026-07-31T18:02:47Z

### Diagnosis

整体审查发现 :40 'grep -nE fn .{50,}|if .{5,}' 伪启发式无判别力（函数名长度/if 条件长度与圈复杂度无关）

### Revision

删除该 grep，只保留 plato/complexity-report/lizard 等真工具

### Evidence

该正则按字符长度匹配，无法反映嵌套深度或分支数，必然大量误报/漏报
