# backend-development — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7722205eb039c-1d8c6e38] accept

- **Skill**: backend-development
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幽灵指针与指错：references/ 目录不存在却有占位句；决策树 bug 修复分支写'此 §7'但 §7 是与其他 skill 的协作节，指错；多语言适配表末行半成品（三个空单元格）且'各自 stack-selection skill'复数不实（仓库只有 frontend-stack-selection）

### Revision

删除 references/ 占位句；决策树改为'（systematic-debugging 主导）'；删除半成品表格行，改为表下注释指向 frontend-stack-selection

### Evidence

ls skills/backend-development 确认无 references/；ls skills/ 确认 stack-selection skill 仅 frontend 一个；SKILL.md 标题枚举确认 §7 语义为协作
